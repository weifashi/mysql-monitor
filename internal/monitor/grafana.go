package monitor

import (
	"bytes"
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

// --- Alert Rule Definitions ---

type AlertRuleDef struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Expr        string  `json:"expr"`
	Condition   string  `json:"condition"` // gt, lt, eq
	Threshold   float64 `json:"threshold"`
	For         string  `json:"for"`
	Summary     string  `json:"summary"`
	Description string  `json:"description"`
}

var DefaultAlertRules = []AlertRuleDef{
	{Key: "mysql_down", Title: "MySQL 宕机", Expr: "mysql_up", Condition: "lt", Threshold: 1, For: "1m", Summary: "MySQL 实例宕机", Description: "mysql_up == 0 超过 1 分钟"},
	{Key: "slow_query_surge", Title: "慢SQL激增", Expr: "increase(mysql_global_status_slow_queries[5m])", Condition: "gt", Threshold: 10, For: "5m", Summary: "慢SQL数量激增", Description: "5分钟内慢SQL超过10条"},
	{Key: "too_many_connections", Title: "连接数过高", Expr: "mysql_global_status_threads_connected", Condition: "gt", Threshold: 100, For: "5m", Summary: "MySQL连接数过高", Description: "连接数超过100持续5分钟"},
	{Key: "pod_restart", Title: "Pod频繁重启", Expr: "increase(kube_pod_container_status_restarts_total[1h])", Condition: "gt", Threshold: 3, For: "5m", Summary: "Pod频繁重启", Description: "1小时内重启超过3次"},
	{Key: "disk_full", Title: "磁盘空间不足", Expr: "node_filesystem_avail_bytes / node_filesystem_size_bytes", Condition: "lt", Threshold: 0.1, For: "5m", Summary: "磁盘空间低于10%", Description: "磁盘可用空间不足10%"},
	{Key: "nginx_slow_response", Title: "Nginx 响应过慢", Expr: "histogram_quantile(0.95, sum(rate(nginx_http_request_duration_seconds_bucket[5m])) by (le))", Condition: "gt", Threshold: 3, For: "5m", Summary: "Nginx P95 响应时间过高", Description: "Nginx P95 响应时间超过3秒持续5分钟"},
}

// --- GrafanaManager ---

type GrafanaManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus
	mu         sync.Mutex
	monitors   map[int64]*grafanaMon
}

type grafanaMon struct {
	cancel context.CancelFunc
}

func NewGrafanaManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *GrafanaManager {
	return &GrafanaManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]*grafanaMon),
	}
}

