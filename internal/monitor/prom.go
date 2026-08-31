package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

// PromManager 采集 Prometheus 文本格式端点，按规则求值并告警。
//
// 一个采集器覆盖五个维度，靠的是"端点即数据源"这个共性：
//
//	node_exporter   -> 主机 USE（CPU / 内存 / swap / 磁盘 / 网络 / fd）
//	cAdvisor        -> 容器状态与资源
//	应用 /metrics    -> RED（QPS / 错误率 / 耗时）+ 连接池 + 业务指标
//	redis_exporter  -> 中间件
//	任意自定义端点   -> 其它
//
// 不引入 PromQL：这里只做"选指标 + 聚合 + 阈值/变化率"，
// 复杂查询交给时序库，本工具的定位是告警而不是指标仓库。
type PromManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus

	mu       sync.Mutex
	monitors map[int64]context.CancelFunc

	stateMu sync.Mutex
	states  map[int64]*promMetricState

	client *http.Client
}

// promMetricState 保存跨轮次的状态：连续命中次数用于 sustained 策略，
// 上一次的值用于变化量/变化率策略。
type promMetricState struct {
	ConsecutiveMatched int
	LastValue          float64
	HasLast            bool
}

func NewPromManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *PromManager {
	return &PromManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]context.CancelFunc),
		states:     make(map[int64]*promMetricState),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

func (m *PromManager) StartAll() error {
	targets, err := m.store.ListPromTargets()
	if err != nil {
		return fmt.Errorf("list prom targets: %w", err)
	}
	for _, t := range targets {
		if t.Enabled {
			if err := m.Start(t.ID); err != nil {
				log.Printf("[prom] start target %s failed: %v", t.Name, err)
			}
		}
	}
	return nil
}

func (m *PromManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	target, err := m.store.GetPromTarget(id)
	if err != nil {
		return fmt.Errorf("get prom target %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = cancel

	go m.runTarget(ctx, target)
	log.Printf("[prom] started target %s (%s)", target.Name, target.URL)
	return nil
}

func (m *PromManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.monitors[id]; ok {
		cancel()
		delete(m.monitors, id)
	}
}

func (m *PromManager) Restart(id int64) error {
	m.Stop(id)
	return m.Start(id)
}

func (m *PromManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.monitors {
		cancel()
		delete(m.monitors, id)
	}
}

func (m *PromManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *PromManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *PromManager) emit(typ string, targetID int64, name, message string, data interface{}) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(MonitorEvent{
		Type:       typ,
		DatabaseID: targetID,
		DBName:     name,
		Message:    message,
		Timestamp:  time.Now(),
		Data:       data,
	})
}

func (m *PromManager) runTarget(ctx context.Context, target *store.PromTarget) {
	interval := time.Duration(target.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.scrapeOnce(ctx, target.ID)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[prom] stopped target %d", target.ID)
			return
		case <-ticker.C:
			m.scrapeOnce(ctx, target.ID)
		}
	}
}

// scrapeOnce 抓一次端点，然后把该端点下所有启用的规则跑一遍。
// 一次抓取服务多条规则，避免每条规则各发一次 HTTP。
func (m *PromManager) scrapeOnce(ctx context.Context, targetID int64) {
	start := time.Now()

	target, err := m.store.GetPromTarget(targetID)
	if err != nil {
		log.Printf("[prom] reload target %d failed: %v", targetID, err)
		return
	}
	if !target.Enabled {
		return
	}

	checks, err := m.store.ListPromChecks(&targetID)
	if err != nil {
		log.Printf("[prom] list checks for target %d failed: %v", targetID, err)
		return
	}

	families, scrapeErr := m.scrape(ctx, target)
	elapsed := time.Since(start).Milliseconds()

	if scrapeErr != nil {
		m.emit("prom_scrape_error", target.ID, target.Name, scrapeErr.Error(), nil)
		// 抓取失败对每条启用的规则都记一条错误，便于在日志页定位
		for _, c := range checks {
			if !c.Enabled {
				continue
			}
			m.store.InsertPromAlertLog(&store.PromAlertLog{
				CheckID: c.ID, CheckName: c.Name,
				TargetID: target.ID, TargetName: target.Name,
				Dimension: c.Dimension, Severity: c.Severity,
				Status: "error", Metric: c.Metric,
				Error: scrapeErr.Error(), DurationMs: elapsed,
			})
		}
		return
	}

	for i := range checks {
		if !checks[i].Enabled {
			continue
		}
		m.evaluate(target, &checks[i], families, elapsed)
	}
}

