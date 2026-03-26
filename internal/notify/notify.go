package notify

import (
	"encoding/json"
	"fmt"
	"log"

	"mysql-monitor/internal/store"
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
		return nil
	}
	return d.dispatchToConfigs(configs, message)
}

// SendGlobalNotifications sends message using global notification configs (scope_type='all').
func (d *Dispatcher) SendGlobalNotifications(message string) error {
	configs, err := d.store.GetGlobalNotifications()
	if err != nil {
		return fmt.Errorf("load global notification configs: %w", err)
	}
	if len(configs) == 0 {
		return nil
	}
	return d.dispatchToConfigs(configs, message)
}

// SendScopedNotifications sends message using scope-specific + global notification configs.
func (d *Dispatcher) SendScopedNotifications(scopeType string, scopeID int64, message string) error {
	configs, err := d.store.GetScopedNotifications(scopeType, scopeID)
	if err != nil {
		return fmt.Errorf("load scoped notification configs: %w", err)
	}
	if len(configs) == 0 {
		return nil
	}
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
				continue
			}
			if err := SendFeishu(c, message); err != nil {
				log.Printf("feishu send failed: %v", err)
				lastErr = err
			}
		case "email":
			var c store.EmailConfig
			if err := json.Unmarshal(cfg.ConfigJSON, &c); err != nil {
				log.Printf("parse email config: %v", err)
				continue
			}
			if c.From == "" || c.To == "" {
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
				continue
			}
			if err := SendDooTask(c, message); err != nil {
				log.Printf("dootask send failed: %v", err)
				lastErr = err
			}
		}
	}
	return lastErr
}

// SendTestNotification sends a test message to a specific notification config.
func SendTestNotification(nc *store.NotificationConfig) error {
	message := "MySQL Monitor 测试通知\n\n这是一条测试消息，说明通知配置正确。"

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
