package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"mysql-monitor/internal/auth"
	"mysql-monitor/internal/monitor"
	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	store      *store.Store
	auth       *auth.SessionStore
	manager    *monitor.Manager
	dispatcher *notify.Dispatcher
	hub        *Hub
	staticHandler http.Handler
}

func NewServer(s *store.Store, a *auth.SessionStore, m *monitor.Manager, d *notify.Dispatcher, eb *monitor.EventBus) *Server {
	hub := NewHub(eb)
	go hub.Run()
	staticSub, _ := fs.Sub(staticFS, "static")
	return &Server{
		store: s, auth: a, manager: m, dispatcher: d, hub: hub,
		staticHandler: http.StripPrefix("/", http.FileServer(http.FS(staticSub))),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// --- Public API routes (no auth) ---
	mux.HandleFunc("POST /api/auth/login", s.apiLogin)
	mux.HandleFunc("GET /api/auth/config", s.apiAuthConfig)
	mux.HandleFunc("GET /api/auth/github", s.apiGitHubLogin)
	mux.HandleFunc("GET /api/auth/github/callback", s.apiGitHubCallback)
	mux.HandleFunc("GET /api/auth/me", s.apiMe)

	// --- WebSocket routes (auth checked inline) ---
	mux.HandleFunc("GET /ws/slow-queries", s.wsHandler("slow-queries"))
	mux.HandleFunc("GET /ws/monitor-logs", s.wsHandler("monitor-logs"))

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

	// Slow queries
	api.HandleFunc("GET /api/slow-queries", s.apiSlowQueries)

	// Users
	api.HandleFunc("GET /api/users", s.apiUsersList)
	api.HandleFunc("POST /api/users", s.apiUserCreate)
	api.HandleFunc("DELETE /api/users/{id}", s.apiUserDelete)

	// Settings
	api.HandleFunc("GET /api/settings", s.apiSettingsGet)
	api.HandleFunc("PUT /api/settings", s.apiSettingsUpdate)

	// Simple databases list for selectors
	api.HandleFunc("GET /api/databases-simple", s.apiDatabasesSimpleList)

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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
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