func (m *PromManager) scrape(ctx context.Context, target *store.PromTarget) (map[string][]promSample, error) {
	timeout := time.Duration(target.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	for k, v := range parseJSONStringMap(target.HeadersJSON) {
		req.Header.Set(k, v)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", target.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: HTTP %d", target.URL, resp.StatusCode)
	}

	// 限制读取体积，避免异常端点拖垮内存
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return parsePromText(string(body)), nil
}

// evaluate 对单条规则求值并按需告警。
func (m *PromManager) evaluate(target *store.PromTarget, check *store.PromCheck, families map[string][]promSample, elapsed int64) {
	value, err := computePromValue(check, families)
	if err != nil {
		m.store.InsertPromAlertLog(&store.PromAlertLog{
			CheckID: check.ID, CheckName: check.Name,
			TargetID: target.ID, TargetName: target.Name,
			Dimension: check.Dimension, Severity: check.Severity,
			Status: "error", Metric: check.Metric,
			Error: err.Error(), DurationMs: elapsed,
		})
		m.emit("prom_check_error", target.ID, target.Name,
			fmt.Sprintf("%s: %v", check.Name, err), nil)
		return
	}

	state := m.metricState(check.ID)
	matched, reason := evaluatePromRule(value, check, state)
	state.LastValue = value
	state.HasLast = true

	valueStr := formatPromValue(value)
	prevStatus, hasPrev, _ := m.store.LastPromAlertStatus(check.ID)

	if matched {
		message := renderPromMessage(target, check, valueStr, reason)
		m.store.InsertPromAlertLog(&store.PromAlertLog{
			CheckID: check.ID, CheckName: check.Name,
			TargetID: target.ID, TargetName: target.Name,
			Dimension: check.Dimension, Severity: check.Severity,
			Status: "alert", Metric: check.Metric, Value: valueStr,
			Threshold: check.AlertValue, Message: message, DurationMs: elapsed,
		})
		m.emit("prom_alert", target.ID, target.Name, message, map[string]interface{}{
			"check":     check.Name,
			"dimension": check.Dimension,
			"severity":  check.Severity,
			"value":     valueStr,
		})

		// 持续告警时不重复推送，只在状态由非 alert 变为 alert 时通知一次
		if check.NotifyEnabled && !(hasPrev && prevStatus == "alert") {
			if err := m.dispatcher.SendGlobalNotifications(message); err != nil {
				log.Printf("[prom] notify failed for check %s: %v", check.Name, err)
			}
		}
		return
	}

	m.store.InsertPromAlertLog(&store.PromAlertLog{
		CheckID: check.ID, CheckName: check.Name,
		TargetID: target.ID, TargetName: target.Name,
		Dimension: check.Dimension, Severity: check.Severity,
		Status: "ok", Metric: check.Metric, Value: valueStr,
		Threshold: check.AlertValue, DurationMs: elapsed,
	})

	if hasPrev && prevStatus == "alert" && check.RecoveryNotify && check.NotifyEnabled {
		msg := fmt.Sprintf("[恢复] %s / %s\n指标：%s\n当前值：%s",
			target.Name, check.Name, check.Metric, valueStr)
		if err := m.dispatcher.SendGlobalNotifications(msg); err != nil {
			log.Printf("[prom] recovery notify failed for check %s: %v", check.Name, err)
		}
		m.emit("prom_recovered", target.ID, target.Name, msg, nil)
	}
}

func (m *PromManager) metricState(checkID int64) *promMetricState {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	st, ok := m.states[checkID]
	if !ok {
		st = &promMetricState{}
		m.states[checkID] = st
	}
	return st
}

// DeleteState 在规则被删除或修改时清理状态，避免旧的连续计数影响新配置。
func (m *PromManager) DeleteState(checkID int64) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	delete(m.states, checkID)
}

