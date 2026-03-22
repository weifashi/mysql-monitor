package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mysql-monitor/internal/auth"
	"mysql-monitor/internal/monitor"
	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
	"mysql-monitor/internal/web"
)

func main() {
	adminUser := getEnv("ADMIN_USER", "admin")
	adminPassword := getEnv("ADMIN_PASSWORD", "")

	githubClientID := getEnv("GITHUB_CLIENT_ID", "")
	githubClientSecret := getEnv("GITHUB_CLIENT_SECRET", "")

	dataDir := getEnv("DATA_DIR", "./data")
	listenAddr := getEnv("LISTEN_ADDR", ":8080")

	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Initialize encryption for database passwords
	if adminPassword != "" {
		store.InitEncryption(adminPassword)
	} else {
		store.InitEncryption("default-encryption-key")
	}

	// Open SQLite
	s, err := store.New(dataDir)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer s.Close()

	// Initialize default settings
	s.InitDefaultSettings()

	// If GitHub config provided via env, store in DB
	if githubClientID != "" {
		s.SetSetting("github_client_id", githubClientID)
		s.SetSetting("github_client_secret", githubClientSecret)
		s.SetSetting("github_enabled", "1")
	}

	// Determine if password login is required
	ghEnabled := s.GetSetting("github_enabled")
	pwEnabled := s.GetSetting("password_login_enabled")

	if adminPassword == "" && (ghEnabled != "1" || pwEnabled == "1") {
		fmt.Println("ADMIN_PASSWORD environment variable is required (or enable GitHub-only login)")
		os.Exit(1)
	}
	if adminPassword == "" {
		adminPassword = "disabled"
	}

	// Start purge loop for old slow query logs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartPurgeLoop(ctx)

	// Create components
	authStore := auth.NewSessionStore(adminUser, adminPassword, s)

	// Load GitHub config from DB settings
	if cid := s.GetSetting("github_client_id"); cid != "" {
		csec := s.GetSetting("github_client_secret")
		authStore.GitHub = auth.GitHubConfig{
			ClientID:     cid,
			ClientSecret: csec,
		}
	}

	dispatcher := notify.NewDispatcher(s)
	eventBus := monitor.NewEventBus()
	mgr := monitor.NewManager(s, dispatcher, eventBus)

	// Start session cleanup loop
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				authStore.CleanupExpired()
			}
		}
	}()

	// Start all enabled monitors
	if err := mgr.StartAll(); err != nil {
		log.Printf("start monitors: %v", err)
	}

	// RocketMQ monitors
	rocketMQMgr := monitor.NewRocketMQManager(s, dispatcher, eventBus)
	if err := rocketMQMgr.StartAll(); err != nil {
		log.Printf("start rocketmq monitors: %v", err)
	}

	// Health check monitors
	healthCheckMgr := monitor.NewHealthCheckManager(s, dispatcher, eventBus)
	if err := healthCheckMgr.StartAll(); err != nil {
		log.Printf("start health check monitors: %v", err)
	}

	// Grafana monitors
	grafanaMgr := monitor.NewGrafanaManager(s, dispatcher, eventBus)
	if err := grafanaMgr.StartAll(); err != nil {
		log.Printf("start grafana monitors: %v", err)
	}

	// Web server
	srv := web.NewServer(s, authStore, mgr, rocketMQMgr, healthCheckMgr, grafanaMgr, dispatcher, eventBus)
	httpSrv := &http.Server{
		Addr:    listenAddr,
		Handler: srv.Routes(),
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("shutting down...")
		mgr.StopAll()
		rocketMQMgr.StopAll()
		healthCheckMgr.StopAll()
		grafanaMgr.StopAll()
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("MySQL Monitor started on %s (user: %s)", listenAddr, adminUser)
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
