package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
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
	metricStates map[string]*healthMetricState
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
		metricStates: make(map[string]*healthMetricState),
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
		m.deleteMetricStatesLocked(id)
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
		m.deleteMetricStatesLocked(id)
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

	logEntry := TestCustomSQLCheckWithMetricStateProvider(m.store, cfg, func(ruleKey, field string) *healthMetricState {
		return m.metricState(cfg.ID, ruleKey, field)
	})
	if id, err := m.store.InsertCustomSQLLog(&logEntry); err == nil {
		logEntry.ID = id
	} else {
		log.Printf("[CustomSQL %s] insert log error: %v", cfg.Name, err)
	}

	m.emit("custom_sql_result", cfg.ID, cfg.Name, logEntry.Message, logEntry)

	if logEntry.Status == "alert" {
		return
	}

	if logEntry.Status == "ok" && m.isAlertNotified(cfg.ID) {
		m.setAlertNotified(cfg.ID, false)
	}
}

func (m *CustomSQLManager) metricState(id int64, ruleKey, field string) *healthMetricState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if field == "" {
		field = "first_column"
	}
	key := fmt.Sprintf("%d:%s:%s", id, ruleKey, field)
	st := m.metricStates[key]
	if st == nil || st.Field != field {
		st = &healthMetricState{Field: field}
		m.metricStates[key] = st
	}
	return st
}

func (m *CustomSQLManager) deleteMetricStatesLocked(id int64) {
	prefix := fmt.Sprintf("%d:", id)
	for key := range m.metricStates {
		if strings.HasPrefix(key, prefix) {
			delete(m.metricStates, key)
		}
	}
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
	return TestCustomSQLCheckWithMetricStateProvider(s, cfg, func(ruleKey, field string) *healthMetricState {
		if metricState != nil {
			metricState.Field = field
		}
		return metricState
	})
}

func TestCustomSQLCheckWithMetricStateProvider(s *store.Store, cfg *store.CustomSQLCheck, metricState func(ruleKey, field string) *healthMetricState) store.CustomSQLLog {
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

	sqlText := strings.TrimSpace(strings.TrimSuffix(cfg.SQLText, ";"))
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("启动只读事务失败: %v", err)
		result.Message = result.Error
		return result
	}
	defer tx.Rollback()
	cols, values, err := queryFirstRowValues(ctx, tx, sqlText)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}
	if err := tx.Commit(); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("提交只读查询失败: %v", err)
		result.Message = result.Error
		return result
	}
	rules := customSQLAlertRulesFromConfig(cfg)
	if len(rules) == 0 {
		rules = []customSQLAlertRule{{
			ResultField:      cfg.ResultField,
			Strategy:         cfg.AlertStrategy,
			Condition:        cfg.Condition,
			ExpectedValue:    cfg.ExpectedValue,
			DeltaValue:       cfg.AlertDeltaValue,
			DeltaPercent:     cfg.AlertDeltaPercent,
			AlertConsecutive: cfg.AlertConsecutive,
		}}
	}

	var summaries []string
	for i, rule := range rules {
		value, err := selectedCustomSQLValue(cols, values, rule.ResultField)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			result.Message = err.Error()
			return result
		}
		ruleName := strings.TrimSpace(rule.Name)
		if ruleName == "" {
			ruleName = strings.TrimSpace(rule.ResultField)
		}
		if ruleName == "" {
			ruleName = "第一列"
		}
		ruleKey := fmt.Sprintf("%d:%s:%s", i, ruleName, rule.ResultField)
		ruleCfg := customSQLCheckForAlertRule(cfg, rule)
		fieldName := strings.TrimSpace(ruleCfg.ResultField)
		if fieldName == "" {
			fieldName = "第一列"
		}
		summaries = append(summaries, fmt.Sprintf("%s=%s", fieldName, value))

		if strings.EqualFold(strings.TrimSpace(ruleCfg.Condition), "changed") {
			key := fmt.Sprintf("custom_sql_last_value_%d_%s", cfg.ID, safeCustomSQLSettingSuffix(ruleKey))
			if len(rules) == 1 && strings.TrimSpace(cfg.AlertRules) == "" {
				key = fmt.Sprintf("custom_sql_last_value_%d", cfg.ID)
			}
			last := s.GetSetting(key)
			s.SetSetting(key, value)
			result.Value = value
			result.ExpectedValue = ruleCfg.ExpectedValue
			result.Condition = ruleCfg.Condition
			if last != "" && last != value {
				result.Status = "alert"
				result.Message = fmt.Sprintf("命中规则: %s\n字段: %s\n当前值: %s\n上次值: %s\n策略: 发生变化", ruleName, fieldName, value, last)
				return result
			}
			continue
		}

		st := (*healthMetricState)(nil)
		if metricState != nil {
			st = metricState(ruleKey, fieldName)
		}
		matched, reason := EvaluateCustomSQLRule(value, ruleCfg, st)
		if matched {
			result.Status = "alert"
			result.Value = value
			result.ExpectedValue = ruleCfg.ExpectedValue
			result.Condition = ruleCfg.Condition
			result.Message = fmt.Sprintf("命中规则: %s\n%s", ruleName, reason)
			return result
		}
	}

	result.Status = "ok"
	result.Value = strings.Join(summaries, "; ")
	if result.Value == "" {
		result.Value = strings.Join(values, "; ")
	}
	result.Message = "所有规则正常"
	return result
}