// TestTarget 供 UI 的"测试连接"用：抓一次并返回指标条数与样例。
func (m *PromManager) TestTarget(target *store.PromTarget) (int, []string, error) {
	families, err := m.scrape(context.Background(), target)
	if err != nil {
		return 0, nil, err
	}
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 50 {
		names = names[:50]
	}
	return len(families), names, nil
}

// TestCheck 供 UI 的"测试规则"用：抓一次并按规则求值，返回当前值与是否命中。
func (m *PromManager) TestCheck(target *store.PromTarget, check *store.PromCheck) (string, bool, string, error) {
	families, err := m.scrape(context.Background(), target)
	if err != nil {
		return "", false, "", err
	}
	value, err := computePromValue(check, families)
	if err != nil {
		return "", false, "", err
	}
	// 测试不写入状态，用临时 state 保证幂等
	matched, reason := evaluatePromRule(value, check, &promMetricState{})
	return formatPromValue(value), matched, reason, nil
}

// ---------------------------------------------------------------------------
// Prometheus 文本格式解析
// ---------------------------------------------------------------------------

type promSample struct {
	Labels map[string]string
	Value  float64
}

// parsePromText 解析 Prometheus 文本曝露格式。
// 只取指标名、标签与值，忽略 # HELP / # TYPE 与时间戳 —— 告警场景用不到。
func parsePromText(body string) map[string][]promSample {
	out := make(map[string][]promSample)

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, labels, valuePart, ok := splitPromLine(line)
		if !ok {
			continue
		}

		// 值之后可能还跟着时间戳，取第一段即可
		fields := strings.Fields(valuePart)
		if len(fields) == 0 {
			continue
		}
		value, err := parsePromFloat(fields[0])
		if err != nil {
			continue
		}

		out[name] = append(out[name], promSample{Labels: labels, Value: value})
	}

	return out
}

// splitPromLine 把一行拆成 指标名 / 标签 / 值。
// 形如：metric_name{label="v",label2="v2"} 12.34
func splitPromLine(line string) (string, map[string]string, string, bool) {
	brace := strings.IndexByte(line, '{')
	if brace < 0 {
		// 无标签：metric_name value
		sp := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
		if sp < 0 {
			return "", nil, "", false
		}
		return line[:sp], map[string]string{}, strings.TrimSpace(line[sp+1:]), true
	}

	name := line[:brace]
	closing := strings.LastIndexByte(line, '}')
	if closing < brace {
		return "", nil, "", false
	}

	labels := parsePromLabels(line[brace+1 : closing])
	return name, labels, strings.TrimSpace(line[closing+1:]), true
}

// parsePromLabels 解析标签串，处理引号内的逗号与转义。
func parsePromLabels(s string) map[string]string {
	out := map[string]string{}
	var key, val strings.Builder
	inKey, inQuote, escaped := true, false, false

	flush := func() {
		k := strings.TrimSpace(key.String())
		if k != "" {
			out[k] = val.String()
		}
		key.Reset()
		val.Reset()
		inKey = true
	}

	for _, r := range s {
		switch {
		case escaped:
			// 还原常见转义，其余原样保留
			switch r {
			case 'n':
				val.WriteRune('\n')
			case 't':
				val.WriteRune('\t')
			default:
				val.WriteRune(r)
			}
			escaped = false
		case inQuote && r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case inQuote:
			val.WriteRune(r)
		case r == '=' && inKey:
			inKey = false
		case r == ',' && !inQuote:
			flush()
		case inKey:
			key.WriteRune(r)
		default:
			// 引号外的值部分（非标准格式），忽略空白
			if r != ' ' && r != '\t' {
				val.WriteRune(r)
			}
		}
	}
	flush()
	return out
}

