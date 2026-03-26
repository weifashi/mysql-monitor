package web

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	"mysql-monitor/internal/auth"
	"mysql-monitor/internal/monitor"
	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

// --- Helpers ---

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) getSession(r *http.Request) *auth.Session {
	token := auth.GetSessionToken(r)
	sess, _ := s.auth.Validate(token)
	return sess
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	sess := s.getSession(r)
	if sess == nil || sess.Role != "admin" {
		jsonError(w, http.StatusForbidden, "admin required")
		return false
	}
	return true
}

// --- Audit ---

func (s *Server) audit(r *http.Request, action, target string, targetID int64, detail string) {
	user := ""
	if sess := s.getSession(r); sess != nil {
		user = sess.Username
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	s.store.InsertAuditLog(&store.AuditLog{
		User: user, Action: action, Target: target, TargetID: targetID, Detail: detail, IP: ip,
	})
}

// --- Auth ---

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	// Check if password login is enabled
	if s.store.GetSetting("password_login_enabled") == "0" {
		jsonError(w, http.StatusForbidden, "密码登录已关闭，请使用 GitHub 登录")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}

	token, ok := s.auth.Login(req.Username, req.Password)
	if !ok {
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	auth.SetSessionCookie(w, token)
	s.audit(r, "login", "auth", 0, "用户 "+req.Username+" 密码登录")
	jsonOK(w, map[string]any{
		"username": req.Username,
		"role":     "admin",
	})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	s.audit(r, "logout", "auth", 0, "用户登出")
	token := auth.GetSessionToken(r)
	if token != "" {
		s.auth.Logout(token)
	}
	auth.ClearSessionCookie(w)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	sess := s.getSession(r)
	if sess == nil {
		jsonError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	jsonOK(w, map[string]any{
		"username":     sess.Username,
		"role":         sess.Role,
		"github_login": sess.GitHubLogin,
		"avatar_url":   sess.AvatarURL,
	})
}

func (s *Server) apiAuthConfig(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetAllSettings()
	jsonOK(w, map[string]any{
		"github_enabled":         settings["github_enabled"] == "1",
		"password_login_enabled": settings["password_login_enabled"] != "0",
		"github_client_id":       settings["github_client_id"],
	})
}

func (s *Server) oauthGitHubRedirectURI(r *http.Request) string {
	if base := strings.TrimSpace(strings.TrimSuffix(s.store.GetSetting("oauth_public_base_url"), "/")); base != "" {
		return base + "/api/auth/github/callback"
	}
	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/api/auth/github/callback"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		p := strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
		if p == "https" {
			scheme = "https"
		}
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	return fmt.Sprintf("%s://%s/api/auth/github/callback", scheme, host)
}

func (s *Server) apiGitHubLogin(w http.ResponseWriter, r *http.Request) {
	clientID := s.store.GetSetting("github_client_id")
	if clientID == "" {
		clientID = s.auth.GitHub.ClientID
	}
	if clientID == "" {
		jsonError(w, http.StatusBadRequest, "GitHub OAuth 未配置")
		return
	}

	redirectURI := s.oauthGitHubRedirectURI(r)

	s.auth.GitHub.ClientID = clientID
	url := s.auth.GetGitHubAuthURL(redirectURI)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) apiGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/#/login?error=no_code", http.StatusTemporaryRedirect)
		return
	}

	// Load secret from DB or env
	clientSecret := s.store.GetSetting("github_client_secret")
	if clientSecret == "" {
		clientSecret = s.auth.GitHub.ClientSecret
	}
	clientID := s.store.GetSetting("github_client_id")
	if clientID == "" {
		clientID = s.auth.GitHub.ClientID
	}
	s.auth.GitHub.ClientID = clientID
	s.auth.GitHub.ClientSecret = clientSecret

	ghUser, err := s.auth.ExchangeGitHubCode(code)
	if err != nil {
		log.Printf("GitHub OAuth error: %v", err)
		http.Redirect(w, r, "/#/login?error=oauth_failed", http.StatusTemporaryRedirect)
		return
	}

	// Check if user is in allowed list
	user, err := s.store.GetUserByGitHubLogin(ghUser.Login)
	if err != nil {
		// Also try by github_id
		user, err = s.store.GetUserByGitHubID(ghUser.ID)
	}
	if err != nil {
		log.Printf("GitHub user %s not allowed", ghUser.Login)
		http.Redirect(w, r, "/#/login?error=not_allowed", http.StatusTemporaryRedirect)
		return
	}

	// Update user's GitHub info
	s.store.UpdateUserGitHub(user.ID, ghUser.ID, ghUser.Login, ghUser.AvatarURL)

	token := s.auth.LoginGitHub(user.ID, user.Username, ghUser.Login, ghUser.AvatarURL, user.Role)
	auth.SetSessionCookie(w, token)
	s.audit(r, "login", "auth", 0, "用户 "+ghUser.Login+" GitHub 登录")
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// --- Dashboard ---

func (s *Server) apiDashboardStats(w http.ResponseWriter, r *http.Request) {
	var (
		totalDBs, enabledDBs, todayCount, weekCount int
		rocketmqConfigs, rocketmqAlertsToday        int
		healthCheckCount, healthCheckErrorsToday    int
		grafanaConfigs, grafanaAlertsToday          int
		recentLogs                                   []store.SlowQueryLog
		wg                                           sync.WaitGroup
	)
	wg.Add(11)
	go func() { defer wg.Done(); totalDBs, _ = s.store.CountDatabases() }()
	go func() { defer wg.Done(); enabledDBs, _ = s.store.CountEnabledDatabases() }()
	go func() { defer wg.Done(); todayCount, _ = s.store.CountSlowQueriesToday() }()
	go func() { defer wg.Done(); weekCount, _ = s.store.CountSlowQueriesWeek() }()
	go func() { defer wg.Done(); recentLogs, _, _ = s.store.ListSlowQueryLogs(nil, 1, 10) }()
	go func() { defer wg.Done(); rocketmqConfigs, _ = s.store.CountRocketMQConfigs() }()
	go func() { defer wg.Done(); rocketmqAlertsToday, _ = s.store.CountRocketMQAlertsToday() }()
	go func() { defer wg.Done(); healthCheckCount, _ = s.store.CountHealthChecks() }()
	go func() { defer wg.Done(); healthCheckErrorsToday, _ = s.store.CountHealthCheckErrorsToday() }()
	go func() { defer wg.Done(); grafanaConfigs, _ = s.store.CountGrafanaConfigs() }()
	go func() { defer wg.Done(); grafanaAlertsToday, _ = s.store.CountGrafanaAlertsToday() }()
	wg.Wait()

	jsonOK(w, map[string]any{
		"total_dbs":              totalDBs,
		"enabled_dbs":           enabledDBs,
		"running_dbs":           s.manager.RunningCount(),
		"today_count":           todayCount,
		"week_count":            weekCount,
		"recent_logs":           recentLogs,
		"rocketmq_configs":      rocketmqConfigs,
		"rocketmq_running":      s.rocketMQMgr.RunningCount(),
		"rocketmq_alerts_today": rocketmqAlertsToday,
		"health_checks":              healthCheckCount,
		"health_checks_running":      s.healthCheckMgr.RunningCount(),
		"health_check_errors_today": healthCheckErrorsToday,
		"grafana_configs":       grafanaConfigs,
		"grafana_running":       s.grafanaMgr.RunningCount(),
		"grafana_alerts_today":  grafanaAlertsToday,
	})
}

// --- Databases ---

type dbWithStatus struct {
	store.Database
	Running bool `json:"running"`
}

func (s *Server) apiDatabasesList(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.store.ListDatabases()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var list []dbWithStatus
	for _, db := range dbs {
		d := dbWithStatus{Database: db, Running: s.manager.IsRunning(db.ID)}
		d.Password = "" // never send password
		list = append(list, d)
	}
	if list == nil {
		list = []dbWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiDatabaseCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		User         string `json:"user"`
		Password     string `json:"password"`
		IntervalSec  int    `json:"interval_sec"`
		ThresholdSec int    `json:"threshold_sec"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.User) == "" {
		jsonError(w, http.StatusBadRequest, "名称、主机和用户不能为空")
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 3306
	}
	if req.IntervalSec < 1 {
		req.IntervalSec = 10
	}
	if req.ThresholdSec < 1 {
		req.ThresholdSec = 10
	}

	db := &store.Database{
		Name:         strings.TrimSpace(req.Name),
		Host:         strings.TrimSpace(req.Host),
		Port:         req.Port,
		User:         strings.TrimSpace(req.User),
		Password:     req.Password,
		IntervalSec:  req.IntervalSec,
		ThresholdSec: req.ThresholdSec,
		Enabled:      true,
	}

	id, err := s.store.CreateDatabase(db)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.manager.StartDatabase(id); err != nil {
		log.Printf("start monitor for new db %d: %v", id, err)
	}

	s.audit(r, "create", "database", id, "创建数据库 "+db.Name)
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiDatabaseUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req struct {
		Name         string `json:"name"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		User         string `json:"user"`
		Password     string `json:"password"`
		IntervalSec  int    `json:"interval_sec"`
		ThresholdSec int    `json:"threshold_sec"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	password := req.Password
	if password == "" {
		if existing, err := s.store.GetDatabase(id); err == nil {
			password = existing.Password
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	db := &store.Database{
		ID:           id,
		Name:         req.Name,
		Host:         req.Host,
		Port:         req.Port,
		User:         req.User,
		Password:     password,
		IntervalSec:  req.IntervalSec,
		ThresholdSec: req.ThresholdSec,
		Enabled:      enabled,
	}

	if err := s.store.UpdateDatabase(db); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if db.Enabled {
		s.manager.RestartDatabase(id)
	} else {
		s.manager.StopDatabase(id)
	}

	s.audit(r, "update", "database", id, "更新数据库 "+db.Name)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiDatabaseDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.manager.StopDatabase(id)
	if err := s.store.DeleteDatabase(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "database", id, fmt.Sprintf("删除数据库 ID=%d", id))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiDatabaseToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleDatabase(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	db, err := s.store.GetDatabase(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if db.Enabled {
		s.manager.StartDatabase(id)
	} else {
		s.manager.StopDatabase(id)
	}

	action := "启用"
	if !db.Enabled {
		action = "禁用"
	}
	s.audit(r, "toggle", "database", id, action+"数据库 "+db.Name)
	jsonOK(w, map[string]any{"enabled": db.Enabled})
}

func (s *Server) apiDatabaseTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	db, err := s.store.GetDatabase(id)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "获取配置失败")
		return
	}

	testDB, err := openAndPingMySQL(db.DSN())
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": "连接失败: " + err.Error()})
		return
	}
	testDB.Close()
	jsonOK(w, map[string]any{"ok": true, "message": "连接成功"})
}

func openAndPingMySQL(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// --- Notifications ---

func (s *Server) apiNotificationsList(w http.ResponseWriter, r *http.Request) {
	configs, err := s.store.ListNotificationConfigs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build name maps for all scope types
	nameMap := make(map[string]map[int64]string) // scopeType -> id -> name
	dbs, _ := s.store.ListDatabases()
	dbMap := make(map[int64]string)
	for _, db := range dbs {
		dbMap[db.ID] = db.Name
	}
	nameMap["mysql"] = dbMap

	hcList, _ := s.store.ListHealthChecks()
	hcMap := make(map[int64]string)
	for _, h := range hcList {
		hcMap[h.ID] = h.Name
	}
	nameMap["health"] = hcMap

	rmqList, _ := s.store.ListRocketMQConfigs()
	rmqMap := make(map[int64]string)
	for _, r := range rmqList {
		rmqMap[r.ID] = r.Name
	}
	nameMap["rocketmq"] = rmqMap

	gfList, _ := s.store.ListGrafanaConfigs()
	gfMap := make(map[int64]string)
	for _, g := range gfList {
		gfMap[g.ID] = g.Name
	}
	nameMap["grafana"] = gfMap

	type ncDisplay struct {
		store.NotificationConfig
		ScopeName     string `json:"scope_name"`
		ConfigSummary string `json:"config_summary"`
	}
	var list []ncDisplay
	for _, nc := range configs {
		d := ncDisplay{NotificationConfig: nc}
		if nc.ScopeType == "all" || nc.ScopeType == "" {
			d.ScopeName = "全局"
		} else if nc.DatabaseID != nil {
			if m, ok := nameMap[nc.ScopeType]; ok {
				d.ScopeName = m[*nc.DatabaseID]
			}
		}
		d.ConfigSummary = getConfigSummary(nc)
		list = append(list, d)
	}
	if list == nil {
		list = []ncDisplay{}
	}
	jsonOK(w, list)
}

func getConfigSummary(nc store.NotificationConfig) string {
	switch nc.Type {
	case "dingtalk":
		var c store.DingTalkConfig
		if json.Unmarshal(nc.ConfigJSON, &c) == nil && c.Webhook != "" {
			return "Webhook: " + truncateStr(c.Webhook, 40)
		}
	case "feishu":
		var c store.FeishuConfig
		if json.Unmarshal(nc.ConfigJSON, &c) == nil && c.Webhook != "" {
			return "Webhook: " + truncateStr(c.Webhook, 40)
		}
	case "email":
		var c store.EmailConfig
		if json.Unmarshal(nc.ConfigJSON, &c) == nil && c.To != "" {
			return fmt.Sprintf("%s -> %s", c.From, c.To)
		}
	case "dootask":
		var c store.DooTaskConfig
		if json.Unmarshal(nc.ConfigJSON, &c) == nil && c.BaseURL != "" {
			return fmt.Sprintf("DooTask: %s (dialog: %s)", truncateStr(c.BaseURL, 30), c.DialogID)
		}
	}
	return "-"
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (s *Server) apiNotificationCreate(w http.ResponseWriter, r *http.Request) {
	nc, err := parseNotificationJSON(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.CreateNotificationConfig(nc)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "create", "notification", id, fmt.Sprintf("创建通知配置 类型:%s", nc.Type))
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiNotificationUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	nc, err := parseNotificationJSON(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	nc.ID = id
	if err := s.store.UpdateNotificationConfig(nc); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "update", "notification", id, fmt.Sprintf("更新通知配置 类型:%s", nc.Type))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiNotificationDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteNotificationConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "notification", id, "删除通知配置")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiNotificationTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	nc, err := s.store.GetNotificationConfig(id)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": "配置不存在"})
		return
	}
	if err := notify.SendTestNotification(nc); err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": "发送失败: " + err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true, "message": "发送成功"})
}

func parseNotificationJSON(r *http.Request) (*store.NotificationConfig, error) {
	var req struct {
		Type       string `json:"type"`
		ScopeType  string `json:"scope_type"`
		DatabaseID *int64 `json:"database_id"`
		Enabled    *bool  `json:"enabled"`
		// Webhook fields
		Webhook string `json:"webhook"`
		Secret  string `json:"secret"`
		// Email fields
		SMTPHost     string `json:"smtp_host"`
		SMTPPort     int    `json:"smtp_port"`
		SMTPUsername string `json:"smtp_username"`
		SMTPPassword string `json:"smtp_password"`
		EmailFrom    string `json:"email_from"`
		EmailTo      string `json:"email_to"`
		// DooTask fields
		DootaskBaseURL  string `json:"dootask_base_url"`
		DootaskToken    string `json:"dootask_token"`
		DootaskDialogID string `json:"dootask_dialog_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	nc := &store.NotificationConfig{
		Type:      req.Type,
		ScopeType: req.ScopeType,
		Enabled:   true,
	}
	if nc.ScopeType == "" {
		nc.ScopeType = "all"
	}
	if req.Enabled != nil {
		nc.Enabled = *req.Enabled
	}
	if req.DatabaseID != nil && *req.DatabaseID > 0 {
		nc.DatabaseID = req.DatabaseID
	}

	var configJSON []byte
	var err error
	switch nc.Type {
	case "dingtalk":
		configJSON, err = json.Marshal(store.DingTalkConfig{
			Webhook: strings.TrimSpace(req.Webhook),
			Secret:  strings.TrimSpace(req.Secret),
		})
	case "feishu":
		configJSON, err = json.Marshal(store.FeishuConfig{
			Webhook: strings.TrimSpace(req.Webhook),
			Secret:  strings.TrimSpace(req.Secret),
		})
	case "email":
		port := req.SMTPPort
		if port == 0 {
			port = 587
		}
		configJSON, err = json.Marshal(store.EmailConfig{
			SMTPHost: strings.TrimSpace(req.SMTPHost),
			SMTPPort: port,
			Username: strings.TrimSpace(req.SMTPUsername),
			Password: strings.TrimSpace(req.SMTPPassword),
			From:     strings.TrimSpace(req.EmailFrom),
			To:       strings.TrimSpace(req.EmailTo),
		})
	case "dootask":
		configJSON, err = json.Marshal(store.DooTaskConfig{
			BaseURL:  strings.TrimSpace(req.DootaskBaseURL),
			Token:    strings.TrimSpace(req.DootaskToken),
			DialogID: strings.TrimSpace(req.DootaskDialogID),
		})
	default:
		return nil, fmt.Errorf("unknown notification type: %s", nc.Type)
	}
	if err != nil {
		return nil, err
	}
	nc.ConfigJSON = configJSON
	return nc, nil
}

func (s *Server) apiNotificationScopes(w http.ResponseWriter, r *http.Request) {
	type scopeItem struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	result := make(map[string][]scopeItem)

	dbs, _ := s.store.ListDatabases()
	var mysqlItems []scopeItem
	for _, db := range dbs {
		mysqlItems = append(mysqlItems, scopeItem{ID: db.ID, Name: db.Name})
	}
	result["mysql"] = mysqlItems

	hcList, _ := s.store.ListHealthChecks()
	var healthItems []scopeItem
	for _, h := range hcList {
		healthItems = append(healthItems, scopeItem{ID: h.ID, Name: h.Name})
	}
	result["health"] = healthItems

	rmqList, _ := s.store.ListRocketMQConfigs()
	var rmqItems []scopeItem
	for _, r := range rmqList {
		rmqItems = append(rmqItems, scopeItem{ID: r.ID, Name: r.Name})
	}
	result["rocketmq"] = rmqItems

	gfList, _ := s.store.ListGrafanaConfigs()
	var gfItems []scopeItem
	for _, g := range gfList {
		gfItems = append(gfItems, scopeItem{ID: g.ID, Name: g.Name})
	}
	result["grafana"] = gfItems

	jsonOK(w, result)
}

// --- Slow Queries ---

func (s *Server) apiSlowQueries(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 50

	var dbID *int64
	if dbIDStr := r.URL.Query().Get("database_id"); dbIDStr != "" {
		id, _ := strconv.ParseInt(dbIDStr, 10, 64)
		if id > 0 {
			dbID = &id
		}
	}

	logs, total, err := s.store.ListSlowQueryLogs(dbID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.SlowQueryLog{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	jsonOK(w, map[string]any{
		"logs":        logs,
		"total":       total,
		"page":        page,
		"total_pages": totalPages,
	})
}

// --- Users ---

func (s *Server) apiUsersList(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	users, err := s.store.ListUsers()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []store.User{}
	}
	jsonOK(w, users)
}

func (s *Server) apiUserCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Username    string `json:"username"`
		GitHubLogin string `json:"github_login"`
		Role        string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.GitHubLogin == "" {
		jsonError(w, http.StatusBadRequest, "github_login is required")
		return
	}
	if req.Username == "" {
		req.Username = req.GitHubLogin
	}
	if req.Role == "" {
		req.Role = "member"
	}

	id, err := s.store.CreateUser(&store.User{
		Username:    req.Username,
		GitHubLogin: req.GitHubLogin,
		Role:        req.Role,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "create", "user", id, fmt.Sprintf("创建用户 %s (角色:%s)", req.Username, req.Role))
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "user", id, "删除用户")
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Settings ---

func (s *Server) apiSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	settings := s.store.GetAllSettings()
	// Don't expose secret
	delete(settings, "github_client_secret")
	jsonOK(w, settings)
}

func (s *Server) apiSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	allowed := map[string]bool{
		"github_client_id":        true,
		"github_client_secret":    true,
		"github_enabled":          true,
		"password_login_enabled":  true,
		"oauth_public_base_url":   true,
	}
	var changed []string
	for k, v := range req {
		if !allowed[k] {
			continue
		}
		if err := s.store.SetSetting(k, v); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, k)
	}
	if len(changed) > 0 {
		s.audit(r, "update", "settings", 0, fmt.Sprintf("更新设置: %s", strings.Join(changed, ", ")))
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Databases list for selectors ---

func (s *Server) apiDatabasesSimpleList(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.store.ListDatabases()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type simple struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var list []simple
	for _, db := range dbs {
		list = append(list, simple{ID: db.ID, Name: db.Name})
	}
	if list == nil {
		list = []simple{}
	}
	jsonOK(w, list)
}

// --- RocketMQ ---

type rocketMQWithStatus struct {
	store.RocketMQConfig
	Running bool `json:"running"`
}

func (s *Server) apiRocketMQList(w http.ResponseWriter, r *http.Request) {
	configs, err := s.store.ListRocketMQConfigs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var list []rocketMQWithStatus
	for _, c := range configs {
		item := rocketMQWithStatus{RocketMQConfig: c, Running: s.rocketMQMgr.IsRunning(c.ID)}
		item.Password = ""
		list = append(list, item)
	}
	if list == nil {
		list = []rocketMQWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiRocketMQCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		DashboardURL  string `json:"dashboard_url"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		ConsumerGroup string `json:"consumer_group"`
		Topic         string `json:"topic"`
		Threshold     int    `json:"threshold"`
		IntervalSec   int    `json:"interval_sec"`
		NotifyNewMsg  bool   `json:"notify_new_msg"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" || req.DashboardURL == "" || req.ConsumerGroup == "" || req.Topic == "" {
		jsonError(w, http.StatusBadRequest, "name, dashboard_url, consumer_group, topic are required")
		return
	}
	if req.Threshold <= 0 {
		req.Threshold = 1000
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 30
	}

	cfg := &store.RocketMQConfig{
		Name: req.Name, DashboardURL: req.DashboardURL,
		Username: req.Username, Password: req.Password,
		ConsumerGroup: req.ConsumerGroup, Topic: req.Topic,
		Threshold: req.Threshold, IntervalSec: req.IntervalSec,
		NotifyNewMsg: req.NotifyNewMsg,
		Enabled: true,
	}
	id, err := s.store.CreateRocketMQConfig(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.rocketMQMgr.Start(id); err != nil {
		log.Printf("start rocketmq monitor %d: %v", id, err)
	}
	s.audit(r, "create", "rocketmq", id, fmt.Sprintf("创建RocketMQ监控 %s", req.Name))
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) apiRocketMQUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetRocketMQConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Name          string `json:"name"`
		DashboardURL  string `json:"dashboard_url"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		ConsumerGroup string `json:"consumer_group"`
		Topic         string `json:"topic"`
		Threshold     int    `json:"threshold"`
		IntervalSec   int    `json:"interval_sec"`
		NotifyNewMsg  *bool  `json:"notify_new_msg"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.DashboardURL != "" {
		existing.DashboardURL = req.DashboardURL
	}
	existing.Username = req.Username
	if req.Password != "" {
		existing.Password = req.Password
	}
	if req.ConsumerGroup != "" {
		existing.ConsumerGroup = req.ConsumerGroup
	}
	if req.Topic != "" {
		existing.Topic = req.Topic
	}
	if req.Threshold > 0 {
		existing.Threshold = req.Threshold
	}
	if req.IntervalSec > 0 {
		existing.IntervalSec = req.IntervalSec
	}
	if req.NotifyNewMsg != nil {
		existing.NotifyNewMsg = *req.NotifyNewMsg
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := s.store.UpdateRocketMQConfig(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.rocketMQMgr.Stop(id)
	if existing.Enabled {
		if err := s.rocketMQMgr.Start(id); err != nil {
			log.Printf("restart rocketmq monitor %d: %v", id, err)
		}
	}
	s.audit(r, "update", "rocketmq", id, fmt.Sprintf("更新RocketMQ监控 %s", existing.Name))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiRocketMQDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.rocketMQMgr.Stop(id)
	if err := s.store.DeleteRocketMQConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "rocketmq", id, "删除RocketMQ监控")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiRocketMQToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleRocketMQ(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := s.store.GetRocketMQConfig(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg.Enabled {
		s.rocketMQMgr.Start(id)
	} else {
		s.rocketMQMgr.Stop(id)
	}
	action := "启用"
	if !cfg.Enabled {
		action = "禁用"
	}
	s.audit(r, "toggle", "rocketmq", id, action+"RocketMQ监控 "+cfg.Name)
	jsonOK(w, map[string]bool{"enabled": cfg.Enabled})
}

func (s *Server) apiRocketMQTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetRocketMQConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if err := monitor.TestRocketMQConnection(cfg); err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true, "message": "连接成功"})
}

func (s *Server) rocketMQResolveCredentials(r *http.Request) (dashboardURL, username, password string, err error) {
	var req struct {
		ConfigID     int64  `json:"config_id"`
		DashboardURL string `json:"dashboard_url"`
		Username     string `json:"username"`
		Password     string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return "", "", "", fmt.Errorf("invalid json")
	}
	if req.ConfigID > 0 {
		cfg, err := s.store.GetRocketMQConfig(req.ConfigID)
		if err != nil {
			return "", "", "", fmt.Errorf("配置不存在")
		}
		return cfg.DashboardURL, cfg.Username, cfg.Password, nil
	}
	return req.DashboardURL, req.Username, req.Password, nil
}

func (s *Server) apiRocketMQConsumerGroups(w http.ResponseWriter, r *http.Request) {
	dashboardURL, username, password, err := s.rocketMQResolveCredentials(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	groups, err := monitor.ListRocketMQConsumerGroups(dashboardURL, username, password)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, groups)
}

func (s *Server) apiRocketMQTopics(w http.ResponseWriter, r *http.Request) {
	dashboardURL, username, password, err := s.rocketMQResolveCredentials(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	topics, err := monitor.ListRocketMQTopics(dashboardURL, username, password)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, topics)
}

func (s *Server) apiRocketMQAlerts(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}
	var configID *int64
	if cid := r.URL.Query().Get("config_id"); cid != "" {
		if v, err := strconv.ParseInt(cid, 10, 64); err == nil && v > 0 {
			configID = &v
		}
	}

	logs, total, err := s.store.ListRocketMQAlertLogs(configID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RocketMQAlertLog{}
	}
	jsonOK(w, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

// --- Audit Logs ---

func (s *Server) apiAuditLogs(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 50
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	logs, total, err := s.store.ListAuditLogs(page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	jsonOK(w, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

// --- Health Checks ---

type healthCheckWithStatus struct {
	store.HealthCheck
	Running bool `json:"running"`
}

func (s *Server) apiHealthCheckList(w http.ResponseWriter, r *http.Request) {
	checks, err := s.store.ListHealthChecks()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var list []healthCheckWithStatus
	for _, c := range checks {
		list = append(list, healthCheckWithStatus{HealthCheck: c, Running: s.healthCheckMgr.IsRunning(c.ID)})
	}
	if list == nil {
		list = []healthCheckWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiHealthCheckCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		URL            string `json:"url"`
		Method         string `json:"method"`
		HeadersJSON    string `json:"headers_json"`
		Body           string `json:"body"`
		ExpectedStatus int    `json:"expected_status"`
		ExpectedField  string `json:"expected_field"`
		ExpectedValue  string `json:"expected_value"`
		TimeoutSec     int    `json:"timeout_sec"`
		IntervalSec    int    `json:"interval_sec"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" || req.URL == "" {
		jsonError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.ExpectedStatus <= 0 {
		req.ExpectedStatus = 200
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 10
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 30
	}
	if req.HeadersJSON == "" {
		req.HeadersJSON = "{}"
	}

	cfg := &store.HealthCheck{
		Name: req.Name, URL: req.URL, Method: req.Method,
		HeadersJSON: req.HeadersJSON, Body: req.Body,
		ExpectedStatus: req.ExpectedStatus, ExpectedField: req.ExpectedField, ExpectedValue: req.ExpectedValue,
		TimeoutSec: req.TimeoutSec, IntervalSec: req.IntervalSec,
		Enabled: true,
	}
	id, err := s.store.CreateHealthCheck(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.healthCheckMgr.Start(id); err != nil {
		log.Printf("start health check %d: %v", id, err)
	}
	s.audit(r, "create", "healthcheck", id, fmt.Sprintf("创建健康检查 %s", req.Name))
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) apiHealthCheckUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetHealthCheck(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Name           string  `json:"name"`
		URL            string  `json:"url"`
		Method         string  `json:"method"`
		HeadersJSON    string  `json:"headers_json"`
		Body           *string `json:"body"`
		ExpectedStatus int     `json:"expected_status"`
		ExpectedField  *string `json:"expected_field"`
		ExpectedValue  *string `json:"expected_value"`
		TimeoutSec     int     `json:"timeout_sec"`
		IntervalSec    int     `json:"interval_sec"`
		Enabled        *bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.URL != "" {
		existing.URL = req.URL
	}
	if req.Method != "" {
		existing.Method = req.Method
	}
	if req.HeadersJSON != "" {
		existing.HeadersJSON = req.HeadersJSON
	}
	if req.Body != nil {
		existing.Body = *req.Body
	}
	if req.ExpectedStatus > 0 {
		existing.ExpectedStatus = req.ExpectedStatus
	}
	if req.ExpectedField != nil {
		existing.ExpectedField = *req.ExpectedField
	}
	if req.ExpectedValue != nil {
		existing.ExpectedValue = *req.ExpectedValue
	}
	if req.TimeoutSec > 0 {
		existing.TimeoutSec = req.TimeoutSec
	}
	if req.IntervalSec > 0 {
		existing.IntervalSec = req.IntervalSec
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := s.store.UpdateHealthCheck(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.healthCheckMgr.Stop(id)
	if existing.Enabled {
		if err := s.healthCheckMgr.Start(id); err != nil {
			log.Printf("restart health check %d: %v", id, err)
		}
	}
	s.audit(r, "update", "healthcheck", id, fmt.Sprintf("更新健康检查 %s", existing.Name))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiHealthCheckDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.healthCheckMgr.Stop(id)
	if err := s.store.DeleteHealthCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "healthcheck", id, fmt.Sprintf("删除健康检查 ID=%d", id))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiHealthCheckToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleHealthCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := s.store.GetHealthCheck(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg.Enabled {
		s.healthCheckMgr.Start(id)
	} else {
		s.healthCheckMgr.Stop(id)
	}
	action := "启用"
	if !cfg.Enabled {
		action = "禁用"
	}
	s.audit(r, "toggle", "healthcheck", id, action+"健康检查 "+cfg.Name)
	jsonOK(w, map[string]bool{"enabled": cfg.Enabled})
}

func (s *Server) apiHealthCheckTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetHealthCheck(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	result := monitor.TestHealthCheck(cfg)
	jsonOK(w, map[string]any{
		"ok":          result.Status == "up",
		"status":      result.Status,
		"http_status": result.HTTPStatus,
		"latency_ms":  result.LatencyMs,
		"error":       result.Error,
		"response":    result.Response,
	})
}

func (s *Server) apiHealthCheckLogs(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}
	var checkID *int64
	if cid := r.URL.Query().Get("check_id"); cid != "" {
		if v, err := strconv.ParseInt(cid, 10, 64); err == nil && v > 0 {
			checkID = &v
		}
	}

	logs, total, err := s.store.ListHealthCheckLogs(checkID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.HealthCheckLog{}
	}
	jsonOK(w, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

// --- Grafana ---

type grafanaWithStatus struct {
	store.GrafanaConfig
	Running bool `json:"running"`
}

func (s *Server) apiGrafanaList(w http.ResponseWriter, r *http.Request) {
	configs, err := s.store.ListGrafanaConfigs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var list []grafanaWithStatus
	for _, c := range configs {
		item := grafanaWithStatus{GrafanaConfig: c, Running: s.grafanaMgr.IsRunning(c.ID)}
		item.Password = ""
		list = append(list, item)
	}
	if list == nil {
		list = []grafanaWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiGrafanaCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string   `json:"name"`
		GrafanaURL    string   `json:"grafana_url"`
		Username      string   `json:"username"`
		Password      string   `json:"password"`
		DatasourceUID string   `json:"datasource_uid"`
		AutoRules     []string `json:"auto_rules"`
		WebhookURL    string   `json:"webhook_url"`
		WebhookSecret string   `json:"webhook_secret"`
		IntervalSec   int      `json:"interval_sec"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" || req.GrafanaURL == "" {
		jsonError(w, http.StatusBadRequest, "name 和 grafana_url 不能为空")
		return
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 60
	}

	rulesJSON, _ := json.Marshal(req.AutoRules)
	cfg := &store.GrafanaConfig{
		Name:          req.Name,
		GrafanaURL:    req.GrafanaURL,
		Username:      req.Username,
		Password:      req.Password,
		DatasourceUID: req.DatasourceUID,
		AutoRules:     string(rulesJSON),
		WebhookURL:    req.WebhookURL,
		WebhookSecret: req.WebhookSecret,
		IntervalSec:   req.IntervalSec,
		Enabled:       true,
	}
	id, err := s.store.CreateGrafanaConfig(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.grafanaMgr.Start(id); err != nil {
		log.Printf("start grafana monitor %d: %v", id, err)
	}
	s.audit(r, "create", "grafana", id, "创建Grafana配置 "+req.Name)
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) apiGrafanaUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetGrafanaConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Name          string   `json:"name"`
		GrafanaURL    string   `json:"grafana_url"`
		Username      string   `json:"username"`
		Password      string   `json:"password"`
		DatasourceUID string   `json:"datasource_uid"`
		AutoRules     []string `json:"auto_rules"`
		WebhookURL    string   `json:"webhook_url"`
		WebhookSecret string   `json:"webhook_secret"`
		IntervalSec   int      `json:"interval_sec"`
		Enabled       *bool    `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.GrafanaURL != "" {
		existing.GrafanaURL = req.GrafanaURL
	}
	existing.Username = req.Username
	if req.Password != "" {
		existing.Password = req.Password
	}
	if req.DatasourceUID != "" {
		existing.DatasourceUID = req.DatasourceUID
	}
	if req.AutoRules != nil {
		rulesJSON, _ := json.Marshal(req.AutoRules)
		existing.AutoRules = string(rulesJSON)
	}
	if req.WebhookURL != "" {
		existing.WebhookURL = req.WebhookURL
	}
	if req.WebhookSecret != "" {
		existing.WebhookSecret = req.WebhookSecret
	}
	if req.IntervalSec > 0 {
		existing.IntervalSec = req.IntervalSec
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := s.store.UpdateGrafanaConfig(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.grafanaMgr.Stop(id)
	if existing.Enabled {
		if err := s.grafanaMgr.Start(id); err != nil {
			log.Printf("restart grafana monitor %d: %v", id, err)
		}
	}
	s.audit(r, "update", "grafana", id, "更新Grafana配置 "+existing.Name)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiGrafanaDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.grafanaMgr.Stop(id)
	s.grafanaMgr.CleanupGrafanaResources(id)
	if err := s.store.DeleteGrafanaConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "grafana", id, "删除Grafana配置")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiGrafanaToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleGrafana(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := s.store.GetGrafanaConfig(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg.Enabled {
		s.grafanaMgr.Start(id)
	} else {
		s.grafanaMgr.Stop(id)
	}
	action := "启用"
	if !cfg.Enabled {
		action = "禁用"
	}
	s.audit(r, "toggle", "grafana", id, action+"Grafana配置 "+cfg.Name)
	jsonOK(w, map[string]bool{"enabled": cfg.Enabled})
}

func (s *Server) apiGrafanaTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetGrafanaConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if err := monitor.TestGrafanaConnection(cfg); err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true, "message": "Grafana 连接成功"})
}

func (s *Server) apiGrafanaProvision(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.grafanaMgr.ProvisionForConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "provision", "grafana", id, "同步Grafana告警规则")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiGrafanaCleanupRules(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	deleted, err := s.grafanaMgr.CleanupAlertRules(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "cleanup", "grafana", id, fmt.Sprintf("清理Grafana告警规则 %d 条", deleted))
	jsonOK(w, map[string]interface{}{"deleted": deleted})
}

func (s *Server) apiGrafanaAlerts(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}
	var configID *int64
	if cid := r.URL.Query().Get("config_id"); cid != "" {
		if v, err := strconv.ParseInt(cid, 10, 64); err == nil && v > 0 {
			configID = &v
		}
	}

	logs, total, err := s.store.ListGrafanaAlertLogs(configID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.GrafanaAlertLog{}
	}
	jsonOK(w, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

func (s *Server) apiGrafanaRuleDefs(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, monitor.DefaultAlertRules)
}

func (s *Server) apiGrafanaDatasources(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GrafanaURL string `json:"grafana_url"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.GrafanaURL == "" || req.Username == "" {
		jsonError(w, http.StatusBadRequest, "grafana_url 和 username 不能为空")
		return
	}
	datasources, err := monitor.ListGrafanaDatasources(req.GrafanaURL, req.Username, req.Password)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, datasources)
}

func (s *Server) apiGrafanaGenerateSecret(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 16)
	rand.Read(b)
	secret := hex.EncodeToString(b)
	jsonOK(w, map[string]string{"secret": secret})
}

func (s *Server) apiGrafanaConfigDatasources(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetGrafanaConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "配置不存在")
		return
	}
	datasources, err := monitor.ListGrafanaDatasources(cfg.GrafanaURL, cfg.Username, cfg.Password)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, datasources)
}

