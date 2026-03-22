package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"time"

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
	rows, err := s.db.Query(`SELECT id, database_id, type, config_json, enabled, created_at, updated_at FROM notification_configs WHERE enabled=1 AND (database_id=? OR database_id IS NULL) ORDER BY database_id DESC`, databaseID)
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
	if n, err := s.PurgeOldLogs(); err == nil && n > 0 {
		log.Printf("purged %d old slow query logs", n)
	}
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := s.PurgeOldLogs(); err == nil && n > 0 {
					log.Printf("purged %d old slow query logs", n)
				}
			}
		}
	}()
}

func (s *Store) CountSlowQueriesToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM slow_query_logs WHERE detected_at >= date('now')`).Scan(&count)
	return count, err
}

func (s *Store) CountSlowQueriesWeek() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM slow_query_logs WHERE detected_at >= date('now', '-7 days')`).Scan(&count)
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
