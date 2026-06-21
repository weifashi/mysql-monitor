package store

import (
	"encoding/json"
	"testing"
)

func TestGetScopedNotificationsReturnsAllEnabledDestinations(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer s.Close()

	globalOne := mustNotificationConfigJSON(t, FeishuConfig{Webhook: "https://open.larksuite.com/open-apis/bot/v2/hook/one"})
	globalTwo := mustNotificationConfigJSON(t, FeishuConfig{Webhook: "https://open.larksuite.com/open-apis/bot/v2/hook/two"})
	disabled := mustNotificationConfigJSON(t, FeishuConfig{Webhook: "https://open.larksuite.com/open-apis/bot/v2/hook/disabled"})

	if _, err := s.CreateNotificationConfig(&NotificationConfig{ScopeType: "all", Type: "feishu", ConfigJSON: globalOne, Enabled: true}); err != nil {
		t.Fatalf("create first global config: %v", err)
	}
	if _, err := s.CreateNotificationConfig(&NotificationConfig{ScopeType: "all", Type: "feishu", ConfigJSON: globalTwo, Enabled: true}); err != nil {
		t.Fatalf("create second global config: %v", err)
	}
	if _, err := s.CreateNotificationConfig(&NotificationConfig{ScopeType: "all", Type: "feishu", ConfigJSON: disabled, Enabled: false}); err != nil {
		t.Fatalf("create disabled global config: %v", err)
	}

	configs, err := s.GetScopedNotifications("health", 1)
	if err != nil {
		t.Fatalf("get scoped notifications: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected both enabled global destinations, got %d", len(configs))
	}
	if configs[0].ID >= configs[1].ID {
		t.Fatalf("expected deterministic id order, got ids %d then %d", configs[0].ID, configs[1].ID)
	}
}

func TestLastHealthCheckLogStatus(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer s.Close()

	status, ok, err := s.LastHealthCheckLogStatus(42)
	if err != nil {
		t.Fatalf("last missing health status: %v", err)
	}
	if ok || status != "" {
		t.Fatalf("expected missing status, got ok=%v status=%q", ok, status)
	}

	s.InsertHealthCheckLog(&HealthCheckLog{CheckID: 42, CheckName: "web1", Status: "down", HTTPStatus: 502})

	status, ok, err = s.LastHealthCheckLogStatus(42)
	if err != nil {
		t.Fatalf("last health status: %v", err)
	}
	if !ok || status != "down" {
		t.Fatalf("expected down status, got ok=%v status=%q", ok, status)
	}
}

func TestLastCustomSQLLogStatus(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer s.Close()

	status, ok, err := s.LastCustomSQLLogStatus(7)
	if err != nil {
		t.Fatalf("last missing custom sql status: %v", err)
	}
	if ok || status != "" {
		t.Fatalf("expected missing status, got ok=%v status=%q", ok, status)
	}

	dbID, err := s.CreateDatabase(&Database{Name: "ttpos", Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret", Enabled: true})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	checkID, err := s.CreateCustomSQLCheck(&CustomSQLCheck{
		DatabaseID:     dbID,
		Name:           "locks",
		SQLText:        "SELECT 1 AS value",
		ResultField:    "value",
		NotifyEnabled:  true,
		RecoveryNotify: true,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create custom sql check: %v", err)
	}

	if _, err := s.InsertCustomSQLLog(&CustomSQLLog{CheckID: checkID, CheckName: "locks", Status: "alert"}); err != nil {
		t.Fatalf("insert custom sql log: %v", err)
	}

	status, ok, err = s.LastCustomSQLLogStatus(checkID)
	if err != nil {
		t.Fatalf("last custom sql status: %v", err)
	}
	if !ok || status != "alert" {
		t.Fatalf("expected alert status, got ok=%v status=%q", ok, status)
	}
}

func mustNotificationConfigJSON(t *testing.T, cfg FeishuConfig) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return data
}