func parsePromFloat(s string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// ---------------------------------------------------------------------------
// 取值与求值
// ---------------------------------------------------------------------------

// computePromValue 按规则从样本里算出一个标量。
// 支持三种表达式：
//
//	raw             直接聚合 metric
//	ratio           metric / denominator
//	available_ratio 1 - metric/denominator，用于"剩余率"到"使用率"的翻转
func computePromValue(check *store.PromCheck, families map[string][]promSample) (float64, error) {
	numerator, err := aggregateMetric(check.Metric, check.LabelFilter, check.Aggregate, families)
	if err != nil {
		return 0, err
	}

	kind := strings.ToLower(strings.TrimSpace(check.ExprKind))
	if kind == "" || kind == "raw" {
		return numerator, nil
	}

	denomName := strings.TrimSpace(check.ExprDenominator)
	if denomName == "" {
		return 0, fmt.Errorf("表达式 %s 需要配置分母指标", kind)
	}
	denominator, err := aggregateMetric(denomName, check.LabelFilter, check.Aggregate, families)
	if err != nil {
		return 0, fmt.Errorf("分母指标 %s: %w", denomName, err)
	}
	if denominator == 0 {
		return 0, fmt.Errorf("分母指标 %s 为 0，无法计算比率", denomName)
	}

	ratio := numerator / denominator
	if kind == "available_ratio" {
		ratio = 1 - ratio
	}
	return ratio * 100, nil // 比率统一以百分比呈现，阈值也按百分比填
}

func aggregateMetric(metric, labelFilter, aggregate string, families map[string][]promSample) (float64, error) {
	metric = strings.TrimSpace(metric)
	samples, ok := families[metric]
	if !ok || len(samples) == 0 {
		return 0, fmt.Errorf("端点中没有指标 %s", metric)
	}

	filters := parseLabelFilter(labelFilter)
	matched := make([]float64, 0, len(samples))
	for _, s := range samples {
		if matchLabels(s.Labels, filters) {
			matched = append(matched, s.Value)
		}
	}
	if len(matched) == 0 {
		return 0, fmt.Errorf("指标 %s 没有匹配 %s 的样本", metric, labelFilter)
	}

	switch strings.ToLower(strings.TrimSpace(aggregate)) {
	case "", "last":
		return matched[len(matched)-1], nil
	case "sum":
		sum := 0.0
		for _, v := range matched {
			sum += v
		}
		return sum, nil
	case "avg":
		sum := 0.0
		for _, v := range matched {
			sum += v
		}
		return sum / float64(len(matched)), nil
	case "max":
		max := matched[0]
		for _, v := range matched {
			if v > max {
				max = v
			}
		}
		return max, nil
	case "min":
		min := matched[0]
		for _, v := range matched {
			if v < min {
				min = v
			}
		}
		return min, nil
	case "count":
		return float64(len(matched)), nil
	default:
		return 0, fmt.Errorf("不支持的聚合方式 %s", aggregate)
	}
}

// parseLabelFilter 解析 device="sda2",mountpoint="/" 形式的过滤条件。
func parseLabelFilter(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return parsePromLabels(s)
}

func matchLabels(labels, filters map[string]string) bool {
	for k, want := range filters {
		got, ok := labels[k]
		if !ok {
			return false
		}
		// 末尾 * 支持前缀匹配，容器名带随机后缀时很有用
		if strings.HasSuffix(want, "*") {
			if !strings.HasPrefix(got, strings.TrimSuffix(want, "*")) {
				return false
			}
			continue
		}
		if got != want {
			return false
		}
	}
	return true
}

// evaluatePromRule 复用 health_checks 的告警语义：
//
//	threshold  单次越界即命中
//	sustained  连续 N 次越界才命中，用于过滤抖动
//	increase   相对上次的变化量或变化率超标才命中
func evaluatePromRule(value float64, check *store.PromCheck, st *promMetricState) (bool, string) {
	strategy := strings.ToLower(strings.TrimSpace(check.AlertStrategy))
	if strategy == "" {
		strategy = "threshold"
	}

	consecutive := check.AlertConsecutive
	if consecutive <= 0 {
		consecutive = 1
	}

	thresholdMatched, thresholdMsg := evaluatePromCondition(value, check.AlertCondition, check.AlertValue)

	switch strategy {
	case "sustained":
		if thresholdMatched {
			st.ConsecutiveMatched++
		} else {
			st.ConsecutiveMatched = 0
		}
		matched := st.ConsecutiveMatched >= consecutive
		return matched, fmt.Sprintf("%s（连续 %d/%d 次）", thresholdMsg, st.ConsecutiveMatched, consecutive)

	case "increase":
		if !st.HasLast {
			return false, "首次采集，建立基线"
		}
		delta := value - st.LastValue

		if dv := strings.TrimSpace(check.AlertDeltaValue); dv != "" {
			limit, err := strconv.ParseFloat(dv, 64)
			if err == nil && delta >= limit {
				return true, fmt.Sprintf("增量 %s 达到阈值 %s（上次 %s，当前 %s）",
					formatPromValue(delta), dv, formatPromValue(st.LastValue), formatPromValue(value))
			}
		}
		if dp := strings.TrimSpace(check.AlertDeltaPercent); dp != "" {
			limit, err := strconv.ParseFloat(dp, 64)
			if err == nil && st.LastValue != 0 {
				pct := delta / math.Abs(st.LastValue) * 100
				if pct >= limit {
					return true, fmt.Sprintf("增幅 %.2f%% 达到阈值 %s%%（上次 %s，当前 %s）",
						pct, dp, formatPromValue(st.LastValue), formatPromValue(value))
				}
			}
		}
		return false, fmt.Sprintf("增量 %s 未达阈值", formatPromValue(delta))

	default: // threshold
		return thresholdMatched, thresholdMsg
	}
}

func evaluatePromCondition(value float64, condition, threshold string) (bool, string) {
	condition = strings.ToLower(strings.TrimSpace(condition))
	if condition == "" {
		condition = "gt"
	}

	raw := strings.TrimSpace(threshold)
	if raw == "" {
		return false, "未配置阈值"
	}
	limit, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return false, fmt.Sprintf("阈值 %s 不是数字", raw)
	}

	var matched bool
	var symbol string
	switch condition {
	case "gt":
		matched, symbol = value > limit, ">"
	case "gte":
		matched, symbol = value >= limit, ">="
	case "lt":
		matched, symbol = value < limit, "<"
	case "lte":
		matched, symbol = value <= limit, "<="
	case "eq":
		matched, symbol = value == limit, "=="
	case "ne":
		matched, symbol = value != limit, "!="
	default:
		return false, fmt.Sprintf("不支持的条件 %s", condition)
	}

	return matched, fmt.Sprintf("当前值 %s %s 阈值 %s", formatPromValue(value), symbol, raw)
}

