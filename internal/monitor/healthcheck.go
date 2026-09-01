package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

type HealthCheckManager struct {
	store        *store.Store
	dispatcher   *notify.Dispatcher
	eventBus     *EventBus
	mu           sync.Mutex
	monitors     map[int64]*hcMon
	metricStates map[string]*healthMetricState
}

type hcMon struct {
	cancel context.CancelFunc
}

type healthMetricState struct {
	Version            int
	Field              string
	HasLast            bool
	LastValue          float64
	ConsecutiveRise    int
	ConsecutiveMatched int
}

type healthAlertRule struct {
	Name              string `json:"name"`
	Field             string `json:"field"`
	Strategy          string `json:"strategy"`
	Condition         string `json:"condition"`
	Value             string `json:"value"`
	DeltaValue        string `json:"delta_value"`
	DeltaPercent      string `json:"delta_percent"`
	Consecutive       int    `json:"consecutive"`
	AlertDeltaValue   string `json:"alert_delta_value,omitempty"`
	AlertDeltaPercent string `json:"alert_delta_percent,omitempty"`
	AlertConsecutive  int    `json:"alert_consecutive,omitempty"`
}

type healthTriggerAction struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Command        string `json:"command"`
	URL            string `json:"url"`
	Method         string `json:"method"`
	HeadersJSON    string `json:"headers_json"`
	Body           string `json:"body"`
	TimeoutSec     int    `json:"timeout_sec"`
	NotifyMaxChars int    `json:"notify_max_chars"`
	Enabled        *bool  `json:"enabled"`
}

func NewHealthCheckManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *HealthCheckManager {
	return &HealthCheckManager{
		store:        s,
		dispatcher:   d,
		eventBus:     eb,
		monitors:     make(map[int64]*hcMon),
		metricStates: make(map[string]*healthMetricState),
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
		m.deleteMetricStates(id)
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
		m.deleteMetricStates(id)
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

	result := m.executeHealthCheck(cfg)
	previousStatus, hasPreviousStatus, err := m.store.LastHealthCheckLogStatus(cfg.ID)
	if err != nil {
		log.Printf("[HealthCheck %s] load last status failed: %v", cfg.Name, err)
	}
	wasDown := m.isDownNotified(cfg.ID) || (hasPreviousStatus && previousStatus != "up")

	if result.Status == "up" {
		m.store.InsertHealthCheckLog(&result)
		m.emit("healthcheck_success", cfg.ID, cfg.Name, fmt.Sprintf("服务正常 (HTTP %d, %dms)", result.HTTPStatus, result.LatencyMs), nil)

		if wasDown {
			m.store.ResolveEvent("health", cfg.ID, fmt.Sprintf("HTTP %d", result.HTTPStatus))
			recoveryMsg := fmt.Sprintf("服务恢复通知\n\n服务: %s\nURL: %s\n状态: 已恢复正常", cfg.Name, cfg.URL)
			if sendErr := m.dispatcher.SendScopedNotifications("health", cfg.ID, recoveryMsg); sendErr != nil {
				log.Printf("[HealthCheck %s] recovery notification failed: %v", cfg.Name, sendErr)
				m.setDownNotified(cfg.ID, true)
				m.emit("healthcheck_notify_error", cfg.ID, cfg.Name, fmt.Sprintf("恢复通知发送失败: %v", sendErr), nil)
			} else {
				m.setDownNotified(cfg.ID, false)
				m.emit("healthcheck_notified", cfg.ID, cfg.Name, "已发送恢复通知", nil)
			}
		}
	} else {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", result.HTTPStatus)
		}
		m.emit("healthcheck_error", cfg.ID, cfg.Name, fmt.Sprintf("服务异常: %s", errMsg), nil)

		firstDown := !m.isDownNotified(cfg.ID)
		m.store.UpsertFiringEvent(&store.AlertEvent{
			Source: "health", CheckID: cfg.ID, CheckName: cfg.Name,
			Title: cfg.Name, TargetID: cfg.ID, TargetName: cfg.Name,
			Dimension: "site", Severity: "critical",
			Value: errMsg, Message: errMsg,
		}, firstDown)
		if firstDown {
			actionSummary, notifyActionSummary := runHealthTriggerActions(cfg)
			diagnosticsSection := ""
			if notifyActionSummary != "" {
				diagnosticsSection = "\n\n诊断输出:\n" + notifyActionSummary
			}
			if actionSummary != "" {
				result.DiagnosticOutput = actionSummary
				result.Error = truncateForLog(result.Error, "\n\n触发操作: 已执行诊断动作", 4000)
				errMsg = result.Error
				m.emit("healthcheck_action", cfg.ID, cfg.Name, "已执行异常触发操作", map[string]string{"summary": actionSummary})
			}
			m.store.InsertHealthCheckLog(&result)
			alertMsg := fmt.Sprintf("服务异常告警\n\n服务: %s\nURL: %s\n状态: %s\n错误: %s\n\n该告警仅发送一次，恢复后如再次异常将重新通知。%s",
				cfg.Name, cfg.URL, result.Status, errMsg, diagnosticsSection)
			if sendErr := m.dispatcher.SendScopedNotifications("health", cfg.ID, alertMsg); sendErr != nil {
				log.Printf("[HealthCheck %s] alert notification failed: %v", cfg.Name, sendErr)
				m.emit("healthcheck_notify_error", cfg.ID, cfg.Name, fmt.Sprintf("通知发送失败: %v", sendErr), nil)
			} else {
				m.emit("healthcheck_notified", cfg.ID, cfg.Name, "已发送异常告警通知", nil)
				m.setDownNotified(cfg.ID, true)
			}
		} else {
			m.store.InsertHealthCheckLog(&result)
		}
	}
}

