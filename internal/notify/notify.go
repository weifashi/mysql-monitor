package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"ops-sentinel/internal/store"
)

type LongQuery struct {
	ThreadID  uint64
	ProcessID uint64
	User      string
	Host      string
	DB        string
	SQLText   string
	ExecSec   float64
	LockSec   float64
	RowsExam  uint64
	RowsSent  uint64
	State     string
}

type Dispatcher struct {
	store *store.Store
}

func NewDispatcher(s *store.Store) *Dispatcher {
	return &Dispatcher{store: s}
}

func (d *Dispatcher) SendNotifications(databaseID int64, message string) error {
	configs, err := d.store.GetEffectiveNotifications(databaseID)
	if err != nil {
		return fmt.Errorf("load notification configs: %w", err)
	}
	if len(configs) == 0 {
		log.Printf("notification skipped: no enabled configs for database_id=%d", databaseID)
		return nil
	}
	log.Printf("notification dispatch: database_id=%d configs=%d", databaseID, len(configs))
	return d.dispatchToConfigs(configs, message)
}

// SendGlobalNotifications sends message using global notification configs (scope_type='all').
func (d *Dispatcher) SendGlobalNotifications(message string) error {
	configs, err := d.store.GetGlobalNotifications()
	if err != nil {
		return fmt.Errorf("load global notification configs: %w", err)
	}
	if len(configs) == 0 {
		log.Printf("global notification skipped: no enabled global configs")
		return nil
	}
	log.Printf("global notification dispatch: configs=%d", len(configs))
	return d.dispatchToConfigs(configs, message)
}

// SendScopedNotifications sends message using scope-specific + global notification configs.
func (d *Dispatcher) SendScopedNotifications(scopeType string, scopeID int64, message string) error {
	configs, err := d.store.GetScopedNotifications(scopeType, scopeID)
	if err != nil {
		return fmt.Errorf("load scoped notification configs: %w", err)
	}
	if len(configs) == 0 {
		log.Printf("notification skipped: no enabled configs for scope=%s scope_id=%d", scopeType, scopeID)
		return nil
	}
	log.Printf("notification dispatch: scope=%s scope_id=%d configs=%d", scopeType, scopeID, len(configs))
	return d.dispatchToConfigs(configs, message)
}

func (d *Dispatcher) dispatchToConfigs(configs []store.NotificationConfig, message string) error {
	var lastErr error
	for _, cfg := range configs {
		switch cfg.Type {
		case "dingtalk":
			var c store.DingTalkConfig
			if err := json.Unmarshal(cfg.ConfigJSON, &c); err != nil {
				log.Printf("parse dingtalk config: %v", err)
				continue
			}
			if c.Webhook == "" {
				log.Printf("dingtalk config id=%d skipped: empty webhook", cfg.ID)
				continue
			}
			if err := SendDingTalk(c, message); err != nil {
				log.Printf("dingtalk send failed: %v", err)
				lastErr = err
			}
		case "feishu":
			var c store.FeishuConfig
			if err := json.Unmarshal(cfg.ConfigJSON, &c); err != nil {
				log.Printf("parse feishu config: %v", err)
				continue
			}
			if c.Webhook == "" {
				log.Printf("feishu config id=%d skipped: empty webhook", cfg.ID)
				continue
			}
			target := notificationTarget(c.Webhook)
			log.Printf("sending feishu notification config_id=%d scope=%s database_id=%v target=%s", cfg.ID, cfg.ScopeType, cfg.DatabaseID, target)
			if err := SendFeishu(c, message); err != nil {
				wrappedErr := fmt.Errorf("feishu target=%s send failed: %w", target, err)
				log.Printf("%v", wrappedErr)
				lastErr = wrappedErr
			} else {
				log.Printf("feishu notification sent config_id=%d", cfg.ID)
			}
		case "email":
			var c store.EmailConfig
			if err := json.Unmarshal(cfg.ConfigJSON, &c); err != nil {
				log.Printf("parse email config: %v", err)
				continue
			}
			if c.From == "" || c.To == "" {
				log.Printf("email config id=%d skipped: empty from/to", cfg.ID)
				continue
			}
			if err := SendEmail(c, message); err != nil {
				log.Printf("email send failed: %v", err)
				lastErr = err
			}
		case "dootask":
			var c store.DooTaskConfig
			if err := json.Unmarshal(cfg.ConfigJSON, &c); err != nil {
				log.Printf("parse dootask config: %v", err)
				continue
			}
			if c.BaseURL == "" || c.Token == "" || c.DialogID == "" {
				log.Printf("dootask config id=%d skipped: incomplete config", cfg.ID)
				continue
			}
			if err := SendDooTask(c, message); err != nil {
				log.Printf("dootask send failed: %v", err)
				lastErr = err
			}
		default:
			log.Printf("notification config id=%d skipped: unknown type=%s", cfg.ID, cfg.Type)
		}
	}
	return lastErr
}

func notificationTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return maskSecretTail(raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		parts[len(parts)-1] = maskSecretTail(parts[len(parts)-1])
		u.Path = "/" + strings.Join(parts, "/")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func maskSecretTail(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:6] + "..." + s[len(s)-4:]
}

// SendTestNotification sends a test message to a specific notification config.
func SendTestNotification(nc *store.NotificationConfig) error {
	message := "Ops Sentinel 测试通知\n\n这是一条测试消息，说明通知配置正确。"

	switch nc.Type {
	case "dingtalk":
		var c store.DingTalkConfig
		if err := json.Unmarshal(nc.ConfigJSON, &c); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		return SendDingTalk(c, message)
	case "feishu":
		var c store.FeishuConfig
		if err := json.Unmarshal(nc.ConfigJSON, &c); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		return SendFeishu(c, message)
	case "email":
		var c store.EmailConfig
		if err := json.Unmarshal(nc.ConfigJSON, &c); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		return SendEmail(c, message)
	case "dootask":
		var c store.DooTaskConfig
		if err := json.Unmarshal(nc.ConfigJSON, &c); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		return SendDooTask(c, message)
	default:
		return fmt.Errorf("unknown type: %s", nc.Type)
	}
}
