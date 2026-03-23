package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"mysql-monitor/internal/auth"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Database struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	User         string    `json:"user"`
	Password     string    `json:"password"`
	IntervalSec  int       `json:"interval_sec"`
	ThresholdSec int       `json:"threshold_sec"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (d *Database) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/performance_schema", d.User, d.Password, d.Host, d.Port)
}

type NotificationConfig struct {
	ID         int64           `json:"id"`
	DatabaseID *int64          `json:"database_id"`
	Type       string          `json:"type"`
	ConfigJSON json.RawMessage `json:"config_json"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type DingTalkConfig struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
}

type FeishuConfig struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
}

type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	GitHubID    int64     `json:"github_id,omitempty"`
	GitHubLogin string    `json:"github_login,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type SlowQueryLog struct {
	ID           int64     `json:"id"`
	DatabaseID   int64     `json:"database_id"`
	DatabaseName string    `json:"database_name"`
	ThreadID     uint64    `json:"thread_id"`
	ProcessID    uint64    `json:"process_id"`
	User         string    `json:"user"`
	Host         string    `json:"host"`
	DBName       string    `json:"db_name"`
	SQLText      string    `json:"sql_text"`
	ExecSec      float64   `json:"exec_sec"`
	LockSec      float64   `json:"lock_sec"`
	RowsExamined uint64    `json:"rows_examined"`
	RowsSent     uint64    `json:"rows_sent"`
	State        string    `json:"state"`
	DetectedAt   time.Time `json:"detected_at"`
}

type Store struct {
	db *sql.DB
}

func New(dataDir string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s/monitor.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON", dataDir)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- Database CRUD ---

func (s *Store) ListDatabases() ([]Database, error) {
	rows, err := s.db.Query(`SELECT id, name, host, port, user, password, interval_sec, threshold_sec, enabled, created_at, updated_at FROM databases ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Database
	for rows.Next() {
		var d Database
		var enabled int
		var encPwd string
		if err := rows.Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.User, &encPwd, &d.IntervalSec, &d.ThresholdSec, &enabled, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Password, _ = Decrypt(encPwd)
		d.Enabled = enabled == 1
		list = append(list, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) GetDatabase(id int64) (*Database, error) {
	var d Database
	var enabled int
	var encPwd string
	err := s.db.QueryRow(`SELECT id, name, host, port, user, password, interval_sec, threshold_sec, enabled, created_at, updated_at FROM databases WHERE id=?`, id).
		Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.User, &encPwd, &d.IntervalSec, &d.ThresholdSec, &enabled, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	d.Password, _ = Decrypt(encPwd)
	d.Enabled = enabled == 1
	return &d, nil
}

func (s *Store) CreateDatabase(d *Database) (int64, error) {
	encPwd, err := Encrypt(d.Password)
	if err != nil {
		return 0, fmt.Errorf("encrypt password: %w", err)
	}
	res, err := s.db.Exec(`INSERT INTO databases (name, host, port, user, password, interval_sec, threshold_sec, enabled) VALUES (?,?,?,?,?,?,?,?)`,
		d.Name, d.Host, d.Port, d.User, encPwd, d.IntervalSec, d.ThresholdSec, boolToInt(d.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateDatabase(d *Database) error {
	encPwd, err := Encrypt(d.Password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	_, err = s.db.Exec(`UPDATE databases SET name=?, host=?, port=?, user=?, password=?, interval_sec=?, threshold_sec=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		d.Name, d.Host, d.Port, d.User, encPwd, d.IntervalSec, d.ThresholdSec, boolToInt(d.Enabled), d.ID)
	return err
}

func (s *Store) DeleteDatabase(id int64) error {
	_, err := s.db.Exec(`DELETE FROM databases WHERE id=?`, id)
	return err
}

func (s *Store) ToggleDatabase(id int64) error {
	_, err := s.db.Exec(`UPDATE databases SET enabled = 1 - enabled, updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *Store) CountDatabases() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM databases`).Scan(&count)
	return count, err
}

func (s *Store) CountEnabledDatabases() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM databases WHERE enabled=1`).Scan(&count)
	return count, err
}

// --- Notification Config CRUD ---

func (s *Store) ListNotificationConfigs() ([]NotificationConfig, error) {
	rows, err := s.db.Query(`SELECT id, database_id, type, config_json, enabled, created_at, updated_at FROM notification_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []NotificationConfig
	for rows.Next() {
		var nc NotificationConfig
		var enabled int
		var configStr string
		if err := rows.Scan(&nc.ID, &nc.DatabaseID, &nc.Type, &configStr, &enabled, &nc.CreatedAt, &nc.UpdatedAt); err != nil {
			return nil, err
		}
		nc.ConfigJSON = json.RawMessage(configStr)
		nc.Enabled = enabled == 1
		list = append(list, nc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) GetNotificationConfig(id int64) (*NotificationConfig, error) {
	var nc NotificationConfig
	var enabled int
	var configStr string
	err := s.db.QueryRow(`SELECT id, database_id, type, config_json, enabled, created_at, updated_at FROM notification_configs WHERE id=?`, id).
		Scan(&nc.ID, &nc.DatabaseID, &nc.Type, &configStr, &enabled, &nc.CreatedAt, &nc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	nc.ConfigJSON = json.RawMessage(configStr)
	nc.Enabled = enabled == 1
	return &nc, nil
}

func (s *Store) CreateNotificationConfig(nc *NotificationConfig) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO notification_configs (database_id, type, config_json, enabled) VALUES (?,?,?,?)`,
		nc.DatabaseID, nc.Type, string(nc.ConfigJSON), boolToInt(nc.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateNotificationConfig(nc *NotificationConfig) error {
	_, err := s.db.Exec(`UPDATE notification_configs SET database_id=?, type=?, config_json=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		nc.DatabaseID, nc.Type, string(nc.ConfigJSON), boolToInt(nc.Enabled), nc.ID)
	return err
}

func (s *Store) DeleteNotificationConfig(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notification_configs WHERE id=?`, id)
	return err
}

func (s *Store) GetEffectiveNotifications(databaseID int64) ([]NotificationConfig, error) {
	// Order by database_id DESC NULLS LAST so DB-specific configs come first.
	rows, err := s.db.Query(`SELECT id, database_id, type, config_json, enabled, created_at, updated_at FROM notification_configs WHERE enabled=1 AND (database_id=? OR database_id IS NULL) ORDER BY database_id DESC`, databaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seenType := make(map[string]bool)
	var list []NotificationConfig
	for rows.Next() {
		var nc NotificationConfig
		var enabled int
		var configStr string
		if err := rows.Scan(&nc.ID, &nc.DatabaseID, &nc.Type, &configStr, &enabled, &nc.CreatedAt, &nc.UpdatedAt); err != nil {
			return nil, err
		}
		nc.ConfigJSON = json.RawMessage(configStr)
		nc.Enabled = enabled == 1
		// Deduplicate by type: DB-specific config takes priority over global.
		if seenType[nc.Type] {
			continue
		}
		seenType[nc.Type] = true
		list = append(list, nc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// --- Slow Query Log ---

func (s *Store) InsertSlowQueryLog(l *SlowQueryLog) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM slow_query_logs WHERE database_id=? AND process_id=? AND detected_at > datetime('now', '-1 hour') LIMIT 1`, l.DatabaseID, l.ProcessID).Scan(&exists)
	if err == nil {
		return false, nil // duplicate, skip
	}
	_, err = s.db.Exec(`INSERT INTO slow_query_logs (database_id, thread_id, process_id, user, host, db_name, sql_text, exec_sec, lock_sec, rows_examined, rows_sent, state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.DatabaseID, l.ThreadID, l.ProcessID, l.User, l.Host, l.DBName, l.SQLText, l.ExecSec, l.LockSec, l.RowsExamined, l.RowsSent, l.State)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListSlowQueryLogs(databaseID *int64, page, pageSize int) ([]SlowQueryLog, int, error) {
	countQuery := `SELECT COUNT(*) FROM slow_query_logs`
	dataQuery := `SELECT l.id, l.database_id, COALESCE(d.name,''), l.thread_id, l.process_id, l.user, l.host, l.db_name, l.sql_text, l.exec_sec, l.lock_sec, l.rows_examined, l.rows_sent, l.state, l.detected_at FROM slow_query_logs l LEFT JOIN databases d ON l.database_id=d.id`
	var args []interface{}
	if databaseID != nil {
		countQuery += ` WHERE database_id=?`
		dataQuery += ` WHERE l.database_id=?`
		args = append(args, *databaseID)
	}

	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery += ` ORDER BY l.detected_at DESC LIMIT ? OFFSET ?`
	offset := (page - 1) * pageSize
	dataArgs := append(args, pageSize, offset)

	rows, err := s.db.Query(dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []SlowQueryLog
	for rows.Next() {
		var l SlowQueryLog
		if err := rows.Scan(&l.ID, &l.DatabaseID, &l.DatabaseName, &l.ThreadID, &l.ProcessID, &l.User, &l.Host, &l.DBName, &l.SQLText, &l.ExecSec, &l.LockSec, &l.RowsExamined, &l.RowsSent, &l.State, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *Store) PurgeOldLogs() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM slow_query_logs WHERE detected_at < datetime('now', '-30 days')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) StartPurgeLoop(ctx context.Context) {
	s.runPurge()
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runPurge()
			}
		}
	}()
}

func (s *Store) runPurge() {
	if n, err := s.PurgeOldLogs(); err == nil && n > 0 {
		log.Printf("purged %d old slow query logs", n)
	}
	if n, err := s.PurgeOldAuditLogs(); err == nil && n > 0 {
		log.Printf("purged %d old audit logs", n)
	}
	if n, err := s.PurgeOldHealthCheckLogs(); err == nil && n > 0 {
		log.Printf("purged %d old health check logs", n)
	}
	s.CleanupOldNotifiedPIDs()
}

func (s *Store) CountSlowQueriesToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM slow_query_logs WHERE datetime(detected_at, 'localtime') >= date('now', 'localtime')`).Scan(&count)
	return count, err
}

func (s *Store) CountSlowQueriesWeek() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM slow_query_logs WHERE datetime(detected_at, 'localtime') >= date('now', 'localtime', '-7 days')`).Scan(&count)
	return count, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- User CRUD ---

func (s *Store) CreateUser(u *User) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO users (username, github_id, github_login, avatar_url, role) VALUES (?,?,?,?,?)`,
		u.Username, u.GitHubID, u.GitHubLogin, u.AvatarURL, u.Role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByGitHubLogin(login string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, username, github_id, github_login, avatar_url, role, created_at FROM users WHERE github_login=?`, login).
		Scan(&u.ID, &u.Username, &u.GitHubID, &u.GitHubLogin, &u.AvatarURL, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByGitHubID(ghID int64) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, username, github_id, github_login, avatar_url, role, created_at FROM users WHERE github_id=?`, ghID).
		Scan(&u.ID, &u.Username, &u.GitHubID, &u.GitHubLogin, &u.AvatarURL, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) UpdateUserGitHub(id int64, ghID int64, ghLogin, avatarURL string) error {
	_, err := s.db.Exec(`UPDATE users SET github_id=?, github_login=?, avatar_url=? WHERE id=?`, ghID, ghLogin, avatarURL, id)
	return err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, github_id, github_login, avatar_url, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.GitHubID, &u.GitHubLogin, &u.AvatarURL, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// --- Settings ---

func (s *Store) GetSetting(key string) string {
	var val string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return ""
	}
	return val
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetAllSettings() map[string]string {
	m := make(map[string]string)
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			m[k] = v
		}
	}
	return m
}

// --- RocketMQ Config CRUD ---

type RocketMQConfig struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	DashboardURL  string    `json:"dashboard_url"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
	ConsumerGroup string    `json:"consumer_group"`
	Topic         string    `json:"topic"`
	Threshold     int       `json:"threshold"`
	IntervalSec   int       `json:"interval_sec"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RocketMQAlertLog struct {
	ID            int64     `json:"id"`
	ConfigID      int64     `json:"config_id"`
	ConfigName    string    `json:"config_name"`
	ConsumerGroup string    `json:"consumer_group"`
	Topic         string    `json:"topic"`
	DiffTotal     int64     `json:"diff_total"`
	DetectedAt    time.Time `json:"detected_at"`
}

func (s *Store) ListRocketMQConfigs() ([]RocketMQConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, dashboard_url, username, password, consumer_group, topic, threshold, interval_sec, enabled, created_at, updated_at FROM rocketmq_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RocketMQConfig
	for rows.Next() {
		var c RocketMQConfig
		var enabled int
		var encPwd string
		if err := rows.Scan(&c.ID, &c.Name, &c.DashboardURL, &c.Username, &encPwd, &c.ConsumerGroup, &c.Topic, &c.Threshold, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Password, _ = Decrypt(encPwd)
		c.Enabled = enabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetRocketMQConfig(id int64) (*RocketMQConfig, error) {
	var c RocketMQConfig
	var enabled int
	var encPwd string
	err := s.db.QueryRow(`SELECT id, name, dashboard_url, username, password, consumer_group, topic, threshold, interval_sec, enabled, created_at, updated_at FROM rocketmq_configs WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.DashboardURL, &c.Username, &encPwd, &c.ConsumerGroup, &c.Topic, &c.Threshold, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Password, _ = Decrypt(encPwd)
	c.Enabled = enabled == 1
	return &c, nil
}

func (s *Store) CreateRocketMQConfig(c *RocketMQConfig) (int64, error) {
	encPwd, err := Encrypt(c.Password)
	if err != nil {
		return 0, fmt.Errorf("encrypt password: %w", err)
	}
	res, err := s.db.Exec(`INSERT INTO rocketmq_configs (name, dashboard_url, username, password, consumer_group, topic, threshold, interval_sec, enabled) VALUES (?,?,?,?,?,?,?,?,?)`,
		c.Name, c.DashboardURL, c.Username, encPwd, c.ConsumerGroup, c.Topic, c.Threshold, c.IntervalSec, boolToInt(c.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateRocketMQConfig(c *RocketMQConfig) error {
	encPwd, err := Encrypt(c.Password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	_, err = s.db.Exec(`UPDATE rocketmq_configs SET name=?, dashboard_url=?, username=?, password=?, consumer_group=?, topic=?, threshold=?, interval_sec=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		c.Name, c.DashboardURL, c.Username, encPwd, c.ConsumerGroup, c.Topic, c.Threshold, c.IntervalSec, boolToInt(c.Enabled), c.ID)
	return err
}

func (s *Store) DeleteRocketMQConfig(id int64) error {
	_, err := s.db.Exec(`DELETE FROM rocketmq_configs WHERE id=?`, id)
	return err
}

func (s *Store) ToggleRocketMQ(id int64) error {
	_, err := s.db.Exec(`UPDATE rocketmq_configs SET enabled = 1 - enabled, updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *Store) InsertRocketMQAlertLog(l *RocketMQAlertLog) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO rocketmq_alert_logs (config_id, config_name, consumer_group, topic, diff_total) VALUES (?,?,?,?,?)`,
		l.ConfigID, l.ConfigName, l.ConsumerGroup, l.Topic, l.DiffTotal)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListRocketMQAlertLogs(configID *int64, page, pageSize int) ([]RocketMQAlertLog, int, error) {
	countQ := `SELECT COUNT(*) FROM rocketmq_alert_logs`
	dataQ := `SELECT id, config_id, config_name, consumer_group, topic, diff_total, detected_at FROM rocketmq_alert_logs`
	var args []interface{}
	if configID != nil {
		countQ += ` WHERE config_id=?`
		dataQ += ` WHERE config_id=?`
		args = append(args, *configID)
	}
	var total int
	if err := s.db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dataQ += ` ORDER BY detected_at DESC LIMIT ? OFFSET ?`
	offset := (page - 1) * pageSize
	dataArgs := append(args, pageSize, offset)
	rows, err := s.db.Query(dataQ, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []RocketMQAlertLog
	for rows.Next() {
		var l RocketMQAlertLog
		if err := rows.Scan(&l.ID, &l.ConfigID, &l.ConfigName, &l.ConsumerGroup, &l.Topic, &l.DiffTotal, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	return list, total, rows.Err()
}

func (s *Store) CountRocketMQConfigs() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rocketmq_configs`).Scan(&count)
	return count, err
}

func (s *Store) CountRocketMQAlertsToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rocketmq_alert_logs WHERE datetime(detected_at, 'localtime') >= date('now', 'localtime')`).Scan(&count)
	return count, err
}

func (s *Store) GetGlobalNotifications() ([]NotificationConfig, error) {
	rows, err := s.db.Query(`SELECT id, database_id, type, config_json, enabled, created_at, updated_at FROM notification_configs WHERE enabled=1 AND database_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []NotificationConfig
	for rows.Next() {
		var nc NotificationConfig
		var enabled int
		var configStr string
		if err := rows.Scan(&nc.ID, &nc.DatabaseID, &nc.Type, &configStr, &enabled, &nc.CreatedAt, &nc.UpdatedAt); err != nil {
			return nil, err
		}
		nc.ConfigJSON = json.RawMessage(configStr)
		nc.Enabled = enabled == 1
		list = append(list, nc)
	}
	return list, rows.Err()
}

// --- Audit Logs ---

type AuditLog struct {
	ID        int64     `json:"id"`
	User      string    `json:"user"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	TargetID  int64     `json:"target_id"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) InsertAuditLog(l *AuditLog) {
	s.db.Exec(`INSERT INTO audit_logs (user, action, target, target_id, detail, ip) VALUES (?,?,?,?,?,?)`,
		l.User, l.Action, l.Target, l.TargetID, l.Detail, l.IP)
}

func (s *Store) ListAuditLogs(page, pageSize int) ([]AuditLog, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := s.db.Query(`SELECT id, user, action, target, target_id, detail, ip, created_at FROM audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.User, &l.Action, &l.Target, &l.TargetID, &l.Detail, &l.IP, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	return list, total, rows.Err()
}

func (s *Store) PurgeOldAuditLogs() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM audit_logs WHERE created_at < datetime('now', '-90 days')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Health Checks ---

type HealthCheck struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	Method         string    `json:"method"`
	HeadersJSON    string    `json:"headers_json"`
	Body           string    `json:"body"`
	ExpectedStatus int       `json:"expected_status"`
	ExpectedField  string    `json:"expected_field"`
	ExpectedValue  string    `json:"expected_value"`
	TimeoutSec     int       `json:"timeout_sec"`
	IntervalSec    int       `json:"interval_sec"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type HealthCheckLog struct {
	ID         int64     `json:"id"`
	CheckID    int64     `json:"check_id"`
	CheckName  string    `json:"check_name"`
	Status     string    `json:"status"`
	HTTPStatus int       `json:"http_status"`
	Response   string    `json:"response"`
	Error      string    `json:"error"`
	LatencyMs  int64     `json:"latency_ms"`
	DetectedAt time.Time `json:"detected_at"`
}

func (s *Store) ListHealthChecks() ([]HealthCheck, error) {
	rows, err := s.db.Query(`SELECT id, name, url, method, headers_json, body, expected_status, expected_field, expected_value, timeout_sec, interval_sec, enabled, created_at, updated_at FROM health_checks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []HealthCheck
	for rows.Next() {
		var h HealthCheck
		var enabled int
		if err := rows.Scan(&h.ID, &h.Name, &h.URL, &h.Method, &h.HeadersJSON, &h.Body, &h.ExpectedStatus, &h.ExpectedField, &h.ExpectedValue, &h.TimeoutSec, &h.IntervalSec, &enabled, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		h.Enabled = enabled == 1
		list = append(list, h)
	}
	return list, rows.Err()
}

func (s *Store) GetHealthCheck(id int64) (*HealthCheck, error) {
	var h HealthCheck
	var enabled int
	err := s.db.QueryRow(`SELECT id, name, url, method, headers_json, body, expected_status, expected_field, expected_value, timeout_sec, interval_sec, enabled, created_at, updated_at FROM health_checks WHERE id=?`, id).
		Scan(&h.ID, &h.Name, &h.URL, &h.Method, &h.HeadersJSON, &h.Body, &h.ExpectedStatus, &h.ExpectedField, &h.ExpectedValue, &h.TimeoutSec, &h.IntervalSec, &enabled, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, err
	}
	h.Enabled = enabled == 1
	return &h, nil
}

func (s *Store) CreateHealthCheck(h *HealthCheck) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO health_checks (name, url, method, headers_json, body, expected_status, expected_field, expected_value, timeout_sec, interval_sec, enabled) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		h.Name, h.URL, h.Method, h.HeadersJSON, h.Body, h.ExpectedStatus, h.ExpectedField, h.ExpectedValue, h.TimeoutSec, h.IntervalSec, boolToInt(h.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateHealthCheck(h *HealthCheck) error {
	_, err := s.db.Exec(`UPDATE health_checks SET name=?, url=?, method=?, headers_json=?, body=?, expected_status=?, expected_field=?, expected_value=?, timeout_sec=?, interval_sec=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		h.Name, h.URL, h.Method, h.HeadersJSON, h.Body, h.ExpectedStatus, h.ExpectedField, h.ExpectedValue, h.TimeoutSec, h.IntervalSec, boolToInt(h.Enabled), h.ID)
	return err
}

func (s *Store) DeleteHealthCheck(id int64) error {
	_, err := s.db.Exec(`DELETE FROM health_checks WHERE id=?`, id)
	return err
}

func (s *Store) ToggleHealthCheck(id int64) error {
	_, err := s.db.Exec(`UPDATE health_checks SET enabled = 1 - enabled, updated_at = datetime('now') WHERE id=?`, id)
	return err
}

func (s *Store) InsertHealthCheckLog(l *HealthCheckLog) {
	// Only insert on state change; if same status as last record, just update timestamp.
	var lastStatus string
	var lastID int64
	err := s.db.QueryRow(`SELECT id, status FROM health_check_logs WHERE check_id=? ORDER BY detected_at DESC LIMIT 1`, l.CheckID).Scan(&lastID, &lastStatus)
	if err == nil && lastStatus == l.Status {
		s.db.Exec(`UPDATE health_check_logs SET http_status=?, response=?, error=?, latency_ms=?, detected_at=datetime('now') WHERE id=?`,
			l.HTTPStatus, l.Response, l.Error, l.LatencyMs, lastID)
		return
	}
	s.db.Exec(`INSERT INTO health_check_logs (check_id, check_name, status, http_status, response, error, latency_ms) VALUES (?,?,?,?,?,?,?)`,
		l.CheckID, l.CheckName, l.Status, l.HTTPStatus, l.Response, l.Error, l.LatencyMs)
}

func (s *Store) ListHealthCheckLogs(checkID *int64, page, pageSize int) ([]HealthCheckLog, int, error) {
	var total int
	where := ""
	var args []any
	if checkID != nil {
		where = " WHERE check_id=?"
		args = append(args, *checkID)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM health_check_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	queryArgs := append(args, pageSize, offset)
	rows, err := s.db.Query(`SELECT id, check_id, check_name, status, http_status, response, error, latency_ms, detected_at FROM health_check_logs`+where+` ORDER BY detected_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []HealthCheckLog
	for rows.Next() {
		var l HealthCheckLog
		if err := rows.Scan(&l.ID, &l.CheckID, &l.CheckName, &l.Status, &l.HTTPStatus, &l.Response, &l.Error, &l.LatencyMs, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	return list, total, rows.Err()
}

func (s *Store) PurgeOldHealthCheckLogs() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM health_check_logs WHERE detected_at < datetime('now', '-30 days')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) CountHealthChecks() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM health_checks`).Scan(&count)
	return count, err
}

func (s *Store) CountHealthCheckErrorsToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM health_check_logs WHERE status != 'up' AND datetime(detected_at, 'localtime') >= date('now', 'localtime')`).Scan(&count)
	return count, err
}

// --- Grafana Config CRUD ---

type GrafanaConfig struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	GrafanaURL    string    `json:"grafana_url"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
	DatasourceUID string    `json:"datasource_uid"`
	AutoRules     string    `json:"auto_rules"`
	WebhookURL    string    `json:"webhook_url"`
	WebhookUID    string    `json:"webhook_uid"`
	FolderUID     string    `json:"folder_uid"`
	IntervalSec   int       `json:"interval_sec"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type GrafanaAlertLog struct {
	ID          int64      `json:"id"`
	ConfigID    int64      `json:"config_id"`
	ConfigName  string     `json:"config_name"`
	AlertName   string     `json:"alert_name"`
	Status      string     `json:"status"`
	Severity    string     `json:"severity"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Fingerprint string     `json:"fingerprint"`
	LabelsJSON  string     `json:"labels_json"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	DetectedAt  time.Time  `json:"detected_at"`
}

func (s *Store) ListGrafanaConfigs() ([]GrafanaConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, grafana_url, username, password, datasource_uid, auto_rules, webhook_url, webhook_uid, folder_uid, interval_sec, enabled, created_at, updated_at FROM grafana_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []GrafanaConfig
	for rows.Next() {
		var c GrafanaConfig
		var enabled int
		var encPwd string
		if err := rows.Scan(&c.ID, &c.Name, &c.GrafanaURL, &c.Username, &encPwd, &c.DatasourceUID, &c.AutoRules, &c.WebhookURL, &c.WebhookUID, &c.FolderUID, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Password, _ = Decrypt(encPwd)
		c.Enabled = enabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetGrafanaConfig(id int64) (*GrafanaConfig, error) {
	var c GrafanaConfig
	var enabled int
	var encPwd string
	err := s.db.QueryRow(`SELECT id, name, grafana_url, username, password, datasource_uid, auto_rules, webhook_url, webhook_uid, folder_uid, interval_sec, enabled, created_at, updated_at FROM grafana_configs WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.GrafanaURL, &c.Username, &encPwd, &c.DatasourceUID, &c.AutoRules, &c.WebhookURL, &c.WebhookUID, &c.FolderUID, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Password, _ = Decrypt(encPwd)
	c.Enabled = enabled == 1
	return &c, nil
}

func (s *Store) CreateGrafanaConfig(c *GrafanaConfig) (int64, error) {
	encPwd, _ := Encrypt(c.Password)
	res, err := s.db.Exec(`INSERT INTO grafana_configs (name, grafana_url, username, password, datasource_uid, auto_rules, webhook_url, interval_sec, enabled) VALUES (?,?,?,?,?,?,?,?,?)`,
		c.Name, c.GrafanaURL, c.Username, encPwd, c.DatasourceUID, c.AutoRules, c.WebhookURL, c.IntervalSec, boolToInt(c.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateGrafanaConfig(c *GrafanaConfig) error {
	encPwd, _ := Encrypt(c.Password)
	_, err := s.db.Exec(`UPDATE grafana_configs SET name=?, grafana_url=?, username=?, password=?, datasource_uid=?, auto_rules=?, webhook_url=?, interval_sec=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		c.Name, c.GrafanaURL, c.Username, encPwd, c.DatasourceUID, c.AutoRules, c.WebhookURL, c.IntervalSec, boolToInt(c.Enabled), c.ID)
	return err
}

func (s *Store) DeleteGrafanaConfig(id int64) error {
	_, err := s.db.Exec(`DELETE FROM grafana_configs WHERE id = ?`, id)
	return err
}

func (s *Store) ToggleGrafana(id int64) error {
	_, err := s.db.Exec(`UPDATE grafana_configs SET enabled = 1 - enabled, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateGrafanaProvisionUIDs(id int64, webhookUID, folderUID string) error {
	_, err := s.db.Exec(`UPDATE grafana_configs SET webhook_uid=?, folder_uid=?, updated_at=datetime('now') WHERE id=?`, webhookUID, folderUID, id)
	return err
}

func (s *Store) InsertGrafanaAlertLog(l *GrafanaAlertLog) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO grafana_alert_logs (config_id, config_name, alert_name, status, severity, summary, description, fingerprint, labels_json, starts_at, ends_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		l.ConfigID, l.ConfigName, l.AlertName, l.Status, l.Severity, l.Summary, l.Description, l.Fingerprint, l.LabelsJSON, l.StartsAt, l.EndsAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListGrafanaAlertLogs(configID *int64, page, pageSize int) ([]GrafanaAlertLog, int, error) {
	countQ := `SELECT COUNT(*) FROM grafana_alert_logs`
	dataQ := `SELECT id, config_id, config_name, alert_name, status, severity, summary, description, fingerprint, labels_json, starts_at, ends_at, detected_at FROM grafana_alert_logs`
	var args []interface{}
	if configID != nil {
		countQ += ` WHERE config_id = ?`
		dataQ += ` WHERE config_id = ?`
		args = append(args, *configID)
	}
	var total int
	if err := s.db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dataQ += ` ORDER BY detected_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(dataQ, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []GrafanaAlertLog
	for rows.Next() {
		var l GrafanaAlertLog
		if err := rows.Scan(&l.ID, &l.ConfigID, &l.ConfigName, &l.AlertName, &l.Status, &l.Severity, &l.Summary, &l.Description, &l.Fingerprint, &l.LabelsJSON, &l.StartsAt, &l.EndsAt, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	return list, total, rows.Err()
}

func (s *Store) CountGrafanaConfigs() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM grafana_configs`).Scan(&count)
	return count, err
}

func (s *Store) CountGrafanaAlertsToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM grafana_alert_logs WHERE datetime(detected_at, 'localtime') >= date('now', 'localtime')`).Scan(&count)
	return count, err
}

func (s *Store) CountGrafanaRunning() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM grafana_configs WHERE enabled = 1`).Scan(&count)
	return count, err
}

// --- Sessions ---

func (s *Store) SaveSession(sess *auth.SessionRow) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO sessions (token, username, user_id, github_login, role, avatar_url, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.Token, sess.Username, sess.UserID, sess.GitHubLogin, sess.Role, sess.AvatarURL, sess.ExpiresAt)
	return err
}

func (s *Store) GetSession(token string) (*auth.SessionRow, error) {
	row := s.db.QueryRow(`SELECT token, username, user_id, github_login, role, avatar_url, expires_at FROM sessions WHERE token = ?`, token)
	var sess auth.SessionRow
	if err := row.Scan(&sess.Token, &sess.Username, &sess.UserID, &sess.GitHubLogin, &sess.Role, &sess.AvatarURL, &sess.ExpiresAt); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) CleanupExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < datetime('now')`)
	return err
}

// --- Notified PIDs ---

func (s *Store) IsProcessNotified(dbID int64, processID uint64) bool {
	var n int
	err := s.db.QueryRow(`SELECT 1 FROM notified_pids WHERE database_id=? AND process_id=?`, dbID, processID).Scan(&n)
	return err == nil
}

func (s *Store) MarkProcessNotified(dbID int64, processID uint64) {
	s.db.Exec(`INSERT OR IGNORE INTO notified_pids (database_id, process_id) VALUES (?, ?)`, dbID, processID)
}

func (s *Store) ClearNotifiedPIDs(dbID int64, activeProcessIDs []uint64) {
	if len(activeProcessIDs) == 0 {
		s.db.Exec(`DELETE FROM notified_pids WHERE database_id=?`, dbID)
		return
	}
	// Keep only active PIDs, remove stale ones
	placeholders := make([]string, len(activeProcessIDs))
	args := []any{dbID}
	for i, pid := range activeProcessIDs {
		placeholders[i] = "?"
		args = append(args, pid)
	}
	s.db.Exec(`DELETE FROM notified_pids WHERE database_id=? AND process_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
}

func (s *Store) CleanupOldNotifiedPIDs() {
	s.db.Exec(`DELETE FROM notified_pids WHERE notified_at < datetime('now', '-1 day')`)
}

func (s *Store) InitDefaultSettings() {
	defaults := map[string]string{
		"password_login_enabled": "1",
		"github_enabled":         "0",
	}
	for k, v := range defaults {
		if s.GetSetting(k) == "" {
			s.SetSetting(k, v)
		}
	}
}