func (m *HealthCheckManager) deleteMetricStates(id int64) {
	prefix := fmt.Sprintf("%d:", id)
	for key := range m.metricStates {
		if strings.HasPrefix(key, prefix) {
			delete(m.metricStates, key)
		}
	}
}

func (m *HealthCheckManager) metricState(id int64, ruleKey, field string) *healthMetricState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ruleKey == "" {
		ruleKey = field
	}
	key := fmt.Sprintf("%d:%s", id, ruleKey)
	st := m.metricStates[key]
	if st == nil || st.Field != field {
		st = &healthMetricState{Field: field}
		m.metricStates[key] = st
	}
	return st
}

func (m *HealthCheckManager) executeHealthCheck(cfg *store.HealthCheck) store.HealthCheckLog {
	return executeHealthCheckWithMetricStateProvider(cfg, func(ruleKey, field string) *healthMetricState {
		return m.metricState(cfg.ID, ruleKey, field)
	})
}

func executeHealthCheckWithMetricState(cfg *store.HealthCheck, metricState *healthMetricState) store.HealthCheckLog {
	return executeHealthCheckWithMetricStateProvider(cfg, func(_, _ string) *healthMetricState {
		return metricState
	})
}

func executeHealthCheckWithMetricStateProvider(cfg *store.HealthCheck, metricState func(ruleKey, field string) *healthMetricState) store.HealthCheckLog {
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

	bodyBytes, _ := io.ReadAll(resp.Body)
	result.Response = string(bodyBytes)

	// Check HTTP status code
	if resp.StatusCode != cfg.ExpectedStatus {
		result.Status = "down"
		result.Error = fmt.Sprintf("期望状态码 %d, 实际 %d", cfg.ExpectedStatus, resp.StatusCode)
		return result
	}

	var respJSON map[string]interface{}
	alertRules := healthAlertRulesFromConfig(cfg)
	if (cfg.ExpectedField != "" && cfg.ExpectedValue != "") || len(alertRules) > 0 {
		if json.Unmarshal(bodyBytes, &respJSON) != nil {
			result.Status = "down"
			result.Error = "响应非有效JSON，无法检查字段"
			return result
		}
	}

	// Check expected field in JSON response. This is a healthy condition.
	if cfg.ExpectedField != "" && cfg.ExpectedValue != "" {
		val, ok := getJSONPath(respJSON, cfg.ExpectedField)
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

	// Check alert rules in JSON response. If any rule matches, service is down.
	if len(alertRules) > 0 {
		for i, rule := range alertRules {
			val, ok := getJSONPath(respJSON, rule.Field)
			ruleName := strings.TrimSpace(rule.Name)
			if ruleName == "" {
				ruleName = rule.Field
			}
			ruleKey := fmt.Sprintf("%d:%s:%s", i, ruleName, rule.Field)
			if !ok {
				result.Status = "down"
				result.Error = fmt.Sprintf("响应中缺少异常字段 %q", rule.Field)
				return result
			}
			valStr := fmt.Sprintf("%v", val)
			ruleCfg := healthCheckForAlertRule(cfg, rule)
			matched, msg := evaluateHealthAlertRule(valStr, ruleCfg, metricState(ruleKey, rule.Field))
			if matched {
				result.Status = "down"
				result.Error = fmt.Sprintf("命中规则: %s\n%s", ruleName, msg)
				return result
			}
		}
	}

	result.Status = "up"
	return result
}

func healthAlertRulesFromConfig(cfg *store.HealthCheck) []healthAlertRule {
	var rules []healthAlertRule
	if strings.TrimSpace(cfg.AlertRules) != "" && strings.TrimSpace(cfg.AlertRules) != "[]" {
		if err := json.Unmarshal([]byte(cfg.AlertRules), &rules); err != nil {
			rules = nil
		}
	}
	normalized := make([]healthAlertRule, 0, len(rules)+1)
	for _, rule := range rules {
		rule.Field = strings.TrimSpace(rule.Field)
		if rule.Field == "" {
			continue
		}
		if rule.Strategy == "" {
			rule.Strategy = "threshold"
		}
		if rule.Condition == "" {
			rule.Condition = "gt"
		}
		if rule.Consecutive <= 0 {
			if rule.AlertConsecutive > 0 {
				rule.Consecutive = rule.AlertConsecutive
			} else {
				rule.Consecutive = 1
			}
		}
		if rule.DeltaValue == "" {
			rule.DeltaValue = rule.AlertDeltaValue
		}
		if rule.DeltaPercent == "" {
			rule.DeltaPercent = rule.AlertDeltaPercent
		}
		normalized = append(normalized, rule)
	}
	if len(normalized) > 0 {
		return normalized
	}
	if strings.TrimSpace(cfg.AlertField) == "" {
		return nil
	}
	return []healthAlertRule{{
		Field:        cfg.AlertField,
		Strategy:     cfg.AlertStrategy,
		Condition:    cfg.AlertCondition,
		Value:        cfg.AlertValue,
		DeltaValue:   cfg.AlertDeltaValue,
		DeltaPercent: cfg.AlertDeltaPercent,
		Consecutive:  cfg.AlertConsecutive,
	}}
}

func healthCheckForAlertRule(base *store.HealthCheck, rule healthAlertRule) *store.HealthCheck {
	copied := *base
	copied.AlertField = rule.Field
	copied.AlertStrategy = rule.Strategy
	copied.AlertCondition = rule.Condition
	copied.AlertValue = normalizeHealthConfiguredMetricValue(rule.Field, rule.Value)
	copied.AlertDeltaValue = normalizeHealthConfiguredMetricValue(rule.Field, rule.DeltaValue)
	copied.AlertDeltaPercent = rule.DeltaPercent
	copied.AlertConsecutive = rule.Consecutive
	if copied.AlertStrategy == "" {
		copied.AlertStrategy = "threshold"
	}
	if copied.AlertCondition == "" {
		copied.AlertCondition = "gt"
	}
	if copied.AlertConsecutive <= 0 {
		copied.AlertConsecutive = 1
	}
	return &copied
}

func evaluateSingleHealthAlertField(respJSON map[string]interface{}, cfg *store.HealthCheck, metricState *healthMetricState) (bool, string, error) {
	if cfg.AlertField != "" {
		val, ok := getJSONPath(respJSON, cfg.AlertField)
		if !ok {
			return false, "", fmt.Errorf("响应中缺少异常字段 %q", cfg.AlertField)
		}
		valStr := fmt.Sprintf("%v", val)
		matched, msg := evaluateHealthAlertRule(valStr, cfg, metricState)
		if matched {
			return true, msg, nil
		}
		return false, msg, nil
	}
	return false, "", nil
}

func evaluateHealthAlertRule(value string, cfg *store.HealthCheck, st *healthMetricState) (bool, string) {
	strategy := strings.ToLower(strings.TrimSpace(cfg.AlertStrategy))
	if strategy == "" {
		strategy = "threshold"
	}
	consecutive := cfg.AlertConsecutive
	if consecutive <= 0 {
		consecutive = 1
	}
	thresholdConfigured := strings.TrimSpace(cfg.AlertValue) != "" || cfg.AlertCondition == "empty" || cfg.AlertCondition == "not_empty"
	comparableValue := normalizeHealthRuntimeMetricValue(cfg.AlertField, value)
	thresholdMatched, thresholdMsg := evaluateHealthAlertCondition(comparableValue, cfg.AlertCondition, cfg.AlertValue)

	switch strategy {
	case "threshold":
		return thresholdMatched, formatHealthThresholdMessage(cfg.AlertField, comparableValue, cfg.AlertCondition, cfg.AlertValue, thresholdMsg, 0, 0)
	case "sustained":
		if st == nil {
			st = &healthMetricState{}
		}
		if thresholdMatched {
			st.ConsecutiveMatched++
		} else {
			st.ConsecutiveMatched = 0
		}
		matched := st.ConsecutiveMatched >= consecutive
		return matched, formatHealthThresholdMessage(cfg.AlertField, comparableValue, cfg.AlertCondition, cfg.AlertValue, thresholdMsg, st.ConsecutiveMatched, consecutive)
	case "increase", "sudden_increase":
		current, err := strconv.ParseFloat(strings.TrimSpace(comparableValue), 64)
		if err != nil {
			return true, fmt.Sprintf("%s 突增判断需要数值，当前值=%q", cfg.AlertField, value)
		}
		if st == nil {
			st = &healthMetricState{}
		}
		matched, msg := evaluateIncreaseRule(current, cfg, st, thresholdConfigured, thresholdMatched)
		st.HasLast = true
		st.LastValue = current
		return matched, msg
	case "continuous_increase":
		current, err := strconv.ParseFloat(strings.TrimSpace(comparableValue), 64)
		if err != nil {
			return true, fmt.Sprintf("%s 连续上升判断需要数值，当前值=%q", cfg.AlertField, value)
		}
		if st == nil {
			st = &healthMetricState{}
		}
		if st.HasLast && current > st.LastValue {
			st.ConsecutiveRise++
		} else {
			st.ConsecutiveRise = 0
		}
		gateMatched := true
		if thresholdConfigured {
			gateMatched = thresholdMatched
		}
		matched := gateMatched && st.ConsecutiveRise >= consecutive
		msg := fmt.Sprintf("字段: %s\n当前值: %s\n上次值: %s\n策略: 连续上升\n连续: %d/%d 次", cfg.AlertField, formatHealthNumberForField(cfg.AlertField, current), formatHealthNumberForField(cfg.AlertField, st.LastValue), st.ConsecutiveRise, consecutive)
		if thresholdConfigured {
			msg += fmt.Sprintf("\n条件: %s %s", healthConditionSymbol(cfg.AlertCondition), formatHealthMetricValueForField(cfg.AlertField, cfg.AlertValue))
			if thresholdMsg != "" {
				msg += "\n判断: " + thresholdMsg
			}
		}
		st.HasLast = true
		st.LastValue = current
		return matched, msg
	default:
		return thresholdMatched, fmt.Sprintf("未知策略 %q，按单次阈值判断: %s", cfg.AlertStrategy, thresholdMsg)
	}
}

func evaluateIncreaseRule(current float64, cfg *store.HealthCheck, st *healthMetricState, thresholdConfigured, thresholdMatched bool) (bool, string) {
	if !st.HasLast {
		return false, fmt.Sprintf("字段: %s\n当前值: %s\n策略: 突增\n状态: 等待下一次采样", cfg.AlertField, formatHealthNumberForField(cfg.AlertField, current))
	}
	delta := current - st.LastValue
	percentText := "N/A"
	percentMatched := false
	if st.LastValue != 0 {
		percent := delta / st.LastValue * 100
		percentText = fmt.Sprintf("%.2f%%", percent)
		if target, ok := parseOptionalFloat(cfg.AlertDeltaPercent); ok {
			percentMatched = percent >= target
		}
	}
	deltaMatched := false
	if target, ok := parseOptionalFloat(cfg.AlertDeltaValue); ok {
		deltaMatched = delta >= target
	}
	if strings.TrimSpace(cfg.AlertDeltaValue) == "" && strings.TrimSpace(cfg.AlertDeltaPercent) == "" {
		deltaMatched = delta > 0
	}
	gateMatched := true
	if thresholdConfigured {
		gateMatched = thresholdMatched
	}
	matched := gateMatched && (deltaMatched || percentMatched)
	msg := fmt.Sprintf("字段: %s\n当前值: %s\n上次值: %s\n变化量: %s\n变化率: %s\n策略: 突增", cfg.AlertField, formatHealthNumberForField(cfg.AlertField, current), formatHealthNumberForField(cfg.AlertField, st.LastValue), formatHealthNumberForField(cfg.AlertField, delta), percentText)
	if cfg.AlertDeltaValue != "" {
		msg += "\n变化量阈值: >= " + formatHealthMetricValueForField(cfg.AlertField, cfg.AlertDeltaValue)
	}
	if cfg.AlertDeltaPercent != "" {
		msg += "\n变化率阈值: >= " + cfg.AlertDeltaPercent + "%"
	}
	if thresholdConfigured {
		msg += fmt.Sprintf("\n当前值条件: %s %s", healthConditionSymbol(cfg.AlertCondition), formatHealthMetricValueForField(cfg.AlertField, cfg.AlertValue))
	}
	return matched, msg
}

func formatHealthThresholdMessage(field, value, condition, threshold, reason string, consecutiveMatched, consecutiveTarget int) string {
	lines := []string{
		"字段: " + field,
		"当前值: " + formatHealthMetricValueForField(field, value),
		"条件: " + healthConditionSymbol(condition) + " " + formatHealthMetricValueForField(field, threshold),
	}
	if consecutiveTarget > 0 {
		lines = append(lines, fmt.Sprintf("连续: %d/%d 次", consecutiveMatched, consecutiveTarget))
	}
	return strings.Join(lines, "\n")
}

func healthConditionSymbol(condition string) string {
	switch strings.ToLower(strings.TrimSpace(condition)) {
	case "gt", ">":
		return ">"
	case "gte", ">=":
		return ">="
	case "lt", "<":
		return "<"
	case "lte", "<=":
		return "<="
	case "eq", "==":
		return "=="
	case "ne", "!=":
		return "!="
	case "contains":
		return "包含"
	case "not_contains":
		return "不包含"
	case "empty":
		return "为空"
	case "not_empty":
		return "不为空"
	default:
		if strings.TrimSpace(condition) == "" {
			return ">"
		}
		return condition
	}
}

func normalizeHealthRuntimeMetricValue(field, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !isHealthKBMetricField(field) {
		return raw
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
	if err != nil {
		return raw
	}
	return strconv.FormatFloat(v/1024, 'f', -1, 64)
}

func normalizeHealthConfiguredMetricValue(field, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !isHealthKBMetricField(field) {
		return raw
	}
	// Only migrate the old built-in template values. User-entered values are now
	// stored as MB and must not be guessed by magnitude.
	switch strings.ReplaceAll(raw, ",", "") {
	case "1024000":
		return "1000"
	case "150000000":
		return "1000"
	case "819200":
		return "800"
	case "1500000":
		return "1464.84"
	case "1200000":
		return "1171.88"
	}
	return raw
}

func isHealthKBMetricField(field string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(field)), "_kb")
}

func formatHealthMetricValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return formatHealthNumber(v)
	}
	return raw
}

func formatHealthMetricValueForField(field, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return formatHealthNumberForField(field, v)
	}
	return raw
}

func formatHealthNumberForField(field string, v float64) string {
	if isHealthKBMetricField(field) {
		return formatHealthNumber(v) + " MB"
	}
	return formatHealthNumber(v)
}

func formatHealthNumber(v float64) string {
	if v == float64(int64(v)) {
		return formatIntWithCommas(int64(v))
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func formatIntWithCommas(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func parseOptionalFloat(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	return v, err == nil
}

func getJSONPath(root map[string]interface{}, path string) (interface{}, bool) {
	if root == nil {
		return nil, false
	}
	parts := strings.Split(strings.TrimSpace(path), ".")
	var cur interface{} = root
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func evaluateHealthAlertCondition(value, condition, threshold string) (bool, string) {
	cond := strings.ToLower(strings.TrimSpace(condition))
	if cond == "" {
		cond = "gt"
	}
	actual := strings.TrimSpace(value)
	target := strings.TrimSpace(threshold)
	switch cond {
	case "empty":
		ok := actual == ""
		return ok, fmt.Sprintf("当前值为空: %t", ok)
	case "not_empty":
		ok := actual != ""
		return ok, fmt.Sprintf("当前值非空: %t", ok)
	case "contains":
		ok := strings.Contains(actual, target)
		return ok, fmt.Sprintf("当前值 contains %q: %t", target, ok)
	case "not_contains":
		ok := !strings.Contains(actual, target)
		return ok, fmt.Sprintf("当前值 not contains %q: %t", target, ok)
	case "eq", "==":
		ok := actual == target
		return ok, fmt.Sprintf("当前值 %q == %q: %t", actual, target, ok)
	case "ne", "!=":
		ok := actual != target
		return ok, fmt.Sprintf("当前值 %q != %q: %t", actual, target, ok)
	case "gt", ">", "gte", ">=", "lt", "<", "lte", "<=":
		a, aErr := strconv.ParseFloat(actual, 64)
		b, bErr := strconv.ParseFloat(target, 64)
		if aErr != nil || bErr != nil {
			return true, fmt.Sprintf("数值比较失败，当前值=%q 阈值=%q", actual, target)
		}
		var ok bool
		switch cond {
		case "gt", ">":
			ok = a > b
		case "gte", ">=":
			ok = a >= b
		case "lt", "<":
			ok = a < b
		case "lte", "<=":
			ok = a <= b
		}
		return ok, fmt.Sprintf("当前值 %s %s %s: %t", actual, cond, target, ok)
	default:
		ok := actual == target
		return ok, fmt.Sprintf("未知条件 %q，按等于比较: %t", condition, ok)
	}
}

func truncateForLog(base, suffix string, maxLen int) string {
	if maxLen <= 0 {
		return base
	}
	combined := base + suffix
	if len(combined) <= maxLen {
		return combined
	}
	if len(base) >= maxLen {
		return base[:maxLen]
	}
	remain := maxLen - len(base)
	if remain <= 0 {
		return base
	}
	return base + suffix[:remain]
}

func runHealthTriggerActions(cfg *store.HealthCheck) (string, string) {
	actions, err := parseHealthTriggerActions(cfg.TriggerActions)
	if err != nil {
		msg := "解析触发操作失败: " + err.Error()
		return msg, msg
	}
	if len(actions) == 0 {
		return "", ""
	}
	var summaries []string
	var notifySummaries []string
	for i, action := range actions {
		if action.Enabled != nil && !*action.Enabled {
			continue
		}
		label := strings.TrimSpace(action.Name)
		if label == "" {
			label = fmt.Sprintf("动作%d", i+1)
		}
		timeout := action.TimeoutSec
		if timeout <= 0 {
			timeout = cfg.TimeoutSec
		}
		if timeout <= 0 {
			timeout = 10
		}
		if timeout > 300 {
			timeout = 300
		}
		start := time.Now()
		output, runErr := runHealthTriggerAction(action, time.Duration(timeout)*time.Second)
		durationMs := time.Since(start).Milliseconds()
		status := "成功"
		if runErr != nil {
			status = "失败"
			output = strings.TrimSpace(runErr.Error() + "\n" + output)
		}
		output = strings.TrimSpace(output)
		if output != "" {
			summaries = append(summaries, fmt.Sprintf("[%s] %s (%dms)\n%s", label, status, durationMs, output))
			notifyOutput := truncateHealthNotifyOutput(output, action.NotifyMaxChars)
			notifySummaries = append(notifySummaries, fmt.Sprintf("[%s] %s (%dms)\n%s", label, status, durationMs, notifyOutput))
		} else {
			summaries = append(summaries, fmt.Sprintf("[%s] %s (%dms)", label, status, durationMs))
			notifySummaries = append(notifySummaries, fmt.Sprintf("[%s] %s (%dms)", label, status, durationMs))
		}
	}
	return strings.Join(summaries, "\n\n"), strings.Join(notifySummaries, "\n\n")
}

func truncateHealthNotifyOutput(output string, maxChars int) string {
	output = cleanHealthNotifyOutput(output)
	if output == "" {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 2000
	}
	if len(output) <= maxChars {
		return output
	}
	return output[:maxChars] + fmt.Sprintf("\n...(已截断，飞书通知仅展示该命令前 %d 字符；完整输出请查看检查日志详情)", maxChars)
}

func cleanHealthNotifyOutput(output string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "File:"):
			continue
		case strings.HasPrefix(trimmed, "Build ID:"):
			continue
		case strings.HasPrefix(trimmed, "Type:"):
			continue
		default:
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseHealthTriggerActions(raw string) ([]healthTriggerAction, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var actions []healthTriggerAction
	if err := json.Unmarshal([]byte(raw), &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

func runHealthTriggerAction(action healthTriggerAction, timeout time.Duration) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action.Type)) {
	case "command", "shell", "":
		if strings.TrimSpace(action.Command) == "" {
			return "", fmt.Errorf("命令为空")
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", action.Command)
		out, err := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			return string(out), fmt.Errorf("执行超时")
		}
		return string(out), err
	case "http":
		return runHealthHTTPAction(action, timeout)
	default:
		return "", fmt.Errorf("不支持的动作类型 %q", action.Type)
	}
}

func runHealthHTTPAction(action healthTriggerAction, timeout time.Duration) (string, error) {
	if strings.TrimSpace(action.URL) == "" {
		return "", fmt.Errorf("URL为空")
	}
	method := strings.ToUpper(strings.TrimSpace(action.Method))
	if method == "" {
		method = "GET"
	}
	client := &http.Client{Timeout: timeout}
	var body io.Reader
	if action.Body != "" {
		body = strings.NewReader(action.Body)
	}
	req, err := http.NewRequest(method, action.URL, body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(action.HeadersJSON) != "" && strings.TrimSpace(action.HeadersJSON) != "{}" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(action.HeadersJSON), &headers); err != nil {
			return "", fmt.Errorf("请求头JSON无效: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	output := fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(bodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return output, fmt.Errorf("HTTP状态码异常")
	}
	return output, nil
}

// TestHealthCheck executes a single health check and returns the result.
func TestHealthCheck(cfg *store.HealthCheck) store.HealthCheckLog {
	return executeHealthCheckWithMetricState(cfg, &healthMetricState{Field: cfg.AlertField})
}
