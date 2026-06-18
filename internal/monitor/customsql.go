package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

type CustomSQLManager struct {
	store        *store.Store
	dispatcher   *notify.Dispatcher
	eventBus     *EventBus
	mu           sync.Mutex
	monitors     map[int64]*customSQLMon
	metricStates map[int64]*healthMetricState
}

type customSQLMon struct {
	cancel context.CancelFunc
}

func NewCustomSQLManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *CustomSQLManager {
	return &CustomSQLManager{
		store:        s,
		dispatcher:   d,
		eventBus:     eb,
		monitors:     make(map[int64]*customSQLMon),
		metricStates: make(map[int64]*healthMetricState),
	}
}

func (m *CustomSQLManager) StartAll() error {
	checks, err := m.store.ListCustomSQLChecks()
	if err != nil {
		return fmt.Errorf("list custom sql checks: %w", err)
	}
	for _, c := range checks {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("failed to start custom sql check for %s: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *CustomSQLManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	cfg, err := m.store.GetCustomSQLCheck(id)
	if err != nil {
		return fmt.Errorf("get custom sql check %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = &customSQLMon{cancel: cancel}

	go m.runMonitor(ctx, cfg)
	log.Printf("started custom sql check for %s", cfg.Name)
	return nil
}

func (m *CustomSQLManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mon, ok := m.monitors[id]; ok {
		mon.cancel()
		delete(m.monitors, id)
		delete(m.metricStates, id)
		log.Printf("stopped custom sql check id=%d", id)
	}
}

func (m *CustomSQLManager) Restart(id int64) error {
	m.Stop(id)
	time.Sleep(100 * time.Millisecond)
	return m.Start(id)
}

func (m *CustomSQLManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mon := range m.monitors {
		mon.cancel()
		delete(m.monitors, id)
		delete(m.metricStates, id)
	}
	log.Println("all custom sql checks stopped")
}

func (m *CustomSQLManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *CustomSQLManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *CustomSQLManager) emit(typ string, checkID int64, name, message string, data interface{}) {
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

func (m *CustomSQLManager) runMonitor(ctx context.Context, cfg *store.CustomSQLCheck) {
	interval := time.Duration(cfg.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
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

func (m *CustomSQLManager) doCheck(cfg *store.CustomSQLCheck) {
	m.emit("custom_sql_checking", cfg.ID, cfg.Name, "检查中...", nil)

	logEntry := TestCustomSQLCheckWithMetricState(m.store, cfg, m.metricState(cfg.ID, cfg.ResultField))
	if id, err := m.store.InsertCustomSQLLog(&logEntry); err == nil {
		logEntry.ID = id
	} else {
		log.Printf("[CustomSQL %s] insert log error: %v", cfg.Name, err)
	}

	m.emit("custom_sql_result", cfg.ID, cfg.Name, logEntry.Message, logEntry)

	if logEntry.Status == "alert" {
		notifyEveryRun := strings.EqualFold(strings.TrimSpace(cfg.Condition), "always")
		if cfg.NotifyEnabled && (notifyEveryRun || !m.isAlertNotified(cfg.ID)) {
			if err := m.dispatcher.SendScopedNotifications("custom_sql", cfg.ID, buildCustomSQLMessage(cfg, &logEntry)); err != nil {
				log.Printf("[CustomSQL %s] notification failed: %v", cfg.Name, err)
				m.emit("custom_sql_error", cfg.ID, cfg.Name, fmt.Sprintf("通知发送失败: %v", err), nil)
			} else {
				m.emit("custom_sql_notified", cfg.ID, cfg.Name, "已发送告警通知", nil)
			}
			if !notifyEveryRun {
				m.setAlertNotified(cfg.ID, true)
			}
		}
		return
	}

	if logEntry.Status == "ok" && m.isAlertNotified(cfg.ID) {
		m.setAlertNotified(cfg.ID, false)
		if cfg.NotifyEnabled && cfg.RecoveryNotify {
			recoveryMsg := fmt.Sprintf("自定义 SQL 恢复通知\n\n规则: %s\n数据库: %s\n当前值: %s\n状态: 已恢复正常", cfg.Name, cfg.DatabaseName, logEntry.Value)
			if err := m.dispatcher.SendScopedNotifications("custom_sql", cfg.ID, recoveryMsg); err != nil {
				log.Printf("[CustomSQL %s] recovery notification failed: %v", cfg.Name, err)
			} else {
				m.emit("custom_sql_notified", cfg.ID, cfg.Name, "已发送恢复通知", nil)
			}
		}
	}
}

func (m *CustomSQLManager) metricState(id int64, field string) *healthMetricState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if field == "" {
		field = "first_column"
	}
	st := m.metricStates[id]
	if st == nil || st.Field != field {
		st = &healthMetricState{Field: field}
		m.metricStates[id] = st
	}
	return st
}

func (m *CustomSQLManager) settingKey(id int64) string {
	return fmt.Sprintf("custom_sql_alert_%d", id)
}

func (m *CustomSQLManager) isAlertNotified(id int64) bool {
	return m.store.GetSetting(m.settingKey(id)) == "1"
}

func (m *CustomSQLManager) setAlertNotified(id int64, v bool) {
	if v {
		m.store.SetSetting(m.settingKey(id), "1")
	} else {
		m.store.SetSetting(m.settingKey(id), "")
	}
}

func TestCustomSQLCheck(s *store.Store, cfg *store.CustomSQLCheck) store.CustomSQLLog {
	return TestCustomSQLCheckWithMetricState(s, cfg, &healthMetricState{Field: cfg.ResultField})
}

func TestCustomSQLCheckWithMetricState(s *store.Store, cfg *store.CustomSQLCheck, metricState *healthMetricState) store.CustomSQLLog {
	start := time.Now()
	result := store.CustomSQLLog{
		CheckID:       cfg.ID,
		CheckName:     cfg.Name,
		DatabaseID:    cfg.DatabaseID,
		DatabaseName:  cfg.DatabaseName,
		ExpectedValue: cfg.ExpectedValue,
		Condition:     cfg.Condition,
	}
	defer func() {
		result.DurationMs = time.Since(start).Milliseconds()
	}()

	if err := ValidateCustomSQL(cfg.SQLText); err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}

	dbCfg, err := s.GetDatabase(cfg.DatabaseID)
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("数据库配置不存在: %v", err)
		result.Message = result.Error
		return result
	}
	if cfg.DatabaseName == "" {
		result.DatabaseName = dbCfg.Name
	}

	db, err := sql.Open("mysql", customSQLDSN(dbCfg, cfg.DBName))
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.Message = result.Error
		return result
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	value, err := querySelectedValue(ctx, db, strings.TrimSpace(strings.TrimSuffix(cfg.SQLText, ";")), cfg.ResultField)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}
	result.Value = value

	if strings.EqualFold(strings.TrimSpace(cfg.Condition), "changed") {
		key := fmt.Sprintf("custom_sql_last_value_%d", cfg.ID)
		last := s.GetSetting(key)
		s.SetSetting(key, value)
		if last != "" && last != value {
			result.Status = "alert"
			result.Message = fmt.Sprintf("当前值从 %q 变为 %q", last, value)
		} else {
			result.Status = "ok"
			if last == "" {
				result.Message = "已记录首次结果"
			} else {
				result.Message = "当前值未变化"
			}
		}
		return result
	}

	matched, reason := EvaluateCustomSQLRule(value, cfg, metricState)
	if matched {
		result.Status = "alert"
		result.Message = reason
	} else {
		result.Status = "ok"
		result.Message = reason
	}
	return result
}

func EvaluateCustomSQLRule(value string, cfg *store.CustomSQLCheck, st *healthMetricState) (bool, string) {
	strategy := strings.ToLower(strings.TrimSpace(cfg.AlertStrategy))
	if strategy == "" {
		strategy = "threshold"
	}
	consecutive := cfg.AlertConsecutive
	if consecutive <= 0 {
		consecutive = 1
	}
	field := strings.TrimSpace(cfg.ResultField)
	if field == "" {
		field = "第一列"
	}
	thresholdConfigured := strings.TrimSpace(cfg.ExpectedValue) != "" || cfg.Condition == "empty" || cfg.Condition == "not_empty" || cfg.Condition == "always"
	thresholdMatched, thresholdMsg := EvaluateCustomSQLCondition(value, cfg.Condition, cfg.ExpectedValue)

	switch strategy {
	case "threshold":
		return thresholdMatched, fmt.Sprintf("%s %s %s: %s", field, cfg.Condition, cfg.ExpectedValue, thresholdMsg)
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
		return matched, fmt.Sprintf("%s %s %s 连续命中 %d/%d 次: %s", field, cfg.Condition, cfg.ExpectedValue, st.ConsecutiveMatched, consecutive, thresholdMsg)
	case "increase", "sudden_increase":
		current, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return true, fmt.Sprintf("%s 突增判断需要数值，当前值=%q", field, value)
		}
		if st == nil {
			st = &healthMetricState{}
		}
		matched, msg := evaluateCustomSQLIncreaseRule(current, field, cfg, st, thresholdConfigured, thresholdMatched)
		st.HasLast = true
		st.LastValue = current
		return matched, msg
	case "continuous_increase":
		current, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return true, fmt.Sprintf("%s 连续上升判断需要数值，当前值=%q", field, value)
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
		msg := fmt.Sprintf("%s 连续上升 %d/%d 次，上次 %.4f，本次 %.4f", field, st.ConsecutiveRise, consecutive, st.LastValue, current)
		if thresholdConfigured {
			msg += "，阈值条件: " + thresholdMsg
		}
		st.HasLast = true
		st.LastValue = current
		return matched, msg
	default:
		return thresholdMatched, fmt.Sprintf("未知策略 %q，按单次阈值判断: %s", cfg.AlertStrategy, thresholdMsg)
	}
}

func evaluateCustomSQLIncreaseRule(current float64, field string, cfg *store.CustomSQLCheck, st *healthMetricState, thresholdConfigured, thresholdMatched bool) (bool, string) {
	if !st.HasLast {
		return false, fmt.Sprintf("%s 突增等待下一次采样，当前 %.4f", field, current)
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
	msg := fmt.Sprintf("%s 突增判断，上次 %.4f，本次 %.4f，变化 %.4f，变化率 %s", field, st.LastValue, current, delta, percentText)
	if cfg.AlertDeltaValue != "" {
		msg += "，变化量阈值 " + cfg.AlertDeltaValue
	}
	if cfg.AlertDeltaPercent != "" {
		msg += "，变化率阈值 " + cfg.AlertDeltaPercent + "%"
	}
	if thresholdConfigured {
		msg += "，当前值阈值: " + fmt.Sprintf("%s %s %s", field, cfg.Condition, cfg.ExpectedValue)
	}
	return matched, msg
}

func customSQLDSN(db *store.Database, dbName string) string {
	schema := strings.TrimSpace(dbName)
	if schema == "" {
		schema = "performance_schema"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", db.User, db.Password, db.Host, db.Port, url.PathEscape(schema))
}

func querySelectedValue(ctx context.Context, db *sql.DB, sqlText, resultField string) (string, error) {
	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", nil
	}
	selectedIndex, err := resolveCustomSQLResultIndex(cols, resultField)
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", nil
	}

	raw := make([]sql.RawBytes, len(cols))
	dest := make([]any, len(cols))
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return "", err
	}
	if raw[selectedIndex] == nil {
		return "", nil
	}
	return string(raw[selectedIndex]), nil
}

func resolveCustomSQLResultIndex(cols []string, resultField string) (int, error) {
	field := strings.TrimSpace(resultField)
	if field == "" {
		return 0, nil
	}
	if idx, err := strconv.Atoi(field); err == nil {
		if idx <= 0 || idx > len(cols) {
			return 0, fmt.Errorf("结果字段序号 %d 超出范围，当前返回 %d 列", idx, len(cols))
		}
		return idx - 1, nil
	}
	for i, col := range cols {
		if strings.EqualFold(strings.TrimSpace(col), field) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("结果字段 %q 不存在，当前返回列: %s", field, strings.Join(cols, ", "))
}

func ValidateCustomSQL(sqlText string) error {
	stmt := strings.TrimSpace(sqlText)
	if stmt == "" {
		return fmt.Errorf("SQL 不能为空")
	}
	stmt = strings.TrimSuffix(stmt, ";")
	if strings.Contains(stmt, ";") {
		return fmt.Errorf("只允许单条查询 SQL")
	}
	lower := strings.ToLower(strings.TrimSpace(stmt))
	allowed := []string{"select ", "show ", "with ", "explain ", "select\n", "show\n", "with\n", "explain\n"}
	for _, prefix := range allowed {
		if strings.HasPrefix(lower, prefix) || lower == strings.TrimSpace(prefix) {
			return nil
		}
	}
	return fmt.Errorf("只允许 SELECT / SHOW / WITH / EXPLAIN 查询")
}

func EvaluateCustomSQLCondition(value, condition, expected string) (bool, string) {
	cond := strings.ToLower(strings.TrimSpace(condition))
	actual := strings.TrimSpace(value)
	target := strings.TrimSpace(expected)

	switch cond {
	case "", "always":
		return true, "始终上报"
	case "empty":
		return actual == "", fmt.Sprintf("当前值为空: %t", actual == "")
	case "not_empty":
		return actual != "", fmt.Sprintf("当前值非空: %t", actual != "")
	case "contains":
		ok := strings.Contains(actual, target)
		return ok, fmt.Sprintf("当前值 contains %q: %t", target, ok)
	case "not_contains":
		ok := !strings.Contains(actual, target)
		return ok, fmt.Sprintf("当前值 not contains %q: %t", target, ok)
	case "changed":
		return false, "当前值未变化"
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
			return true, fmt.Sprintf("数值比较失败，当前值=%q 期望=%q", actual, target)
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

func buildCustomSQLMessage(cfg *store.CustomSQLCheck, logEntry *store.CustomSQLLog) string {
	if strings.TrimSpace(cfg.MessageTemplate) != "" {
		msg := cfg.MessageTemplate
		replacements := map[string]string{
			"{{name}}":           cfg.Name,
			"{{database}}":       cfg.DatabaseName,
			"{{db_name}}":        cfg.DBName,
			"{{result_field}}":   cfg.ResultField,
			"{{alert_strategy}}": cfg.AlertStrategy,
			"{{value}}":          logEntry.Value,
			"{{expected}}":       cfg.ExpectedValue,
			"{{condition}}":      cfg.Condition,
			"{{message}}":        logEntry.Message,
			"{{sql}}":            cfg.SQLText,
			"{{duration_ms}}":    fmt.Sprintf("%d", logEntry.DurationMs),
			"{{detected_at}}":    time.Now().Format("2006-01-02 15:04:05"),
			"{{status}}":         logEntry.Status,
			"{{database_id}}":    fmt.Sprintf("%d", cfg.DatabaseID),
			"{{custom_sql_id}}":  fmt.Sprintf("%d", cfg.ID),
		}
		for k, v := range replacements {
			msg = strings.ReplaceAll(msg, k, v)
		}
		return msg
	}

	resultField := strings.TrimSpace(cfg.ResultField)
	if resultField == "" {
		resultField = "第一列"
	}
	return fmt.Sprintf("自定义 SQL 告警\n\n规则: %s\n数据库: %s\n执行库: %s\n结果字段: %s\n异常策略: %s\n条件: %s %s\n当前值: %s\n结果: %s\n耗时: %dms\n\nSQL:\n%s",
		cfg.Name, cfg.DatabaseName, cfg.DBName, resultField, cfg.AlertStrategy, cfg.Condition, cfg.ExpectedValue, logEntry.Value, logEntry.Message, logEntry.DurationMs, cfg.SQLText)
}
