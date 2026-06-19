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

func mustNotificationConfigJSON(t *testing.T, cfg FeishuConfig) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return data
}
