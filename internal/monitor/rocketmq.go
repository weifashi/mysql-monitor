package monitor

import (
	"bytes"
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

// queryNewMessagesByOffset is the fallback when consumers are offline.
// It queries the topic's total maxOffset and returns the increment since the last check.
// The increment represents new messages produced but not yet consumed.
func (m *RocketMQManager) queryNewMessagesByOffset(client *http.Client, cfg *store.RocketMQConfig) (int64, error) {
	current, err := queryTopicMaxOffset(client, cfg)
	if err != nil {
		return 0, err
	}

	key := m.settingKey(cfg.ID, "maxoffset")
	lastStr := m.store.GetSetting(key)

	var last int64
	if lastStr != "" {
		fmt.Sscanf(lastStr, "%d", &last)
	}

	m.store.SetSetting(key, fmt.Sprintf("%d", current))

	if last == 0 {
		// First check — no baseline yet, report 0
		return 0, nil
	}
	delta := current - last
	if delta < 0 {
		delta = 0
	}
	return delta, nil
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

// doCheckNewMsg handles the notify_new_msg mode: queries actual messages by time range,
// deduplicates by msgId, and alerts for each new batch of unseen messages.
func (m *RocketMQManager) doCheckNewMsg(cfg *store.RocketMQConfig, client *http.Client) {
	lastKey := m.settingKey(cfg.ID, "last_msg_time")
	lastStr := m.store.GetSetting(lastKey)

	now := time.Now().UnixMilli()
	var begin int64
	if lastStr != "" {
		fmt.Sscanf(lastStr, "%d", &begin)
	} else {
		// First run: use now as baseline, don't alert
		m.store.SetSetting(lastKey, fmt.Sprintf("%d", now))
		m.emit("rocketmq_ok", cfg.ID, cfg.Name, "新消息监控已启动，等待下次检查", nil)
		return
	}

	msgs, err := queryNewMessages(client, cfg, begin, now)
	if err != nil {
		log.Printf("[RocketMQ %s] query new messages error: %v", cfg.Name, err)
		m.emit("rocketmq_error", cfg.ID, cfg.Name, fmt.Sprintf("查询错误: %v", err), nil)
		return
	}

	// Update last check time
	m.store.SetSetting(lastKey, fmt.Sprintf("%d", now))

	if len(msgs) == 0 {
		m.emit("rocketmq_ok", cfg.ID, cfg.Name, "无新消息", nil)
		return
	}

	// Build notification
	msgCount := len(msgs)
	var msgIDSnippet string
	for i, id := range msgs {
		if i >= 3 {
			msgIDSnippet += fmt.Sprintf("... 等 %d 条", msgCount)
			break
		}
		if i > 0 {
			msgIDSnippet += "\n"
		}
		msgIDSnippet += id
	}
	if msgCount <= 3 {
		msgIDSnippet = strings.Join(msgs, "\n")
	}

	m.emit("rocketmq_alert", cfg.ID, cfg.Name, fmt.Sprintf("新消息 %d 条", msgCount), map[string]interface{}{
		"msg_count": msgCount,
		"msg_ids":   msgs,
	})

	alertLog := &store.RocketMQAlertLog{
		ConfigID:      cfg.ID,
		ConfigName:    cfg.Name,
		ConsumerGroup: cfg.ConsumerGroup,
		Topic:         cfg.Topic,
		DiffTotal:     int64(msgCount),
	}
	if _, err := m.store.InsertRocketMQAlertLog(alertLog); err != nil {
		log.Printf("[RocketMQ %s] insert alert log error: %v", cfg.Name, err)
	}

	alertMsg := fmt.Sprintf("RocketMQ 新消息告警\n\n配置: %s\nTopic: %s\n消费组: %s\n新消息数: %d\n消息ID:\n%s",
		cfg.Name, cfg.Topic, cfg.ConsumerGroup, msgCount, msgIDSnippet)
	if sendErr := m.dispatcher.SendGlobalNotifications(alertMsg); sendErr != nil {
		log.Printf("[RocketMQ %s] new msg notification failed: %v", cfg.Name, sendErr)
	} else {
		m.emit("rocketmq_notified", cfg.ID, cfg.Name, fmt.Sprintf("已发送新消息告警 (%d条)", msgCount), nil)
	}
}

// queryNewMessages queries messages from the topic in the given time range and returns msgIds.
func queryNewMessages(client *http.Client, cfg *store.RocketMQConfig, beginMs, endMs int64) ([]string, error) {
	apiURL := fmt.Sprintf("%s/message/queryMessageByTopic.query?topic=%s&begin=%d&end=%d",
		strings.TrimRight(cfg.DashboardURL, "/"), cfg.Topic, beginMs, endMs)

	body, err := rocketMQGet(client, apiURL, cfg.Username, cfg.Password)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status int `json:"status"`
		Data   []struct {
			MsgID string `json:"msgId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析消息列表失败: %w", err)
	}

	var ids []string
	for _, m := range result.Data {
		if m.MsgID != "" {
			ids = append(ids, m.MsgID)
		}
	}
	return ids, nil
}

func (m *RocketMQManager) runMonitor(ctx context.Context, cfg *store.RocketMQConfig) {
	client, err := rocketMQLogin(cfg.DashboardURL, cfg.Username, cfg.Password)
	if err != nil {
		log.Printf("[RocketMQ %s] login failed: %v", cfg.Name, err)
		client = newRocketMQClient()
	}
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

	// New-message mode: alert on every new message by msgId
	if cfg.NotifyNewMsg {
		m.doCheckNewMsg(cfg, client)
		return
	}

	diffTotal, err := queryConsumerLag(client, cfg)
	// Session expired, re-login and retry
	if err != nil && err != errConsumerOffline && cfg.Username != "" && strings.Contains(err.Error(), "重定向") {
		if newClient, loginErr := rocketMQLogin(cfg.DashboardURL, cfg.Username, cfg.Password); loginErr == nil {
			// Copy cookies to existing client's jar
			*client = *newClient
			diffTotal, err = queryConsumerLag(client, cfg)
		}
	}

	// Consumer offline: fall back to topic maxOffset increment detection
	if err == errConsumerOffline {
		diffTotal, err = m.queryNewMessagesByOffset(client, cfg)
	}

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

// errConsumerOffline is returned when the consumer group has no online instances.
var errConsumerOffline = fmt.Errorf("consumer offline")

// queryConsumerLag queries RocketMQ Dashboard API for consumer lag on the configured topic.
// Returns errConsumerOffline if the consumer group is offline (so callers can fall back).
func queryConsumerLag(client *http.Client, cfg *store.RocketMQConfig) (int64, error) {
	apiURL := strings.TrimRight(cfg.DashboardURL, "/") + "/consumer/queryTopicByConsumer.query?consumerGroup=" + cfg.ConsumerGroup

	body, err := rocketMQGet(client, apiURL, cfg.Username, cfg.Password)
	if err != nil {
		return 0, err
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

	// status=-1 with null data means consumer group has no online instances
	if result.Status == -1 || result.Data == nil {
		return 0, errConsumerOffline
	}

	for _, item := range result.Data {
		if item.Topic == cfg.Topic {
			return item.DiffTotal, nil
		}
	}

	// topic not in the list → consumer group online but not subscribed to this topic
	return 0, errConsumerOffline
}

// queryTopicMaxOffset queries the total maxOffset across all queues for a topic.
// This works even when consumers are offline.
func queryTopicMaxOffset(client *http.Client, cfg *store.RocketMQConfig) (int64, error) {
	apiURL := strings.TrimRight(cfg.DashboardURL, "/") + "/topic/stats.query?topic=" + cfg.Topic
	body, err := rocketMQGet(client, apiURL, cfg.Username, cfg.Password)
	if err != nil {
		return 0, err
	}

	var result struct {
		Status int `json:"status"`
		Data   struct {
			OffsetTable map[string]struct {
				MaxOffset int64 `json:"maxOffset"`
			} `json:"offsetTable"`
		} `json:"data"`
		ErrMsg string `json:"errMsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析 topic stats 失败: %w", err)
	}
	if result.Status != 0 {
		return 0, fmt.Errorf("topic stats 查询失败: %s", result.ErrMsg)
	}

	var total int64
	for _, q := range result.Data.OffsetTable {
		total += q.MaxOffset
	}
	return total, nil
}

// TestRocketMQConnection tests connectivity to RocketMQ Dashboard.
func TestRocketMQConnection(cfg *store.RocketMQConfig) error {
	client, err := rocketMQLogin(cfg.DashboardURL, cfg.Username, cfg.Password)
	if err != nil {
		return err
	}

	apiURL := strings.TrimRight(cfg.DashboardURL, "/") + "/topic/list.query?skipSysProcess=true&skipRetryAndDlq=true"
	_, err = rocketMQGet(client, apiURL, cfg.Username, cfg.Password)
	return err
}

// rocketMQLogin performs form-based login to RocketMQ Dashboard and returns an authenticated client.
func rocketMQLogin(dashboardURL, username, password string) (*http.Client, error) {
	client := newRocketMQClient()
	baseURL := strings.TrimRight(dashboardURL, "/")

	if username == "" {
		return client, nil
	}

	// RocketMQ Dashboard login API: POST /login/login.do?username=xxx&password=xxx
	loginURL := fmt.Sprintf("%s/login/login.do?username=%s&password=%s", baseURL, username, password)
	loginReq, err := http.NewRequest("POST", loginURL, bytes.NewReader([]byte{}))
	if err != nil {
		return nil, err
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		return nil, fmt.Errorf("登录失败: %w", err)
	}
	defer loginResp.Body.Close()
	body, _ := io.ReadAll(loginResp.Body)

	if loginResp.StatusCode == 200 {
		var result struct {
			Status int `json:"status"`
		}
		json.Unmarshal(body, &result)
		if result.Status != 0 {
			return nil, fmt.Errorf("登录失败，请检查用户名和密码")
		}
	}

	return client, nil
}

func rocketMQGet(client *http.Client, url, username, password string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 302 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("认证失败 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return body, nil
}

// ListRocketMQConsumerGroups fetches consumer group list from RocketMQ Dashboard.
func ListRocketMQConsumerGroups(dashboardURL, username, password string) ([]string, error) {
	client, err := rocketMQLogin(dashboardURL, username, password)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(dashboardURL, "/")
	body, err := rocketMQGet(client, baseURL+"/consumer/groupList.query", username, password)
	if err != nil {
		return nil, err
	}
	// Response: {"data": [{"group": "xxx", "subGroupType": "NORMAL"|"SYSTEM", ...}]}
	var result struct {
		Data []struct {
			Group        string `json:"group"`
			SubGroupType string `json:"subGroupType"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}
	var groups []string
	for _, g := range result.Data {
		if g.SubGroupType != "SYSTEM" {
			groups = append(groups, g.Group)
		}
	}
	return groups, nil
}

// ListRocketMQTopics fetches topic list from RocketMQ Dashboard.
func ListRocketMQTopics(dashboardURL, username, password string) ([]string, error) {
	client, err := rocketMQLogin(dashboardURL, username, password)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(dashboardURL, "/")
	body, err := rocketMQGet(client, baseURL+"/topic/list.query?skipSysProcess=true&skipRetryAndDlq=true", username, password)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data struct {
			TopicList []string `json:"topicList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}
	var topics []string
	for _, t := range result.Data.TopicList {
		// Skip system topics
		if strings.HasPrefix(t, "RMQ_SYS_") || strings.HasPrefix(t, "rmq_sys_") ||
			strings.HasPrefix(t, "%SYS%") || strings.HasPrefix(t, "SCHEDULE_TOPIC") ||
			strings.HasPrefix(t, "DefaultCluster") || t == "BenchmarkTest" ||
			t == "SELF_TEST_TOPIC" || t == "TBW102" {
			continue
		}
		topics = append(topics, t)
	}
	return topics, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
