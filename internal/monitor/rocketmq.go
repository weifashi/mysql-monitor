package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

type RocketMQManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus
	mu         sync.Mutex
	monitors   map[int64]*rocketmqMon
}

type rocketmqMon struct {
	cancel context.CancelFunc
}

func NewRocketMQManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *RocketMQManager {
	return &RocketMQManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]*rocketmqMon),
	}
}

func (m *RocketMQManager) StartAll() error {
	configs, err := m.store.ListRocketMQConfigs()
	if err != nil {
		return fmt.Errorf("list rocketmq configs: %w", err)
	}
	for _, c := range configs {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("failed to start rocketmq monitor for %s: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *RocketMQManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	cfg, err := m.store.GetRocketMQConfig(id)
	if err != nil {
		return fmt.Errorf("get rocketmq config %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = &rocketmqMon{cancel: cancel}

	go m.runMonitor(ctx, cfg)
	log.Printf("started rocketmq monitor for %s (%s)", cfg.Name, cfg.ConsumerGroup)
	return nil
}

func (m *RocketMQManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mon, ok := m.monitors[id]; ok {
		mon.cancel()
		delete(m.monitors, id)
		log.Printf("stopped rocketmq monitor id=%d", id)
	}
}

func (m *RocketMQManager) Restart(id int64) error {
	m.Stop(id)
	time.Sleep(100 * time.Millisecond)
	return m.Start(id)
}

func (m *RocketMQManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mon := range m.monitors {
		mon.cancel()
		delete(m.monitors, id)
	}
	log.Println("all rocketmq monitors stopped")
}

func (m *RocketMQManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *RocketMQManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *RocketMQManager) emit(typ string, configID int64, name, message string, data interface{}) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(MonitorEvent{
		Type:       typ,
		DatabaseID: configID,
		DBName:     name,
		Message:    message,
		Timestamp:  time.Now(),
		Data:       data,
	})
}

func (m *RocketMQManager) settingKey(id int64, suffix string) string {
	return fmt.Sprintf("rmq_%s_%d", suffix, id)
}

func (m *RocketMQManager) isNotified(id int64, suffix string) bool {
	return m.store.GetSetting(m.settingKey(id, suffix)) == "1"
}

func (m *RocketMQManager) setNotified(id int64, suffix string, v bool) {
	if v {
		m.store.SetSetting(m.settingKey(id, suffix), "1")
	} else {
		m.store.SetSetting(m.settingKey(id, suffix), "")
	}
}

func (m *RocketMQManager) runMonitor(ctx context.Context, cfg *store.RocketMQConfig) {
	client := newRocketMQClient()
	ticker := time.NewTicker(time.Duration(cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	m.doCheck(cfg, client)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.doCheck(cfg, client)
		}
	}
}

func (m *RocketMQManager) doCheck(cfg *store.RocketMQConfig, client *http.Client) {
	m.emit("rocketmq_checking", cfg.ID, cfg.Name, "检查中...", nil)

	diffTotal, err := queryConsumerLag(client, cfg)
	if err != nil {
		log.Printf("[RocketMQ %s] query error: %v", cfg.Name, err)
		m.emit("rocketmq_error", cfg.ID, cfg.Name, fmt.Sprintf("查询错误: %v", err), nil)

		if !m.isNotified(cfg.ID, "err") {
			errMsg := fmt.Sprintf("RocketMQ 连接异常告警\n\n配置: %s\nDashboard: %s\n消费组: %s\nTopic: %s\n错误: %v\n\n该告警仅发送一次，恢复后如再次异常将重新通知。",
				cfg.Name, cfg.DashboardURL, cfg.ConsumerGroup, cfg.Topic, err)
			if sendErr := m.dispatcher.SendGlobalNotifications(errMsg); sendErr != nil {
				log.Printf("[RocketMQ %s] error notification failed: %v", cfg.Name, sendErr)
			} else {
				m.emit("rocketmq_notified", cfg.ID, cfg.Name, "已发送连接错误通知", nil)
			}
			m.setNotified(cfg.ID, "err", true)
		}
		return
	}

	// Connection OK — reset error state
	if m.isNotified(cfg.ID, "err") {
		m.setNotified(cfg.ID, "err", false)
		recoveryMsg := fmt.Sprintf("RocketMQ 连接恢复通知\n\n配置: %s\nDashboard: %s\n状态: 连接已恢复正常", cfg.Name, cfg.DashboardURL)
		if sendErr := m.dispatcher.SendGlobalNotifications(recoveryMsg); sendErr != nil {
			log.Printf("[RocketMQ %s] recovery notification failed: %v", cfg.Name, sendErr)
		} else {
			m.emit("rocketmq_notified", cfg.ID, cfg.Name, "已发送连接恢复通知", nil)
		}
	}

	if diffTotal > int64(cfg.Threshold) {
		m.emit("rocketmq_alert", cfg.ID, cfg.Name, fmt.Sprintf("消息堆积: %d (阈值: %d)", diffTotal, cfg.Threshold), map[string]interface{}{
			"diff_total": diffTotal,
			"threshold":  cfg.Threshold,
		})

		// Record alert log + send notification only once per incident
		if !m.isNotified(cfg.ID, "alert") {
			alertLog := &store.RocketMQAlertLog{
				ConfigID:      cfg.ID,
				ConfigName:    cfg.Name,
				ConsumerGroup: cfg.ConsumerGroup,
				Topic:         cfg.Topic,
				DiffTotal:     diffTotal,
			}
			if _, err := m.store.InsertRocketMQAlertLog(alertLog); err != nil {
				log.Printf("[RocketMQ %s] insert alert log error: %v", cfg.Name, err)
			}

			alertMsg := fmt.Sprintf("RocketMQ 消息堆积告警\n\n配置: %s\nDashboard: %s\n消费组: %s\nTopic: %s\n堆积量: %d\n阈值: %d\n\n该告警仅发送一次，堆积消除后如再次超阈值将重新通知。",
				cfg.Name, cfg.DashboardURL, cfg.ConsumerGroup, cfg.Topic, diffTotal, cfg.Threshold)
			if sendErr := m.dispatcher.SendGlobalNotifications(alertMsg); sendErr != nil {
				log.Printf("[RocketMQ %s] alert notification failed: %v", cfg.Name, sendErr)
			} else {
				m.emit("rocketmq_notified", cfg.ID, cfg.Name, "已发送堆积告警通知", nil)
			}
			m.setNotified(cfg.ID, "alert", true)
		}
	} else {
		m.emit("rocketmq_ok", cfg.ID, cfg.Name, fmt.Sprintf("堆积: %d (阈值: %d)", diffTotal, cfg.Threshold), nil)

		// Recovery from alert
		if m.isNotified(cfg.ID, "alert") {
			m.setNotified(cfg.ID, "alert", false)
			recoveryMsg := fmt.Sprintf("RocketMQ 堆积恢复通知\n\n配置: %s\n消费组: %s\nTopic: %s\n当前堆积: %d (阈值: %d)\n状态: 堆积已消除",
				cfg.Name, cfg.ConsumerGroup, cfg.Topic, diffTotal, cfg.Threshold)
			if sendErr := m.dispatcher.SendGlobalNotifications(recoveryMsg); sendErr != nil {
				log.Printf("[RocketMQ %s] recovery notification failed: %v", cfg.Name, sendErr)
			} else {
				m.emit("rocketmq_notified", cfg.ID, cfg.Name, "已发送堆积恢复通知", nil)
			}
		}
	}
}

// newRocketMQClient creates an http.Client that preserves auth headers on redirects.
func newRocketMQClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout: 15 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Detect login redirect (hash fragment #/login)
			if strings.Contains(req.URL.String(), "/login") || strings.Contains(req.URL.Fragment, "login") {
				return http.ErrUseLastResponse
			}
			// Preserve Authorization header on redirects
			if auth := via[0].Header.Get("Authorization"); auth != "" {
				req.Header.Set("Authorization", auth)
			}
			return nil
		},
	}
}

// queryConsumerLag queries RocketMQ Dashboard API for consumer lag on the configured topic.
func queryConsumerLag(client *http.Client, cfg *store.RocketMQConfig) (int64, error) {
	apiURL := strings.TrimRight(cfg.DashboardURL, "/") + "/consumer/queryTopicByConsumer.query?consumerGroup=" + cfg.ConsumerGroup

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	// Always set Basic Auth if credentials are configured
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return 0, fmt.Errorf("认证失败 (HTTP %d)，请检查用户名和密码", resp.StatusCode)
	}
	if resp.StatusCode == 302 {
		return 0, fmt.Errorf("Dashboard 重定向到登录页，请检查用户名和密码")
	}
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	// RocketMQ Dashboard response: { "status": 0, "data": [ { "topic": "X", "diffTotal": N, ... } ] }
	var result struct {
		Status int `json:"status"`
		Data   []struct {
			Topic     string `json:"topic"`
			DiffTotal int64  `json:"diffTotal"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if cfg.Username == "" {
			return 0, fmt.Errorf("Dashboard 需要登录，请在配置中填写用户名和密码")
		}
		return 0, fmt.Errorf("Dashboard 返回非 JSON 响应，请检查地址和凭据是否正确")
	}

	for _, item := range result.Data {
		if item.Topic == cfg.Topic {
			return item.DiffTotal, nil
		}
	}

	return 0, fmt.Errorf("topic %q not found in consumer group %q", cfg.Topic, cfg.ConsumerGroup)
}

// TestRocketMQConnection tests connectivity to RocketMQ Dashboard.
func TestRocketMQConnection(cfg *store.RocketMQConfig) error {
	client := newRocketMQClient()

	apiURL := strings.TrimRight(cfg.DashboardURL, "/") + "/topic/list.query?skipSysProcess=true&skipRetryAndDlq=true"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("认证失败 (HTTP %d)，请检查用户名和密码", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
