package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

type HealthCheckManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus
	mu         sync.Mutex
	monitors   map[int64]*hcMon
}

type hcMon struct {
	cancel context.CancelFunc
}

func NewHealthCheckManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *HealthCheckManager {
	return &HealthCheckManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]*hcMon),
	}
}

func (m *HealthCheckManager) StartAll() error {
	checks, err := m.store.ListHealthChecks()
	if err != nil {
		return fmt.Errorf("list health checks: %w", err)
	}
	for _, c := range checks {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("failed to start health check for %s: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *HealthCheckManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	cfg, err := m.store.GetHealthCheck(id)
	if err != nil {
		return fmt.Errorf("get health check %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = &hcMon{cancel: cancel}

	go m.runMonitor(ctx, cfg)
	log.Printf("started health check for %s (%s)", cfg.Name, cfg.URL)
	return nil
}

func (m *HealthCheckManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mon, ok := m.monitors[id]; ok {
		mon.cancel()
		delete(m.monitors, id)
		log.Printf("stopped health check id=%d", id)
	}
}

func (m *HealthCheckManager) Restart(id int64) error {
	m.Stop(id)
	time.Sleep(100 * time.Millisecond)
	return m.Start(id)
}

func (m *HealthCheckManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mon := range m.monitors {
		mon.cancel()
		delete(m.monitors, id)
	}
	log.Println("all health check monitors stopped")
}

func (m *HealthCheckManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *HealthCheckManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *HealthCheckManager) emit(typ string, checkID int64, name, message string, data interface{}) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(MonitorEvent{
		Type:       typ,
		DatabaseID: checkID,
		DBName:     name,
		Message:    message,
		Timestamp:  time.Now(),
		Data:       data,
	})
}

func (m *HealthCheckManager) hcSettingKey(id int64) string {
	return fmt.Sprintf("hc_down_notified_%d", id)
}

func (m *HealthCheckManager) isDownNotified(id int64) bool {
	return m.store.GetSetting(m.hcSettingKey(id)) == "1"
}

func (m *HealthCheckManager) setDownNotified(id int64, v bool) {
	if v {
		m.store.SetSetting(m.hcSettingKey(id), "1")
	} else {
		m.store.SetSetting(m.hcSettingKey(id), "")
	}
}

func (m *HealthCheckManager) runMonitor(ctx context.Context, cfg *store.HealthCheck) {
	ticker := time.NewTicker(time.Duration(cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	m.doCheck(cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.doCheck(cfg)
		}
	}
}

func (m *HealthCheckManager) doCheck(cfg *store.HealthCheck) {
	m.emit("healthcheck_checking", cfg.ID, cfg.Name, "检查中...", nil)

	result := executeHealthCheck(cfg)

	m.store.InsertHealthCheckLog(&result)

	if result.Status == "up" {
		m.emit("healthcheck_success", cfg.ID, cfg.Name, fmt.Sprintf("服务正常 (HTTP %d, %dms)", result.HTTPStatus, result.LatencyMs), nil)

		if m.isDownNotified(cfg.ID) {
			m.setDownNotified(cfg.ID, false)
			recoveryMsg := fmt.Sprintf("服务恢复通知\n\n服务: %s\nURL: %s\n状态: 已恢复正常", cfg.Name, cfg.URL)
			if sendErr := m.dispatcher.SendScopedNotifications("health", cfg.ID,recoveryMsg); sendErr != nil {
				log.Printf("[HealthCheck %s] recovery notification failed: %v", cfg.Name, sendErr)
			} else {
				m.emit("healthcheck_notified", cfg.ID, cfg.Name, "已发送恢复通知", nil)
			}
		}
	} else {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", result.HTTPStatus)
		}
		m.emit("healthcheck_error", cfg.ID, cfg.Name, fmt.Sprintf("服务异常: %s", errMsg), nil)

		if !m.isDownNotified(cfg.ID) {
			alertMsg := fmt.Sprintf("服务异常告警\n\n服务: %s\nURL: %s\n状态: %s\n错误: %s\n\n该告警仅发送一次，恢复后如再次异常将重新通知。",
				cfg.Name, cfg.URL, result.Status, errMsg)
			if sendErr := m.dispatcher.SendScopedNotifications("health", cfg.ID,alertMsg); sendErr != nil {
				log.Printf("[HealthCheck %s] alert notification failed: %v", cfg.Name, sendErr)
			} else {
				m.emit("healthcheck_notified", cfg.ID, cfg.Name, "已发送异常告警通知", nil)
			}
			m.setDownNotified(cfg.ID, true)
		}
	}
}

func executeHealthCheck(cfg *store.HealthCheck) store.HealthCheckLog {
	result := store.HealthCheckLog{
		CheckID:   cfg.ID,
		CheckName: cfg.Name,
	}

	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second}

	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequest(cfg.Method, cfg.URL, bodyReader)
	if err != nil {
		result.Status = "down"
		result.Error = fmt.Sprintf("创建请求失败: %v", err)
		return result
	}

	// Parse and set custom headers
	if cfg.HeadersJSON != "" && cfg.HeadersJSON != "{}" {
		var headers map[string]string
		if json.Unmarshal([]byte(cfg.HeadersJSON), &headers) == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = "down"
		result.Error = fmt.Sprintf("请求失败: %v", err)
		return result
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode

	// Read response body (limit to 500 chars for storage)
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	bodyStr := string(bodyBytes)
	if len(bodyStr) > 500 {
		result.Response = bodyStr[:500]
	} else {
		result.Response = bodyStr
	}

	// Check HTTP status code
	if resp.StatusCode != cfg.ExpectedStatus {
		result.Status = "down"
		result.Error = fmt.Sprintf("期望状态码 %d, 实际 %d", cfg.ExpectedStatus, resp.StatusCode)
		return result
	}

	// Check expected field in JSON response
	if cfg.ExpectedField != "" && cfg.ExpectedValue != "" {
		var respJSON map[string]interface{}
		if json.Unmarshal(bodyBytes, &respJSON) != nil {
			result.Status = "down"
			result.Error = "响应非有效JSON，无法检查字段"
			return result
		}
		val, ok := respJSON[cfg.ExpectedField]
		if !ok {
			result.Status = "down"
			result.Error = fmt.Sprintf("响应中缺少字段 %q", cfg.ExpectedField)
			return result
		}
		valStr := fmt.Sprintf("%v", val)
		if valStr != cfg.ExpectedValue {
			result.Status = "down"
			result.Error = fmt.Sprintf("字段 %q 期望值 %q, 实际值 %q", cfg.ExpectedField, cfg.ExpectedValue, valStr)
			return result
		}
	}

	result.Status = "up"
	return result
}

// TestHealthCheck executes a single health check and returns the result.
func TestHealthCheck(cfg *store.HealthCheck) store.HealthCheckLog {
	return executeHealthCheck(cfg)
}