type customSQLAlertRule struct {
	Name              string `json:"name"`
	ResultField       string `json:"result_field"`
	Field             string `json:"field,omitempty"`
	Strategy          string `json:"strategy"`
	AlertStrategy     string `json:"alert_strategy,omitempty"`
	Condition         string `json:"condition"`
	ExpectedValue     string `json:"expected_value"`
	Value             string `json:"value,omitempty"`
	DeltaValue        string `json:"delta_value"`
	DeltaPercent      string `json:"delta_percent"`
	AlertDeltaValue   string `json:"alert_delta_value,omitempty"`
	AlertDeltaPercent string `json:"alert_delta_percent,omitempty"`
	Consecutive       int    `json:"consecutive"`
	AlertConsecutive  int    `json:"alert_consecutive,omitempty"`
}

func customSQLAlertRulesFromConfig(cfg *store.CustomSQLCheck) []customSQLAlertRule {
	var rules []customSQLAlertRule
	if strings.TrimSpace(cfg.AlertRules) != "" && strings.TrimSpace(cfg.AlertRules) != "[]" {
		if err := json.Unmarshal([]byte(cfg.AlertRules), &rules); err != nil {
			rules = nil
		}
	}
	normalized := make([]customSQLAlertRule, 0, len(rules)+1)
	for _, rule := range rules {
		if strings.TrimSpace(rule.ResultField) == "" {
			rule.ResultField = strings.TrimSpace(rule.Field)
		}
		if strings.TrimSpace(rule.ExpectedValue) == "" {
			rule.ExpectedValue = rule.Value
		}
		if strings.TrimSpace(rule.Strategy) == "" {
			rule.Strategy = strings.TrimSpace(rule.AlertStrategy)
		}
		if strings.TrimSpace(rule.Strategy) == "" {
			rule.Strategy = "threshold"
		}
		if strings.TrimSpace(rule.Condition) == "" {
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
	return []customSQLAlertRule{{
		ResultField:      cfg.ResultField,
		Strategy:         cfg.AlertStrategy,
		Condition:        cfg.Condition,
		ExpectedValue:    cfg.ExpectedValue,
		DeltaValue:       cfg.AlertDeltaValue,
		DeltaPercent:     cfg.AlertDeltaPercent,
		AlertConsecutive: cfg.AlertConsecutive,
	}}
}

func customSQLCheckForAlertRule(base *store.CustomSQLCheck, rule customSQLAlertRule) *store.CustomSQLCheck {
	copied := *base
	copied.ResultField = rule.ResultField
	copied.AlertStrategy = rule.Strategy
	copied.Condition = rule.Condition
	copied.ExpectedValue = rule.ExpectedValue
	copied.AlertDeltaValue = rule.DeltaValue
	copied.AlertDeltaPercent = rule.DeltaPercent
	copied.AlertConsecutive = rule.Consecutive
	if copied.AlertStrategy == "" {
		copied.AlertStrategy = "threshold"
	}
	if copied.Condition == "" {
		copied.Condition = "gt"
	}
	if copied.AlertConsecutive <= 0 {
		copied.AlertConsecutive = 1
	}
	return &copied
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

type customSQLQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func querySelectedValue(ctx context.Context, q customSQLQueryer, sqlText, resultField string) (string, error) {
	cols, values, err := queryFirstRowValues(ctx, q, sqlText)
	if err != nil {
		return "", err
	}
	return selectedCustomSQLValue(cols, values, resultField)
}

func queryFirstRowValues(ctx context.Context, q customSQLQueryer, sqlText string) ([]string, []string, error) {
	rows, err := q.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	if len(cols) == 0 {
		return cols, nil, nil
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return cols, nil, err
		}
		return cols, nil, nil
	}

	raw := make([]sql.RawBytes, len(cols))
	dest := make([]any, len(cols))
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return cols, nil, err
	}
	values := make([]string, len(cols))
	for i := range raw {
		if raw[i] != nil {
			values[i] = string(raw[i])
		}
	}
	return cols, values, nil
}

func selectedCustomSQLValue(cols, values []string, resultField string) (string, error) {
	if len(cols) == 0 || len(values) == 0 {
		return "", nil
	}
	selectedIndex, err := resolveCustomSQLResultIndex(cols, resultField)
	if err != nil {
		return "", err
	}
	if selectedIndex >= len(values) {
		return "", nil
	}
	return values[selectedIndex], nil
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

func safeCustomSQLSettingSuffix(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range input {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "default"
	}
	return out
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
	tokens := customSQLTokens(lower)
	if len(tokens) == 0 {
		return fmt.Errorf("SQL 不能为空")
	}
	switch tokens[0] {
	case "select", "show", "with", "explain":
	default:
		return fmt.Errorf("只允许 SELECT / SHOW / WITH / EXPLAIN 查询")
	}
	if err := validateCustomSQLReadOnly(lower, tokens); err != nil {
		return err
	}
	if err := validateCustomSQLScanRisk(lower, tokens); err != nil {
		return err
	}
	return nil
}

func validateCustomSQLReadOnly(lower string, tokens []string) error {
	blockedPhrases := []string{
		" into outfile", " into dumpfile", " into @", " for update",
		" lock in share mode", " procedure analyse",
	}
	padded := " " + customSQLSpaceRE.ReplaceAllString(lower, " ") + " "
	for _, phrase := range blockedPhrases {
		if strings.Contains(padded, phrase) {
			return fmt.Errorf("自定义 SQL 只能查询，禁止使用 %s", strings.TrimSpace(strings.ToUpper(phrase)))
		}
	}
	blockedTokens := map[string]string{
		"insert": "INSERT", "update": "UPDATE", "delete": "DELETE", "replace": "REPLACE",
		"truncate": "TRUNCATE", "drop": "DROP", "alter": "ALTER", "create": "CREATE",
		"rename": "RENAME", "grant": "GRANT", "revoke": "REVOKE", "call": "CALL",
		"load": "LOAD", "handler": "HANDLER", "lock": "LOCK", "unlock": "UNLOCK",
		"analyze": "ANALYZE", "optimize": "OPTIMIZE", "repair": "REPAIR", "kill": "KILL",
		"set": "SET", "reset": "RESET", "use": "USE", "flush": "FLUSH", "commit": "COMMIT",
		"rollback": "ROLLBACK", "start": "START", "begin": "BEGIN", "do": "DO",
		"get_lock": "GET_LOCK", "release_lock": "RELEASE_LOCK", "sleep": "SLEEP",
	}
	for _, token := range tokens {
		if keyword, ok := blockedTokens[token]; ok {
			return fmt.Errorf("自定义 SQL 只能查询，禁止使用 %s", keyword)
		}
	}
	return nil
}

func validateCustomSQLScanRisk(lower string, tokens []string) error {
	if tokens[0] != "select" && tokens[0] != "with" {
		return nil
	}
	normalized := " " + customSQLSpaceRE.ReplaceAllString(lower, " ") + " "
	if !strings.Contains(normalized, " from ") {
		return nil
	}
	if regexp.MustCompile(`(?i)\bselect\s+\*`).FindStringIndex(lower) != nil {
		return fmt.Errorf("禁止 SELECT *，请只查询需要监控的字段")
	}
	hasLimit := tokenExists(tokens, "limit")
	hasGroupBy := strings.Contains(normalized, " group by ")
	if hasLimit {
		return nil
	}
	if customSQLLooksSingleRowAggregate(lower) && !hasGroupBy {
		return nil
	}
	return fmt.Errorf("普通 SELECT 必须带 LIMIT，避免监控 SQL 拉取全量数据；聚合单行查询如 COUNT/SUM 可不带 LIMIT")
}

func customSQLLooksSingleRowAggregate(lower string) bool {
	aggregateRE := regexp.MustCompile(`(?i)\b(count|sum|avg|min|max)\s*\(`)
	return aggregateRE.FindStringIndex(lower) != nil
}

var customSQLSpaceRE = regexp.MustCompile(`\s+`)

func customSQLTokens(lower string) []string {
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tokenExists(tokens []string, target string) bool {
	for _, token := range tokens {
		if token == target {
			return true
		}
	}
	return false
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
