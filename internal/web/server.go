package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"ops-sentinel/internal/auth"
	"ops-sentinel/internal/monitor"
	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

//go:embed static
var staticFS embed.FS

const defaultAppVersion = "20260621162125"

type Server struct {
	store           *store.Store
	auth            *auth.SessionStore
	manager         *monitor.Manager
	rocketMQMgr     *monitor.RocketMQManager
	healthCheckMgr  *monitor.HealthCheckManager
	grafanaMgr      *monitor.GrafanaManager
	customSQLMgr    *monitor.CustomSQLManager
	cloudLoggingMgr *monitor.CloudLoggingManager
	dispatcher      *notify.Dispatcher
	hub             *Hub
	staticHandler   http.Handler
	appVersion      string
	// publicBaseURL 若设置（如 https://app.example.com），OAuth redirect_uri 固定由此拼接，避免反向代理下 r.Host 与公网不一致。
	publicBaseURL string
}

func NewServer(s *store.Store, a *auth.SessionStore, m *monitor.Manager, rmq *monitor.RocketMQManager, hc *monitor.HealthCheckManager, gm *monitor.GrafanaManager, csql *monitor.CustomSQLManager, cl *monitor.CloudLoggingManager, d *notify.Dispatcher, eb *monitor.EventBus, publicBaseURL string) *Server {
	hub := NewHub(eb)
	go hub.Run()
	staticSub, _ := fs.Sub(staticFS, "static")
	appVersion := strings.TrimSpace(os.Getenv("APP_VERSION"))
	if appVersion == "" {
		appVersion = defaultAppVersion
	}
	return &Server{
		store: s, auth: a, manager: m, rocketMQMgr: rmq, healthCheckMgr: hc, grafanaMgr: gm, customSQLMgr: csql, cloudLoggingMgr: cl, dispatcher: d, hub: hub,
		staticHandler: http.StripPrefix("/", noCacheStaticAssets(http.FileServer(http.FS(staticSub)))),
		appVersion:    appVersion,
		publicBaseURL: strings.TrimSpace(strings.TrimSuffix(publicBaseURL, "/")),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", noCacheStaticAssets(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))))

	// --- Public API routes (no auth) ---
	mux.HandleFunc("POST /api/auth/login", s.apiLogin)
	mux.HandleFunc("GET /api/auth/config", s.apiAuthConfig)
	mux.HandleFunc("GET /api/auth/github", s.apiGitHubLogin)
	mux.HandleFunc("GET /api/auth/github/callback", s.apiGitHubCallback)
	mux.HandleFunc("GET /api/auth/me", s.apiMe)

	// Grafana webhook (public, called by Grafana)
	mux.HandleFunc("POST /api/grafana-webhook", s.apiGrafanaWebhook)

	// --- WebSocket routes (auth checked inline) ---
	mux.HandleFunc("GET /ws/slow-queries", s.wsHandler("slow-queries"))
	mux.HandleFunc("GET /ws/monitor-logs", s.wsHandler("monitor-logs"))
	mux.HandleFunc("GET /ws/rocketmq-logs", s.wsHandler("rocketmq-logs"))
	mux.HandleFunc("GET /ws/healthcheck-logs", s.wsHandler("healthcheck-logs"))
	mux.HandleFunc("GET /ws/grafana-logs", s.wsHandler("grafana-logs"))
	mux.HandleFunc("GET /ws/custom-sql-logs", s.wsHandler("custom-sql-logs"))
	mux.HandleFunc("GET /ws/cloud-logging-logs", s.wsHandler("cloud-logging-logs"))

	// --- Protected API routes ---
	api := http.NewServeMux()
	api.HandleFunc("POST /api/auth/logout", s.apiLogout)

	// Dashboard
	api.HandleFunc("GET /api/dashboard/stats", s.apiDashboardStats)

	// Databases
	api.HandleFunc("GET /api/databases", s.apiDatabasesList)
	api.HandleFunc("POST /api/databases", s.apiDatabaseCreate)
	api.HandleFunc("PUT /api/databases/{id}", s.apiDatabaseUpdate)
	api.HandleFunc("DELETE /api/databases/{id}", s.apiDatabaseDelete)
	api.HandleFunc("POST /api/databases/{id}/toggle", s.apiDatabaseToggle)
	api.HandleFunc("POST /api/databases/{id}/test", s.apiDatabaseTest)

	// Notifications
	api.HandleFunc("GET /api/notifications", s.apiNotificationsList)
	api.HandleFunc("POST /api/notifications", s.apiNotificationCreate)
	api.HandleFunc("PUT /api/notifications/{id}", s.apiNotificationUpdate)
	api.HandleFunc("DELETE /api/notifications/{id}", s.apiNotificationDelete)
	api.HandleFunc("POST /api/notifications/{id}/test", s.apiNotificationTest)
	api.HandleFunc("GET /api/notification-scopes", s.apiNotificationScopes)

	// Slow queries
	api.HandleFunc("GET /api/slow-queries", s.apiSlowQueries)
	api.HandleFunc("DELETE /api/slow-queries", s.apiSlowQueriesClear)

	// Ignored SQL patterns
	api.HandleFunc("GET /api/ignored-sql", s.apiIgnoredSQLList)
	api.HandleFunc("POST /api/ignored-sql", s.apiIgnoredSQLCreate)
	api.HandleFunc("DELETE /api/ignored-sql/{id}", s.apiIgnoredSQLDelete)

	// Custom SQL checks
	api.HandleFunc("GET /api/custom-sql", s.apiCustomSQLList)
	api.HandleFunc("POST /api/custom-sql", s.apiCustomSQLCreate)
	api.HandleFunc("PUT /api/custom-sql/{id}", s.apiCustomSQLUpdate)
	api.HandleFunc("DELETE /api/custom-sql/{id}", s.apiCustomSQLDelete)
	api.HandleFunc("POST /api/custom-sql/{id}/toggle", s.apiCustomSQLToggle)
	api.HandleFunc("POST /api/custom-sql/{id}/test", s.apiCustomSQLTest)
	api.HandleFunc("GET /api/custom-sql/logs", s.apiCustomSQLLogs)
	api.HandleFunc("DELETE /api/custom-sql/logs", s.apiCustomSQLLogsClear)

	// Cloud Logging
	api.HandleFunc("GET /api/cloud-logging/configs", s.apiCloudLoggingConfigList)
	api.HandleFunc("POST /api/cloud-logging/configs", s.apiCloudLoggingConfigCreate)
	api.HandleFunc("PUT /api/cloud-logging/configs/{id}", s.apiCloudLoggingConfigUpdate)
	api.HandleFunc("DELETE /api/cloud-logging/configs/{id}", s.apiCloudLoggingConfigDelete)
	api.HandleFunc("POST /api/cloud-logging/configs/{id}/toggle", s.apiCloudLoggingConfigToggle)
	api.HandleFunc("POST /api/cloud-logging/configs/{id}/test", s.apiCloudLoggingConfigTest)
	api.HandleFunc("GET /api/cloud-logging/checks", s.apiCloudLoggingCheckList)
	api.HandleFunc("POST /api/cloud-logging/checks", s.apiCloudLoggingCheckCreate)
	api.HandleFunc("PUT /api/cloud-logging/checks/{id}", s.apiCloudLoggingCheckUpdate)
	api.HandleFunc("DELETE /api/cloud-logging/checks/{id}", s.apiCloudLoggingCheckDelete)
	api.HandleFunc("POST /api/cloud-logging/checks/{id}/toggle", s.apiCloudLoggingCheckToggle)
	api.HandleFunc("POST /api/cloud-logging/checks/{id}/test", s.apiCloudLoggingCheckTest)
	api.HandleFunc("POST /api/cloud-logging/query", s.apiCloudLoggingQuery)
	api.HandleFunc("GET /api/cloud-logging/logs", s.apiCloudLoggingLogs)

	// Users
	api.HandleFunc("GET /api/users", s.apiUsersList)
	api.HandleFunc("POST /api/users", s.apiUserCreate)
	api.HandleFunc("DELETE /api/users/{id}", s.apiUserDelete)

	// Settings
	api.HandleFunc("GET /api/settings", s.apiSettingsGet)
	api.HandleFunc("PUT /api/settings", s.apiSettingsUpdate)

	// Simple databases list for selectors
	api.HandleFunc("GET /api/databases-simple", s.apiDatabasesSimpleList)

	// RocketMQ
	api.HandleFunc("GET /api/rocketmq", s.apiRocketMQList)
	api.HandleFunc("POST /api/rocketmq", s.apiRocketMQCreate)
	api.HandleFunc("PUT /api/rocketmq/{id}", s.apiRocketMQUpdate)
	api.HandleFunc("DELETE /api/rocketmq/{id}", s.apiRocketMQDelete)
	api.HandleFunc("POST /api/rocketmq/{id}/toggle", s.apiRocketMQToggle)
	api.HandleFunc("POST /api/rocketmq/{id}/test", s.apiRocketMQTest)
	api.HandleFunc("POST /api/rocketmq/consumer-groups", s.apiRocketMQConsumerGroups)
	api.HandleFunc("POST /api/rocketmq/topics", s.apiRocketMQTopics)
	api.HandleFunc("GET /api/rocketmq/alerts", s.apiRocketMQAlerts)

	// Audit Logs
	api.HandleFunc("GET /api/audit-logs", s.apiAuditLogs)

	// Health Checks
	api.HandleFunc("GET /api/health-checks", s.apiHealthCheckList)
	api.HandleFunc("POST /api/health-checks", s.apiHealthCheckCreate)
	api.HandleFunc("PUT /api/health-checks/{id}", s.apiHealthCheckUpdate)
	api.HandleFunc("DELETE /api/health-checks/{id}", s.apiHealthCheckDelete)
	api.HandleFunc("POST /api/health-checks/{id}/toggle", s.apiHealthCheckToggle)
	api.HandleFunc("POST /api/health-checks/{id}/test", s.apiHealthCheckTest)
	api.HandleFunc("GET /api/health-checks/logs", s.apiHealthCheckLogs)

	// Grafana
	api.HandleFunc("GET /api/grafana", s.apiGrafanaList)
	api.HandleFunc("POST /api/grafana", s.apiGrafanaCreate)
	api.HandleFunc("PUT /api/grafana/{id}", s.apiGrafanaUpdate)
	api.HandleFunc("DELETE /api/grafana/{id}", s.apiGrafanaDelete)
	api.HandleFunc("POST /api/grafana/{id}/toggle", s.apiGrafanaToggle)
	api.HandleFunc("POST /api/grafana/{id}/test", s.apiGrafanaTest)
	api.HandleFunc("POST /api/grafana/{id}/provision", s.apiGrafanaProvision)
	api.HandleFunc("POST /api/grafana/{id}/cleanup-rules", s.apiGrafanaCleanupRules)
	api.HandleFunc("GET /api/grafana/alerts", s.apiGrafanaAlerts)
	api.HandleFunc("GET /api/grafana/rule-defs", s.apiGrafanaRuleDefs)
	api.HandleFunc("POST /api/grafana/datasources", s.apiGrafanaDatasources)
	api.HandleFunc("POST /api/grafana/generate-secret", s.apiGrafanaGenerateSecret)
	api.HandleFunc("GET /api/grafana/{id}/datasources", s.apiGrafanaConfigDatasources)

	mux.Handle("/api/", s.auth.AuthMiddleware(api))

	// SPA fallback — serve index.html for all non-API, non-static routes
	mux.HandleFunc("/", s.spaFallback)

	return securityHeaders(mux)
}

func (s *Server) spaFallback(w http.ResponseWriter, r *http.Request) {
	// If path has a file extension, try to serve from static
	if strings.Contains(r.URL.Path, ".") {
		s.staticHandler.ServeHTTP(w, r)
		return
	}

	// Serve index.html for SPA routing
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	html := strings.ReplaceAll(string(data), "{{APP_VERSION}}", s.appVersion)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (s *Server) wsHandler(room string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := auth.GetSessionToken(r)
		if _, ok := s.auth.Validate(token); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		s.hub.ServeWs(w, r, room)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func noCacheStaticAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.ToLower(r.URL.Path)
		if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}