func (m *GrafanaManager) StartAll() error {
	configs, err := m.store.ListGrafanaConfigs()
	if err != nil {
		return fmt.Errorf("list grafana configs: %w", err)
	}
	for _, c := range configs {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("failed to start grafana monitor for %s: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *GrafanaManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	cfg, err := m.store.GetGrafanaConfig(id)
	if err != nil {
		return fmt.Errorf("get grafana config %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = &grafanaMon{cancel: cancel}

	go m.runMonitor(ctx, cfg)
	log.Printf("started grafana monitor for %s (%s)", cfg.Name, cfg.GrafanaURL)
	return nil
}

func (m *GrafanaManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mon, ok := m.monitors[id]; ok {
		mon.cancel()
		delete(m.monitors, id)
		log.Printf("stopped grafana monitor id=%d", id)
	}
}

func (m *GrafanaManager) Restart(id int64) error {
	m.Stop(id)
	time.Sleep(100 * time.Millisecond)
	return m.Start(id)
}

func (m *GrafanaManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mon := range m.monitors {
		mon.cancel()
		delete(m.monitors, id)
	}
	log.Println("all grafana monitors stopped")
}

func (m *GrafanaManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *GrafanaManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *GrafanaManager) emit(typ string, configID int64, name, message string, data interface{}) {
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

func (m *GrafanaManager) runMonitor(ctx context.Context, cfg *store.GrafanaConfig) {
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

func (m *GrafanaManager) doCheck(cfg *store.GrafanaConfig) {
	m.emit("grafana_checking", cfg.ID, cfg.Name, "检查 Grafana 连接...", nil)

	client := newGrafanaClient(cfg)
	if err := client.TestConnection(); err != nil {
		m.emit("grafana_error", cfg.ID, cfg.Name, fmt.Sprintf("Grafana 连接失败: %v", err), nil)

		key := fmt.Sprintf("grafana_err_%d", cfg.ID)
		if m.store.GetSetting(key) != "1" {
			errMsg := fmt.Sprintf("Grafana 连接异常告警\n\n配置: %s\nURL: %s\n错误: %v", cfg.Name, cfg.GrafanaURL, err)
			m.dispatcher.SendGlobalNotifications(errMsg)
			m.store.SetSetting(key, "1")
		}
		return
	}

	// Clear error state on success
	key := fmt.Sprintf("grafana_err_%d", cfg.ID)
	if m.store.GetSetting(key) == "1" {
		m.store.SetSetting(key, "")
		recoveryMsg := fmt.Sprintf("Grafana 连接恢复\n\n配置: %s\nURL: %s\n状态: 已恢复正常", cfg.Name, cfg.GrafanaURL)
		m.dispatcher.SendGlobalNotifications(recoveryMsg)
	}

	m.emit("grafana_ok", cfg.ID, cfg.Name, "Grafana 连接正常", nil)
}

// ProvisionForConfig creates contact point, folder, alert rules in Grafana.
func (m *GrafanaManager) ProvisionForConfig(id int64) error {
	cfg, err := m.store.GetGrafanaConfig(id)
	if err != nil {
		return err
	}

	client := newGrafanaClient(cfg)

	if err := client.TestConnection(); err != nil {
		return fmt.Errorf("连接测试失败: %w", err)
	}

	webhookURL := strings.TrimRight(cfg.WebhookURL, "/") + "/api/grafana-webhook"
	contactPointName := fmt.Sprintf("OpsMonitor-%d", cfg.ID)

	// 1. Create or update contact point
	webhookUID, err := client.CreateContactPoint(contactPointName, webhookURL)
	if err != nil {
		return fmt.Errorf("创建联系点失败: %w", err)
	}

	// 2. Create folder
	folderUID, err := client.CreateOrGetFolder("OpsMonitor Alerts")
	if err != nil {
		return fmt.Errorf("创建文件夹失败: %w", err)
	}

	// 3. Save UIDs
	m.store.UpdateGrafanaProvisionUIDs(id, webhookUID, folderUID)

	// 4. Parse selected rules
	var selectedKeys []string
	json.Unmarshal([]byte(cfg.AutoRules), &selectedKeys)

	ruleGroupName := fmt.Sprintf("OpsMonitor-%d", cfg.ID)

	// 5. Create alert rules
	for _, def := range DefaultAlertRules {
		if !contains(selectedKeys, def.Key) {
			continue
		}
		ruleTitle := fmt.Sprintf("[OpsMonitor-%d] %s", cfg.ID, def.Title)
		if err := client.CreateAlertRule(folderUID, cfg.DatasourceUID, ruleGroupName, ruleTitle, def, cfg.ID); err != nil {
			log.Printf("[Grafana %s] create rule %s failed: %v", cfg.Name, def.Key, err)
		}
	}

	// 6. Update notification policy
	if err := client.AddNotificationRoute(contactPointName, cfg.ID); err != nil {
		log.Printf("[Grafana %s] update notification policy failed: %v", cfg.Name, err)
	}

	m.emit("grafana_provisioned", cfg.ID, cfg.Name, "已同步告警规则到 Grafana", nil)
	return nil
}

// CleanupGrafanaResources removes contact point from Grafana when config is deleted.
func (m *GrafanaManager) CleanupGrafanaResources(id int64) {
	cfg, err := m.store.GetGrafanaConfig(id)
	if err != nil {
		return
	}
	client := newGrafanaClient(cfg)
	if cfg.WebhookUID != "" {
		client.DeleteContactPoint(cfg.WebhookUID)
	}
}

// HandleWebhook processes incoming Grafana alert webhooks.
func (m *GrafanaManager) HandleWebhook(body []byte) error {
	var payload struct {
		Status       string `json:"status"`
		CommonLabels map[string]string `json:"commonLabels"`
		Alerts       []struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			StartsAt    time.Time         `json:"startsAt"`
			EndsAt      time.Time         `json:"endsAt"`
			Fingerprint string            `json:"fingerprint"`
		} `json:"alerts"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse webhook payload: %w", err)
	}

	for _, alert := range payload.Alerts {
		alertName := alert.Labels["alertname"]
		severity := alert.Labels["severity"]
		summary := alert.Annotations["summary"]
		description := alert.Annotations["description"]

		labelsJSON, _ := json.Marshal(alert.Labels)

		var endsAt *time.Time
		zeroTime := time.Time{}
		if alert.EndsAt != zeroTime && !alert.EndsAt.IsZero() {
			endsAt = &alert.EndsAt
		}

		// Find config ID from label
		var configID int64
		var configName string
		if src, ok := alert.Labels["ops_monitor_source"]; ok {
			fmt.Sscanf(src, "grafana-%d", &configID)
		}
		if configID > 0 {
			if cfg, err := m.store.GetGrafanaConfig(configID); err == nil {
				configName = cfg.Name
			}
		}

		logEntry := &store.GrafanaAlertLog{
			ConfigID:    configID,
			ConfigName:  configName,
			AlertName:   alertName,
			Status:      alert.Status,
			Severity:    severity,
			Summary:     summary,
			Description: description,
			Fingerprint: alert.Fingerprint,
			LabelsJSON:  string(labelsJSON),
			StartsAt:    alert.StartsAt,
			EndsAt:      endsAt,
		}

		logID, err := m.store.InsertGrafanaAlertLog(logEntry)
		if err != nil {
			log.Printf("[Grafana webhook] insert alert log failed: %v", err)
			continue
		}
		logEntry.ID = logID
		logEntry.DetectedAt = time.Now()

		m.emit("grafana_alert_received", configID, configName, fmt.Sprintf("[%s] %s: %s", alert.Status, alertName, summary), logEntry)

		// Send notification for firing alerts
		if alert.Status == "firing" {
			msg := fmt.Sprintf("Grafana 告警通知\n\n告警: %s\n严重度: %s\n摘要: %s\n详情: %s\n状态: %s\n配置: %s",
				alertName, severity, summary, description, alert.Status, configName)
			if sendErr := m.dispatcher.SendGlobalNotifications(msg); sendErr != nil {
				log.Printf("[Grafana webhook] notification failed: %v", sendErr)
			}
		} else if alert.Status == "resolved" {
			msg := fmt.Sprintf("Grafana 告警恢复\n\n告警: %s\n摘要: %s\n状态: 已恢复\n配置: %s",
				alertName, summary, configName)
			m.dispatcher.SendGlobalNotifications(msg)
		}
	}

	return nil
}

// TestGrafanaConnection tests connection to a Grafana instance.
func TestGrafanaConnection(cfg *store.GrafanaConfig) error {
	client := newGrafanaClient(cfg)
	return client.TestConnection()
}

// --- Grafana HTTP Client ---

type grafanaClient struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func newGrafanaClient(cfg *store.GrafanaConfig) *grafanaClient {
	return &grafanaClient{
		baseURL:  strings.TrimRight(cfg.GrafanaURL, "/"),
		username: cfg.Username,
		password: cfg.Password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *grafanaClient) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Disable-Provenance", "true")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	return data, resp.StatusCode, nil
}

func (c *grafanaClient) TestConnection() error {
	data, status, err := c.doRequest("GET", "/api/health", nil)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("HTTP %d: %s", status, string(data))
	}
	return nil
}

type GrafanaDatasource struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (c *grafanaClient) ListDatasources() ([]GrafanaDatasource, error) {
	data, status, err := c.doRequest("GET", "/api/datasources", nil)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(data))
	}
	var all []GrafanaDatasource
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}
	var result []GrafanaDatasource
	for _, ds := range all {
		if ds.Type == "prometheus" {
			result = append(result, ds)
		}
	}
	return result, nil
}

func ListGrafanaDatasources(grafanaURL, username, password string) ([]GrafanaDatasource, error) {
	c := &grafanaClient{
		baseURL:  strings.TrimRight(grafanaURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
	return c.ListDatasources()
}

func (c *grafanaClient) CreateContactPoint(name, webhookURL string) (string, error) {
	payload := map[string]interface{}{
		"name": name,
		"type": "webhook",
		"settings": map[string]interface{}{
			"url":        webhookURL,
			"httpMethod": "POST",
		},
		"disableResolveMessage": false,
	}

	data, status, err := c.doRequest("POST", "/api/v1/provisioning/contact-points", payload)
	if err != nil {
		return "", err
	}

	// If already exists (409), try to find and return existing
	if status == 409 {
		return name, nil
	}

	if status < 200 || status >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", status, string(data))
	}

	var resp struct {
		UID string `json:"uid"`
	}
	json.Unmarshal(data, &resp)
	if resp.UID == "" {
		return name, nil
	}
	return resp.UID, nil
}

func (c *grafanaClient) DeleteContactPoint(uid string) error {
	_, status, err := c.doRequest("DELETE", "/api/v1/provisioning/contact-points/"+uid, nil)
	if err != nil {
		return err
	}
	if status != 202 && status != 200 && status != 204 {
		return fmt.Errorf("delete contact point: HTTP %d", status)
	}
	return nil
}

func (c *grafanaClient) CreateOrGetFolder(title string) (string, error) {
	payload := map[string]string{"title": title}
	data, status, err := c.doRequest("POST", "/api/folders", payload)
	if err != nil {
		return "", err
	}

	var resp struct {
		UID string `json:"uid"`
	}

	if status == 409 || status == 412 {
		// Folder already exists, search for it
		searchData, searchStatus, searchErr := c.doRequest("GET", "/api/folders?limit=100", nil)
		if searchErr != nil {
			return "", searchErr
		}
		if searchStatus == 200 {
			var folders []struct {
				UID   string `json:"uid"`
				Title string `json:"title"`
			}
			json.Unmarshal(searchData, &folders)
			for _, f := range folders {
				if f.Title == title {
					return f.UID, nil
				}
			}
		}
		return "", fmt.Errorf("folder exists but not found")
	}

	if status < 200 || status >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", status, string(data))
	}

	json.Unmarshal(data, &resp)
	return resp.UID, nil
}

func (c *grafanaClient) CreateAlertRule(folderUID, datasourceUID, ruleGroup, title string, def AlertRuleDef, configID int64) error {
	condType := "gt"
	if def.Condition == "lt" {
		condType = "lt"
	}

	payload := map[string]interface{}{
		"title":     title,
		"ruleGroup": ruleGroup,
		"folderUID": folderUID,
		"orgID":     1,
		"condition": "B",
		"data": []map[string]interface{}{
			{
				"refId":             "A",
				"datasourceUid":     datasourceUID,
				"relativeTimeRange": map[string]int{"from": 600, "to": 0},
				"model": map[string]interface{}{
					"expr":    def.Expr,
					"refId":   "A",
					"instant": true,
				},
			},
			{
				"refId":             "B",
				"datasourceUid":     "__expr__",
				"relativeTimeRange": map[string]int{"from": 0, "to": 0},
				"model": map[string]interface{}{
					"type":       "threshold",
					"expression": "A",
					"conditions": []map[string]interface{}{
						{
							"evaluator": map[string]interface{}{
								"type":   condType,
								"params": []float64{def.Threshold},
							},
						},
					},
				},
			},
		},
		"for":          def.For,
		"noDataState":  "NoData",
		"execErrState": "Alerting",
		"labels": map[string]string{
			"severity":           "warning",
			"ops_monitor_source": fmt.Sprintf("grafana-%d", configID),
		},
		"annotations": map[string]string{
			"summary":     def.Summary,
			"description": def.Description,
		},
	}

	data, status, err := c.doRequest("POST", "/api/v1/provisioning/alert-rules", payload)
	if err != nil {
		return err
	}
	if status == 409 {
		// Rule already exists, skip
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(data))
	}
	return nil
}

func (c *grafanaClient) AddNotificationRoute(contactPointName string, configID int64) error {
	// Get current policy
	data, status, err := c.doRequest("GET", "/api/v1/provisioning/policies", nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("get policies: HTTP %d", status)
	}

	var policy map[string]interface{}
	if err := json.Unmarshal(data, &policy); err != nil {
		return err
	}

	// Add our route
	newRoute := map[string]interface{}{
		"receiver": contactPointName,
		"matchers": []string{
			fmt.Sprintf("ops_monitor_source=grafana-%d", configID),
		},
		"group_wait":      "10s",
		"group_interval":  "10s",
		"repeat_interval": "1h",
	}

	routes, _ := policy["routes"].([]interface{})
	// Check if route already exists
	label := fmt.Sprintf("grafana-%d", configID)
	for _, r := range routes {
		if rm, ok := r.(map[string]interface{}); ok {
			if matchers, ok := rm["matchers"].([]interface{}); ok {
				for _, m := range matchers {
					if ms, ok := m.(string); ok && strings.Contains(ms, label) {
						return nil // already exists
					}
				}
			}
		}
	}

	routes = append(routes, newRoute)
	policy["routes"] = routes

	_, putStatus, putErr := c.doRequest("PUT", "/api/v1/provisioning/policies", policy)
	if putErr != nil {
		return putErr
	}
	if putStatus < 200 || putStatus >= 300 {
		return fmt.Errorf("update policies: HTTP %d", putStatus)
	}
	return nil
}

func (c *grafanaClient) ListAlertRules() ([]map[string]interface{}, error) {
	data, status, err := c.doRequest("GET", "/api/v1/provisioning/alert-rules", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(data))
	}
	var rules []map[string]interface{}
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (c *grafanaClient) DeleteAlertRule(uid string) error {
	_, status, err := c.doRequest("DELETE", "/api/v1/provisioning/alert-rules/"+uid, nil)
	if err != nil {
		return err
	}
	if status != 200 && status != 202 && status != 204 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

// CleanupAlertRules removes all OpsMonitor-generated alert rules for a config.
func (m *GrafanaManager) CleanupAlertRules(configID int64) (int, error) {
	cfg, err := m.store.GetGrafanaConfig(configID)
	if err != nil {
		return 0, fmt.Errorf("获取配置失败: %w", err)
	}
	client := newGrafanaClient(cfg)

	rules, err := client.ListAlertRules()
	if err != nil {
		return 0, fmt.Errorf("获取规则列表失败: %w", err)
	}

	label := fmt.Sprintf("grafana-%d", configID)
	deleted := 0
	for _, rule := range rules {
		labels, _ := rule["labels"].(map[string]interface{})
		if src, ok := labels["ops_monitor_source"].(string); ok && src == label {
			uid, _ := rule["uid"].(string)
			if uid != "" {
				if err := client.DeleteAlertRule(uid); err != nil {
					log.Printf("[Grafana %s] delete rule %s failed: %v", cfg.Name, uid, err)
				} else {
					deleted++
				}
			}
		}
	}

	// Also clean up the folder
	if cfg.FolderUID != "" {
		client.doRequest("DELETE", "/api/folders/"+cfg.FolderUID, nil)
	}

	return deleted, nil
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}
