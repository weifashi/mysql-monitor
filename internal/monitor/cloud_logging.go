package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/logging/v2"
	"google.golang.org/api/option"

	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

type CloudLoggingManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus
	mu         sync.Mutex
	monitors   map[int64]*cloudLoggingMon
}

type cloudLoggingMon struct {
	cancel context.CancelFunc
	done   chan struct{}
}

const (
	cloudLoggingMonitorSampleLimit = 20
	cloudLoggingMonitorStatsLimit  = 500000
)

type CloudLoggingEntry struct {
	Timestamp      string                   `json:"timestamp"`
	Severity       string                   `json:"severity"`
	LogName        string                   `json:"log_name"`
	ResourceType   string                   `json:"resource_type"`
	ResourceLabels map[string]string        `json:"resource_labels,omitempty"`
	HTTPRequest    *CloudLoggingHTTPRequest `json:"http_request,omitempty"`
	Payload        string                   `json:"payload"`
	Raw            string                   `json:"raw"`
}

type CloudLoggingHTTPRequest struct {
	RequestMethod string `json:"request_method,omitempty"`
	RequestURL    string `json:"request_url,omitempty"`
	Status        int64  `json:"status,omitempty"`
	Latency       string `json:"latency,omitempty"`
	LatencyMs     int64  `json:"latency_ms,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
	RemoteIP      string `json:"remote_ip,omitempty"`
}

type CloudLoggingQueryStats struct {
	Total                     int                                    `json:"total"`
	Returned                  int                                    `json:"returned"`
	StatsLimit                int                                    `json:"stats_limit"`
	Truncated                 bool                                   `json:"truncated"`
	WithLatency               int                                    `json:"with_latency"`
	PeakConcurrency           int                                    `json:"peak_concurrency"`
	PeakAt                    string                                 `json:"peak_at"`
	AvgLatencyMs              int64                                  `json:"avg_latency_ms"`
	MaxLatencyMs              int64                                  `json:"max_latency_ms"`
	Status2xx                 int                                    `json:"status_2xx"`
	Status3xx                 int                                    `json:"status_3xx"`
	Status4xx                 int                                    `json:"status_4xx"`
	Status5xx                 int                                    `json:"status_5xx"`
	StatusOther               int                                    `json:"status_other"`
	SeverityCounts            map[string]int                         `json:"severity_counts,omitempty"`
	ResourceCounts            map[string]int                         `json:"resource_counts,omitempty"`
	PeakEndpointContributions []CloudLoggingPeakEndpointContribution `json:"peak_endpoint_contributions,omitempty"`
}

type CloudLoggingPeakEndpointContribution struct {
	Endpoint     string `json:"endpoint"`
	ActiveAtPeak int    `json:"active_at_peak"`
	RequestCount int    `json:"request_count"`
}

type CloudLoggingQueryResult struct {
	Entries         []CloudLoggingEntry     `json:"entries"`
	EffectiveFilter string                  `json:"effective_filter"`
	ResourceNames   []string                `json:"resource_names"`
	Stats           *CloudLoggingQueryStats `json:"stats,omitempty"`
}

func NewCloudLoggingManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *CloudLoggingManager {
	return &CloudLoggingManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]*cloudLoggingMon),
	}
}

func (m *CloudLoggingManager) StartAll() error {
	checks, err := m.store.ListCloudLoggingChecks()
	if err != nil {
		return fmt.Errorf("list cloud logging checks: %w", err)
	}
	for _, c := range checks {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("failed to start cloud logging check for %s: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *CloudLoggingManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	check, err := m.store.GetCloudLoggingCheck(id)
	if err != nil {
		return fmt.Errorf("get cloud logging check %d: %w", id, err)
	}
	if !check.Enabled {
		return nil
	}
	cfg, err := m.store.GetCloudLoggingConfig(check.ConfigID)
	if err != nil {
		return fmt.Errorf("get cloud logging config %d: %w", check.ConfigID, err)
	}
	if !cfg.Enabled {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.monitors[id] = &cloudLoggingMon{cancel: cancel, done: done}
	go func() {
		defer close(done)
		m.runMonitor(ctx, cfg, check)
	}()
	log.Printf("started cloud logging check for %s", check.Name)
	return nil
}

func (m *CloudLoggingManager) Stop(id int64) {
	var mon *cloudLoggingMon
	m.mu.Lock()
	if running, ok := m.monitors[id]; ok {
		mon = running
		running.cancel()
		delete(m.monitors, id)
	}
	m.mu.Unlock()
	if mon == nil {
		return
	}
	select {
	case <-mon.done:
	case <-time.After(2 * time.Second):
		log.Printf("timeout waiting cloud logging check id=%d to stop", id)
	}
	log.Printf("stopped cloud logging check id=%d", id)
}

func (m *CloudLoggingManager) Restart(id int64) error {
	m.Stop(id)
	time.Sleep(100 * time.Millisecond)
	return m.Start(id)
}

func (m *CloudLoggingManager) RestartAll() error {
	m.StopAll()
	return m.StartAll()
}

func (m *CloudLoggingManager) StopAll() {
	var monitors map[int64]*cloudLoggingMon
	m.mu.Lock()
	monitors = m.monitors
	m.monitors = make(map[int64]*cloudLoggingMon)
	m.mu.Unlock()
	for id, mon := range monitors {
		mon.cancel()
		select {
		case <-mon.done:
		case <-time.After(2 * time.Second):
			log.Printf("timeout waiting cloud logging check id=%d to stop", id)
		}
	}
	log.Println("all cloud logging checks stopped")
}

func (m *CloudLoggingManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *CloudLoggingManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *CloudLoggingManager) emit(typ string, id int64, name, message string, data interface{}) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(MonitorEvent{
		Type:       typ,
		DatabaseID: id,
		DBName:     name,
		Message:    message,
		Timestamp:  time.Now(),
		Data:       data,
	})
}

func (m *CloudLoggingManager) alertKey(id int64) string {
	return fmt.Sprintf("cloud_logging_alert_%d", id)
}

func (m *CloudLoggingManager) isAlerted(id int64) bool {
	return m.store.GetSetting(m.alertKey(id)) == "1"
}

func (m *CloudLoggingManager) setAlerted(id int64, v bool) {
	if v {
		m.store.SetSetting(m.alertKey(id), "1")
	} else {
		m.store.SetSetting(m.alertKey(id), "")
	}
}

func (m *CloudLoggingManager) runMonitor(ctx context.Context, cfg *store.CloudLoggingConfig, check *store.CloudLoggingCheck) {
	ticker := time.NewTicker(time.Duration(check.IntervalSec) * time.Second)
	defer ticker.Stop()

	m.doCheck(ctx, cfg, check)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.doCheck(ctx, cfg, check)
		}
	}
}

func (m *CloudLoggingManager) doCheck(ctx context.Context, cfg *store.CloudLoggingConfig, check *store.CloudLoggingCheck) {
	m.emit("cloud_logging_checking", check.ID, check.Name, "查询中...", nil)
	start := time.Now()
	result, value, err := m.queryCloudLoggingCheck(ctx, cfg, check)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		m.emit("cloud_logging_error", check.ID, check.Name, "查询失败: "+err.Error(), nil)
		m.store.InsertCloudLoggingAlertLog(&store.CloudLoggingAlertLog{
			CheckID: check.ID, CheckName: check.Name, ConfigID: cfg.ID, ConfigName: cfg.Name,
			Status: "error", Filter: combineCloudLoggingFilter(cfg.DefaultFilter, check.Filter, check.LookbackMinutes), Error: err.Error(), DurationMs: duration,
		})
		return
	}

	if value > check.ThresholdCount {
		m.handleCloudLoggingAlert(cfg, check, result, value, duration)
		return
	}

	metricLabel := cloudLoggingMetricLabel(check.MetricType)
	m.emit("cloud_logging_ok", check.ID, check.Name, fmt.Sprintf("无告警 (%s %d/%d)", metricLabel, value, check.ThresholdCount), nil)
	if m.isAlerted(check.ID) {
		m.setAlerted(check.ID, false)
		m.store.InsertCloudLoggingAlertLog(&store.CloudLoggingAlertLog{
			CheckID: check.ID, CheckName: check.Name, ConfigID: cfg.ID, ConfigName: cfg.Name,
			Status: "recovery", MatchCount: value, ThresholdCount: check.ThresholdCount, Filter: result.EffectiveFilter, DurationMs: duration,
		})
		if check.NotifyEnabled && check.RecoveryNotify {
			msg := fmt.Sprintf("Cloud Logging 恢复通知\n\n规则: %s\n配置: %s\n指标: %s\n当前值: %d\n阈值: > %d\n状态: 已恢复正常",
				check.Name, cfg.Name, metricLabel, value, check.ThresholdCount)
			if err := m.dispatcher.SendScopedNotifications("cloud_logging", check.ID, msg); err != nil {
				log.Printf("[CloudLogging %s] recovery notification failed: %v", check.Name, err)
				m.emit("cloud_logging_notify_error", check.ID, check.Name, "恢复通知发送失败: "+err.Error(), nil)
			} else {
				m.emit("cloud_logging_notified", check.ID, check.Name, "已发送恢复通知", nil)
			}
		}
	}
}

func (m *CloudLoggingManager) queryCloudLoggingCheck(ctx context.Context, cfg *store.CloudLoggingConfig, check *store.CloudLoggingCheck) (*CloudLoggingQueryResult, int, error) {
	if check.MetricType == store.CloudLoggingMetricPeakConcurrency {
		result, err := QueryCloudLoggingWithStats(ctx, cfg, check.Filter, check.LookbackMinutes, cloudLoggingMonitorSampleLimit, cloudLoggingMonitorStatsLimit)
		if err != nil {
			return nil, 0, err
		}
		peak := 0
		if result.Stats != nil {
			peak = result.Stats.PeakConcurrency
		}
		return result, peak, nil
	}
	limit := check.ThresholdCount + 1
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	result, err := QueryCloudLogging(ctx, cfg, check.Filter, check.LookbackMinutes, limit)
	if err != nil {
		return nil, 0, err
	}
	return result, len(result.Entries), nil
}

func cloudLoggingMetricLabel(metricType string) string {
	if metricType == store.CloudLoggingMetricPeakConcurrency {
		return "接口最大并发"
	}
	return "命中数量"
}

func (m *CloudLoggingManager) handleCloudLoggingAlert(cfg *store.CloudLoggingConfig, check *store.CloudLoggingCheck, result *CloudLoggingQueryResult, value int, duration int64) {
	metricLabel := cloudLoggingMetricLabel(check.MetricType)
	if m.isAlerted(check.ID) {
		m.emit("cloud_logging_alert", check.ID, check.Name, fmt.Sprintf("仍在告警 (%s %d > %d)", metricLabel, value, check.ThresholdCount), nil)
		return
	}
	sample := cloudLoggingSampleJSON(result.Entries, 5)
	m.store.InsertCloudLoggingAlertLog(&store.CloudLoggingAlertLog{
		CheckID: check.ID, CheckName: check.Name, ConfigID: cfg.ID, ConfigName: cfg.Name,
		Status: "alert", MatchCount: value, ThresholdCount: check.ThresholdCount, Filter: result.EffectiveFilter, Sample: sample, DurationMs: duration,
	})
	m.emit("cloud_logging_alert", check.ID, check.Name, fmt.Sprintf("%s %d，阈值 > %d", metricLabel, value, check.ThresholdCount), result)

	if !check.NotifyEnabled {
		m.setAlerted(check.ID, true)
		return
	}

	msg := cloudLoggingAlertNotificationMessage(cfg, check, result, value, metricLabel)
	if err := m.dispatcher.SendScopedNotifications("cloud_logging", check.ID, msg); err != nil {
		log.Printf("[CloudLogging %s] alert notification failed: %v", check.Name, err)
		m.emit("cloud_logging_notify_error", check.ID, check.Name, "通知发送失败: "+err.Error(), nil)
		return
	}
	m.emit("cloud_logging_notified", check.ID, check.Name, "已发送 Cloud Logging 告警通知", nil)
	m.setAlerted(check.ID, true)
}

func QueryCloudLogging(ctx context.Context, cfg *store.CloudLoggingConfig, filter string, lookbackMinutes, limit int) (*CloudLoggingQueryResult, error) {
	return queryCloudLogging(ctx, cfg, filter, lookbackMinutes, limit, false, 0)
}

func QueryCloudLoggingWithStats(ctx context.Context, cfg *store.CloudLoggingConfig, filter string, lookbackMinutes, limit, statsLimit int) (*CloudLoggingQueryResult, error) {
	return queryCloudLogging(ctx, cfg, filter, lookbackMinutes, limit, true, statsLimit)
}

func queryCloudLogging(ctx context.Context, cfg *store.CloudLoggingConfig, filter string, lookbackMinutes, limit int, collectStats bool, statsLimit int) (*CloudLoggingQueryResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	if collectStats {
		if statsLimit <= 0 {
			statsLimit = 200000
		}
		if statsLimit > 500000 {
			statsLimit = 500000
		}
	}
	resourceNames := cloudLoggingResourceNames(cfg)
	if len(resourceNames) == 0 {
		return nil, fmt.Errorf("project_id 或 resource_names 不能为空")
	}
	opts := []option.ClientOption{}
	if strings.TrimSpace(cfg.CredentialsFile) != "" {
		opts = append(opts, option.WithCredentialsFile(strings.TrimSpace(cfg.CredentialsFile)))
	}
	svc, err := logging.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create logging service: %w", err)
	}
	windowStart, windowEnd := cloudLoggingQueryWindow(lookbackMinutes)
	effectiveFilter := combineCloudLoggingFilterWindow(cfg.DefaultFilter, filter, windowStart, windowEnd)
	req := &logging.ListLogEntriesRequest{
		ResourceNames: resourceNames,
		Filter:        effectiveFilter,
		OrderBy:       "timestamp desc",
		PageSize:      int64(limit),
	}
	if collectStats {
		req.PageSize = 1000
	}
	entries := make([]CloudLoggingEntry, 0, limit)
	var stats *CloudLoggingQueryStats
	var events []cloudLoggingConcurrencyEvent
	var requests []cloudLoggingConcurrencyRequest
	if collectStats {
		stats = &CloudLoggingQueryStats{
			StatsLimit:     statsLimit,
			SeverityCounts: make(map[string]int),
			ResourceCounts: make(map[string]int),
		}
	}
	for {
		resp, err := svc.Entries.List(req).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		for _, entry := range resp.Entries {
			include := !collectStats || cloudLoggingStatsTimeInWindow(entry, windowStart, windowEnd)
			if include && len(entries) < limit {
				entries = append(entries, cloudLoggingEntryFromAPI(entry))
			}
			if collectStats && include {
				collectCloudLoggingStats(stats, &events, &requests, entry)
				if stats.Total >= statsLimit {
					if resp.NextPageToken != "" {
						stats.Truncated = true
					}
					finalizeCloudLoggingStats(stats, events, requests, len(entries))
					return &CloudLoggingQueryResult{Entries: entries, EffectiveFilter: effectiveFilter, ResourceNames: resourceNames, Stats: stats}, nil
				}
			}
		}
		if !collectStats || resp.NextPageToken == "" {
			break
		}
		req.PageToken = resp.NextPageToken
	}
	if collectStats {
		finalizeCloudLoggingStats(stats, events, requests, len(entries))
	}
	return &CloudLoggingQueryResult{Entries: entries, EffectiveFilter: effectiveFilter, ResourceNames: resourceNames, Stats: stats}, nil
}

func cloudLoggingResourceNames(cfg *store.CloudLoggingConfig) []string {
	var resources []string
	for _, part := range strings.FieldsFunc(cfg.ResourceNames, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		resources = append(resources, part)
	}
	if len(resources) == 0 && strings.TrimSpace(cfg.ProjectID) != "" {
		resources = append(resources, "projects/"+strings.TrimSpace(cfg.ProjectID))
	}
	return resources
}

func combineCloudLoggingFilter(defaultFilter, filter string, lookbackMinutes int) string {
	windowStart, windowEnd := cloudLoggingQueryWindow(lookbackMinutes)
	return combineCloudLoggingFilterWindow(defaultFilter, filter, windowStart, windowEnd)
}

func cloudLoggingQueryWindow(lookbackMinutes int) (time.Time, time.Time) {
	if lookbackMinutes <= 0 {
		lookbackMinutes = 5
	}
	windowEnd := time.Now().UTC()
	windowStart := windowEnd.Add(-time.Duration(lookbackMinutes) * time.Minute)
	return windowStart, windowEnd
}

func combineCloudLoggingFilterWindow(defaultFilter, filter string, windowStart, windowEnd time.Time) string {
	parts := []string{
		fmt.Sprintf(`timestamp >= "%s"`, windowStart.UTC().Format(time.RFC3339Nano)),
		fmt.Sprintf(`timestamp <= "%s"`, windowEnd.UTC().Format(time.RFC3339Nano)),
	}
	for _, part := range []string{defaultFilter, filter} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, "("+part+")")
		}
	}
	return strings.Join(parts, "\nAND ")
}

func cloudLoggingEntryFromAPI(entry *logging.LogEntry) CloudLoggingEntry {
	var raw string
	if data, err := json.Marshal(entry); err == nil {
		raw = string(data)
	}
	var resourceType string
	var resourceLabels map[string]string
	if entry.Resource != nil {
		resourceType = entry.Resource.Type
		resourceLabels = entry.Resource.Labels
	}
	return CloudLoggingEntry{
		Timestamp:      entry.Timestamp,
		Severity:       entry.Severity,
		LogName:        entry.LogName,
		ResourceType:   resourceType,
		ResourceLabels: resourceLabels,
		HTTPRequest:    cloudLoggingHTTPRequestFromAPI(entry.HttpRequest),
		Payload:        cloudLoggingEntryPayload(entry),
		Raw:            raw,
	}
}

func cloudLoggingHTTPRequestFromAPI(req *logging.HttpRequest) *CloudLoggingHTTPRequest {
	if req == nil {
		return nil
	}
	latencyMs := int64(0)
	if d, ok := parseCloudLoggingLatency(req.Latency); ok {
		latencyMs = d.Milliseconds()
	}
	return &CloudLoggingHTTPRequest{
		RequestMethod: req.RequestMethod,
		RequestURL:    req.RequestUrl,
		Status:        req.Status,
		Latency:       req.Latency,
		LatencyMs:     latencyMs,
		UserAgent:     req.UserAgent,
		RemoteIP:      req.RemoteIp,
	}
}

type cloudLoggingConcurrencyEvent struct {
	at    time.Time
	delta int
}

type cloudLoggingConcurrencyRequest struct {
	start    time.Time
	end      time.Time
	endpoint string
}

func cloudLoggingStatsTimeInWindow(entry *logging.LogEntry, windowStart, windowEnd time.Time) bool {
	at, ok := cloudLoggingStatsTime(entry)
	if !ok {
		return false
	}
	return !at.Before(windowStart) && !at.After(windowEnd)
}

func cloudLoggingStatsTime(entry *logging.LogEntry) (time.Time, bool) {
	end, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
	if err != nil {
		return time.Time{}, false
	}
	if entry.HttpRequest == nil {
		return end, true
	}
	if latency, ok := parseCloudLoggingLatency(entry.HttpRequest.Latency); ok && latency >= 0 {
		return end.Add(-latency), true
	}
	return end, true
}

func collectCloudLoggingStats(stats *CloudLoggingQueryStats, events *[]cloudLoggingConcurrencyEvent, requests *[]cloudLoggingConcurrencyRequest, entry *logging.LogEntry) {
	stats.Total++
	severity := strings.TrimSpace(entry.Severity)
	if severity == "" {
		severity = "DEFAULT"
	}
	stats.SeverityCounts[severity]++
	if entry.Resource != nil && strings.TrimSpace(entry.Resource.Type) != "" {
		stats.ResourceCounts[entry.Resource.Type]++
	}
	if entry.HttpRequest == nil {
		return
	}
	switch status := entry.HttpRequest.Status; {
	case status >= 200 && status <= 299:
		stats.Status2xx++
	case status >= 300 && status <= 399:
		stats.Status3xx++
	case status >= 400 && status <= 499:
		stats.Status4xx++
	case status >= 500 && status <= 599:
		stats.Status5xx++
	case status > 0:
		stats.StatusOther++
	}
	latency, ok := parseCloudLoggingLatency(entry.HttpRequest.Latency)
	if !ok || latency < 0 {
		return
	}
	end, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
	if err != nil {
		return
	}
	start := end.Add(-latency)
	*events = append(*events,
		cloudLoggingConcurrencyEvent{at: start, delta: 1},
		cloudLoggingConcurrencyEvent{at: end, delta: -1},
	)
	*requests = append(*requests, cloudLoggingConcurrencyRequest{
		start:    start,
		end:      end,
		endpoint: cloudLoggingEndpointKey(entry.HttpRequest),
	})
	stats.WithLatency++
	ms := latency.Milliseconds()
	stats.AvgLatencyMs += ms
	if ms > stats.MaxLatencyMs {
		stats.MaxLatencyMs = ms
	}
}

func finalizeCloudLoggingStats(stats *CloudLoggingQueryStats, events []cloudLoggingConcurrencyEvent, requests []cloudLoggingConcurrencyRequest, returned int) {
	if stats == nil {
		return
	}
	stats.Returned = returned
	if stats.WithLatency > 0 {
		stats.AvgLatencyMs = stats.AvgLatencyMs / int64(stats.WithLatency)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].at.Equal(events[j].at) {
			return events[i].delta < events[j].delta
		}
		return events[i].at.Before(events[j].at)
	})
	current := 0
	perSecondPeak := make(map[time.Time]int)
	perSecondPeakAt := make(map[time.Time]time.Time)
	for _, event := range events {
		current += event.delta
		second := event.at.UTC().Truncate(time.Second)
		if current > perSecondPeak[second] {
			perSecondPeak[second] = current
			perSecondPeakAt[second] = event.at
		}
	}
	var peakAt time.Time
	var peakMoment time.Time
	for second, value := range perSecondPeak {
		if value > stats.PeakConcurrency || (value == stats.PeakConcurrency && (peakAt.IsZero() || second.Before(peakAt))) {
			stats.PeakConcurrency = value
			peakAt = second
			peakMoment = perSecondPeakAt[second]
		}
	}
	if !peakAt.IsZero() {
		stats.PeakAt = peakAt.Format(time.RFC3339Nano)
	}
	stats.PeakEndpointContributions = cloudLoggingPeakEndpointContributions(requests, peakMoment)
}

func cloudLoggingPeakEndpointContributions(requests []cloudLoggingConcurrencyRequest, peakMoment time.Time) []CloudLoggingPeakEndpointContribution {
	if peakMoment.IsZero() {
		return nil
	}
	counts := make(map[string]int)
	active := make(map[string]int)
	for _, req := range requests {
		endpoint := strings.TrimSpace(req.endpoint)
		if endpoint == "" {
			endpoint = "-"
		}
		counts[endpoint]++
		if !req.start.After(peakMoment) && peakMoment.Before(req.end) {
			active[endpoint]++
		}
	}
	rows := make([]CloudLoggingPeakEndpointContribution, 0, len(active))
	for endpoint, activeCount := range active {
		rows = append(rows, CloudLoggingPeakEndpointContribution{
			Endpoint:     endpoint,
			ActiveAtPeak: activeCount,
			RequestCount: counts[endpoint],
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ActiveAtPeak == rows[j].ActiveAtPeak {
			if rows[i].RequestCount == rows[j].RequestCount {
				return rows[i].Endpoint < rows[j].Endpoint
			}
			return rows[i].RequestCount > rows[j].RequestCount
		}
		return rows[i].ActiveAtPeak > rows[j].ActiveAtPeak
	})
	return rows
}

func cloudLoggingEndpointKey(req *logging.HttpRequest) string {
	if req == nil {
		return "-"
	}
	method := strings.TrimSpace(req.RequestMethod)
	path := cloudLoggingRequestPath(req.RequestUrl)
	key := strings.TrimSpace(strings.TrimSpace(method) + " " + path)
	if key == "" {
		return "-"
	}
	return key
}

func cloudLoggingRequestPath(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "-"
	}
	u, err := url.Parse(rawURL)
	if err == nil && u.Path != "" {
		return u.Path
	}
	if idx := strings.Index(rawURL, "?"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	if rawURL == "" {
		return "-"
	}
	return rawURL
}

func parseCloudLoggingLatency(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, false
	}
	return d, true
}

func cloudLoggingAlertNotificationMessage(cfg *store.CloudLoggingConfig, check *store.CloudLoggingCheck, result *CloudLoggingQueryResult, value int, metricLabel string) string {
	base := fmt.Sprintf("Cloud Logging 告警\n\n规则: %s\n配置: %s\n资源: %s\n指标: %s\n当前值: %d\n阈值: > %d\n回看窗口: %d 分钟",
		check.Name, cfg.Name, strings.Join(result.ResourceNames, ", "), metricLabel, value, check.ThresholdCount, check.LookbackMinutes)

	if check.MetricType == store.CloudLoggingMetricPeakConcurrency {
		return base + "\n\n" + cloudLoggingPeakEndpointNotification(result.Stats, check.LookbackMinutes) + "\n\n该告警仅发送一次，恢复后如再次命中将重新通知。"
	}

	notificationSample := cloudLoggingNotificationSampleJSON(result.Entries)
	return base + fmt.Sprintf("\n\n样例摘要:\n%s\n\n该告警仅发送一次，恢复后如再次命中将重新通知。", notificationSample)
}

func cloudLoggingPeakEndpointNotification(stats *CloudLoggingQueryStats, lookbackMinutes int) string {
	if stats == nil {
		return "峰值接口贡献:\n暂无统计数据"
	}
	var b strings.Builder
	if strings.TrimSpace(stats.PeakAt) != "" {
		fmt.Fprintf(&b, "峰值时间: %s\n\n", stats.PeakAt)
	}
	b.WriteString("峰值接口贡献:\n")
	b.WriteString("接口 | 峰值那一刻活跃并发 | ")
	if lookbackMinutes > 0 {
		fmt.Fprintf(&b, "%d分钟请求数\n", lookbackMinutes)
	} else {
		b.WriteString("窗口请求数\n")
	}
	if len(stats.PeakEndpointContributions) == 0 {
		b.WriteString("暂无接口贡献数据")
		return b.String()
	}
	limit := len(stats.PeakEndpointContributions)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		row := stats.PeakEndpointContributions[i]
		fmt.Fprintf(&b, "%s | %d | %d\n", row.Endpoint, row.ActiveAtPeak, row.RequestCount)
	}
	if stats.Truncated {
		b.WriteString("\n提示: 本次统计达到 Cloud Logging 拉取上限，接口请求数可能不是完整窗口总数。")
	}
	return strings.TrimRight(b.String(), "\n")
}

func cloudLoggingEntryPayload(entry *logging.LogEntry) string {
	if entry.TextPayload != "" {
		return entry.TextPayload
	}
	if len(entry.JsonPayload) > 0 {
		return compactJSONString([]byte(entry.JsonPayload))
	}
	if len(entry.ProtoPayload) > 0 {
		return compactJSONString([]byte(entry.ProtoPayload))
	}
	if len(entry.Otel) > 0 {
		return compactJSONString([]byte(entry.Otel))
	}
	return ""
}

func compactJSONString(data []byte) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(data)
	}
	return string(out)
}

func cloudLoggingSampleJSON(entries []CloudLoggingEntry, max int) string {
	if max <= 0 || max > len(entries) {
		max = len(entries)
	}
	if max == 0 {
		return "[]"
	}
	data, err := json.MarshalIndent(entries[:max], "", "  ")
	if err != nil {
		return "[]"
	}
	return string(data)
}

func cloudLoggingNotificationSampleJSON(entries []CloudLoggingEntry) string {
	if len(entries) == 0 {
		return "{}"
	}
	entry := entries[0]
	sample := struct {
		Timestamp      string            `json:"timestamp"`
		Severity       string            `json:"severity"`
		LogName        string            `json:"log_name"`
		ResourceType   string            `json:"resource_type"`
		ResourceLabels map[string]string `json:"resource_labels,omitempty"`
		Payload        string            `json:"payload"`
	}{
		Timestamp:      entry.Timestamp,
		Severity:       entry.Severity,
		LogName:        entry.LogName,
		ResourceType:   entry.ResourceType,
		ResourceLabels: entry.ResourceLabels,
		Payload:        entry.Payload,
	}
	data, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}