func (s *Server) apiGrafanaWebhook(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if secret == "" {
		jsonError(w, http.StatusUnauthorized, "missing secret")
		return
	}
	// Validate secret against any grafana config
	configs, err := s.store.ListGrafanaConfigs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	valid := false
	for _, c := range configs {
		if c.WebhookSecret != "" && c.WebhookSecret == secret {
			valid = true
			break
		}
	}
	if !valid {
		jsonError(w, http.StatusForbidden, "invalid secret")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "read body failed")
		return
	}
	defer r.Body.Close()
	if err := s.grafanaMgr.HandleWebhook(body); err != nil {
		log.Printf("grafana webhook error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Ignored SQL Patterns ---

func (s *Server) apiIgnoredSQLList(w http.ResponseWriter, r *http.Request) {
	var dbID *int64
	if v := r.URL.Query().Get("database_id"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		if id > 0 {
			dbID = &id
		}
	}
	list, err := s.store.ListIgnoredSQL(dbID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.IgnoredSQLPattern{}
	}
	dbs, _ := s.store.ListDatabases()
	nameMap := make(map[int64]string)
	for _, d := range dbs {
		nameMap[d.ID] = d.Name
	}
	type item struct {
		store.IgnoredSQLPattern
		DatabaseName string `json:"database_name"`
	}
	items := make([]item, len(list))
	for i, p := range list {
		items[i] = item{IgnoredSQLPattern: p, DatabaseName: nameMap[p.DatabaseID]}
	}
	jsonOK(w, items)
}

func (s *Server) apiIgnoredSQLCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DatabaseID int64  `json:"database_id"`
		SQLText    string `json:"sql_text"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.DatabaseID <= 0 || req.SQLText == "" {
		jsonError(w, http.StatusBadRequest, "database_id and sql_text required")
		return
	}
	fp := store.NormalizeSQL(req.SQLText)
	sample := req.SQLText
	if len(sample) > 500 {
		sample = sample[:500]
	}
	id, err := s.store.AddIgnoredSQL(req.DatabaseID, fp, sample)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"id": id, "fingerprint": fp})
}

func (s *Server) apiIgnoredSQLDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveIgnoredSQL(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}