func formatPromValue(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	if math.IsInf(v, -1) {
		return "-Inf"
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func renderPromMessage(target *store.PromTarget, check *store.PromCheck, value, reason string) string {
	if tpl := strings.TrimSpace(check.MessageTemplate); tpl != "" {
		r := strings.NewReplacer(
			"{{target}}", target.Name,
			"{{check}}", check.Name,
			"{{metric}}", check.Metric,
			"{{value}}", value,
			"{{threshold}}", check.AlertValue,
			"{{severity}}", check.Severity,
			"{{dimension}}", check.Dimension,
			"{{reason}}", reason,
		)
		return r.Replace(tpl)
	}

	severity := strings.ToUpper(strings.TrimSpace(check.Severity))
	if severity == "" {
		severity = "WARNING"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s / %s\n", severity, target.Name, check.Name)
	fmt.Fprintf(&b, "维度：%s\n", promDimensionLabel(check.Dimension))
	fmt.Fprintf(&b, "指标：%s", check.Metric)
	if check.LabelFilter != "" {
		fmt.Fprintf(&b, "{%s}", check.LabelFilter)
	}
	fmt.Fprintf(&b, "\n当前值：%s\n", value)
	fmt.Fprintf(&b, "判定：%s", reason)
	return b.String()
}

func promDimensionLabel(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "host":
		return "主机资源"
	case "container":
		return "容器"
	case "database":
		return "数据库"
	case "middleware":
		return "中间件"
	case "app":
		return "应用"
	case "business":
		return "业务"
	default:
		return "自定义"
	}
}

// parseJSONStringMap 解析 {"K":"V"} 形式的配置，失败时返回空 map 而不是报错 ——
// 请求头这类可选配置不该因为格式问题让整个采集停摆。
func parseJSONStringMap(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
