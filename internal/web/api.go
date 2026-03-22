package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	"mysql-monitor/internal/auth"
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
	jsonOK(w, map[string]any{
		"username": req.Username,
		"role":     "admin",
	})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) apiGitHubLogin(w http.ResponseWriter, r *http.Request) {
	clientID := s.store.GetSetting("github_client_id")
	if clientID == "" {
		clientID = s.auth.GitHub.ClientID
	}
	if clientID == "" {
		jsonError(w, http.StatusBadRequest, "GitHub OAuth 未配置")
		return
	}

	// Build callback URL from request
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	redirectURI := fmt.Sprintf("%s://%s/api/auth/github/callback", scheme, r.Host)

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
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// --- Dashboard ---

func (s *Server) apiDashboardStats(w http.ResponseWriter, r *http.Request) {
	var (
		totalDBs, enabledDBs, todayCount, weekCount int
		recentLogs                                   []store.SlowQueryLog
		wg                                           sync.WaitGroup
	)
	wg.Add(5)
	go func() { defer wg.Done(); totalDBs, _ = s.store.CountDatabases() }()
	go func() { defer wg.Done(); enabledDBs, _ = s.store.CountEnabledDatabases() }()
	go func() { defer wg.Done(); todayCount, _ = s.store.CountSlowQueriesToday() }()
	go func() { defer wg.Done(); weekCount, _ = s.store.CountSlowQueriesWeek() }()
	go func() { defer wg.Done(); recentLogs, _, _ = s.store.ListSlowQueryLogs(nil, 1, 10) }()
	wg.Wait()

	jsonOK(w, map[string]any{
		"total_dbs":   totalDBs,
		"enabled_dbs": enabledDBs,
		"running_dbs": s.manager.RunningCount(),
		"today_count": todayCount,
		"week_count":  weekCount,
		"recent_logs": recentLogs,
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
	dbs, _ := s.store.ListDatabases()
	dbMap := make(map[int64]string)
	for _, db := range dbs {
		dbMap[db.ID] = db.Name
	}

	type ncDisplay struct {
		store.NotificationConfig
		DatabaseName  string `json:"database_name"`
		ConfigSummary string `json:"config_summary"`
	}
	var list []ncDisplay
	for _, nc := range configs {
		d := ncDisplay{NotificationConfig: nc}
		if nc.DatabaseID != nil {
			d.DatabaseName = dbMap[*nc.DatabaseID]
		} else {
			d.DatabaseName = "全局"
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
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	nc := &store.NotificationConfig{
		Type:    req.Type,
		Enabled: true,
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
	default:
		return nil, fmt.Errorf("unknown notification type: %s", nc.Type)
	}
	if err != nil {
		return nil, err
	}
	nc.ConfigJSON = configJSON
	return nc, nil
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
		"github_client_id":       true,
		"github_client_secret":   true,
		"github_enabled":         true,
		"password_login_enabled": true,
	}
	for k, v := range req {
		if !allowed[k] {
			continue
		}
		if err := s.store.SetSetting(k, v); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
