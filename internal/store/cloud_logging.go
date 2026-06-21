package store

import "time"

const (
	CloudLoggingMetricCount           = "count"
	CloudLoggingMetricPeakConcurrency = "peak_concurrency"
)

type CloudLoggingConfig struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	ProjectID       string    `json:"project_id"`
	ResourceNames   string    `json:"resource_names"`
	CredentialsFile string    `json:"credentials_file"`
	DefaultFilter   string    `json:"default_filter"`
	IntervalSec     int       `json:"interval_sec"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CloudLoggingCheck struct {
	ID              int64     `json:"id"`
	ConfigID        int64     `json:"config_id"`
	ConfigName      string    `json:"config_name"`
	Name            string    `json:"name"`
	Filter          string    `json:"filter"`
	MetricType      string    `json:"metric_type"`
	LookbackMinutes int       `json:"lookback_minutes"`
	ThresholdCount  int       `json:"threshold_count"`
	IntervalSec     int       `json:"interval_sec"`
	NotifyEnabled   bool      `json:"notify_enabled"`
	RecoveryNotify  bool      `json:"recovery_notify"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CloudLoggingAlertLog struct {
	ID             int64     `json:"id"`
	CheckID        int64     `json:"check_id"`
	CheckName      string    `json:"check_name"`
	ConfigID       int64     `json:"config_id"`
	ConfigName     string    `json:"config_name"`
	Status         string    `json:"status"`
	MatchCount     int       `json:"match_count"`
	ThresholdCount int       `json:"threshold_count"`
	Filter         string    `json:"filter"`
	Sample         string    `json:"sample"`
	Error          string    `json:"error"`
	DurationMs     int64     `json:"duration_ms"`
	DetectedAt     time.Time `json:"detected_at"`
}

func normalizeCloudLoggingConfig(c *CloudLoggingConfig) {
	if c.IntervalSec <= 0 {
		c.IntervalSec = 60
	}
}

func normalizeCloudLoggingCheck(c *CloudLoggingCheck) {
	if c.MetricType == "" {
		c.MetricType = CloudLoggingMetricCount
	}
	if c.MetricType != CloudLoggingMetricCount && c.MetricType != CloudLoggingMetricPeakConcurrency {
		c.MetricType = CloudLoggingMetricCount
	}
	if c.LookbackMinutes <= 0 {
		c.LookbackMinutes = 5
	}
	if c.IntervalSec <= 0 {
		c.IntervalSec = 60
	}
	if c.ThresholdCount < 0 {
		c.ThresholdCount = 0
	}
}

func (s *Store) ListCloudLoggingConfigs() ([]CloudLoggingConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, project_id, resource_names, credentials_file, default_filter, interval_sec, enabled, created_at, updated_at FROM cloud_logging_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CloudLoggingConfig
	for rows.Next() {
		var c CloudLoggingConfig
		var enabled int
		if err := rows.Scan(&c.ID, &c.Name, &c.ProjectID, &c.ResourceNames, &c.CredentialsFile, &c.DefaultFilter, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		normalizeCloudLoggingConfig(&c)
		c.Enabled = enabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetCloudLoggingConfig(id int64) (*CloudLoggingConfig, error) {
	var c CloudLoggingConfig
	var enabled int
	err := s.db.QueryRow(`SELECT id, name, project_id, resource_names, credentials_file, default_filter, interval_sec, enabled, created_at, updated_at FROM cloud_logging_configs WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.ProjectID, &c.ResourceNames, &c.CredentialsFile, &c.DefaultFilter, &c.IntervalSec, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	normalizeCloudLoggingConfig(&c)
	c.Enabled = enabled == 1
	return &c, nil
}

func (s *Store) CreateCloudLoggingConfig(c *CloudLoggingConfig) (int64, error) {
	normalizeCloudLoggingConfig(c)
	res, err := s.db.Exec(`INSERT INTO cloud_logging_configs (name, project_id, resource_names, credentials_file, default_filter, interval_sec, enabled) VALUES (?,?,?,?,?,?,?)`,
		c.Name, c.ProjectID, c.ResourceNames, c.CredentialsFile, c.DefaultFilter, c.IntervalSec, boolToInt(c.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateCloudLoggingConfig(c *CloudLoggingConfig) error {
	normalizeCloudLoggingConfig(c)
	_, err := s.db.Exec(`UPDATE cloud_logging_configs SET name=?, project_id=?, resource_names=?, credentials_file=?, default_filter=?, interval_sec=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		c.Name, c.ProjectID, c.ResourceNames, c.CredentialsFile, c.DefaultFilter, c.IntervalSec, boolToInt(c.Enabled), c.ID)
	return err
}

func (s *Store) DeleteCloudLoggingConfig(id int64) error {
	_, err := s.db.Exec(`DELETE FROM cloud_logging_configs WHERE id=?`, id)
	return err
}

func (s *Store) ToggleCloudLoggingConfig(id int64) error {
	_, err := s.db.Exec(`UPDATE cloud_logging_configs SET enabled = 1 - enabled, updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *Store) ListCloudLoggingChecks() ([]CloudLoggingCheck, error) {
	rows, err := s.db.Query(`SELECT c.id, c.config_id, COALESCE(cfg.name, ''), c.name, c.filter, c.metric_type, c.lookback_minutes, c.threshold_count, c.interval_sec, c.notify_enabled, c.recovery_notify, c.enabled, c.created_at, c.updated_at FROM cloud_logging_checks c LEFT JOIN cloud_logging_configs cfg ON cfg.id=c.config_id ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CloudLoggingCheck
	for rows.Next() {
		var c CloudLoggingCheck
		var notifyEnabled, recoveryNotify, enabled int
		if err := rows.Scan(&c.ID, &c.ConfigID, &c.ConfigName, &c.Name, &c.Filter, &c.MetricType, &c.LookbackMinutes, &c.ThresholdCount, &c.IntervalSec, &notifyEnabled, &recoveryNotify, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		normalizeCloudLoggingCheck(&c)
		c.NotifyEnabled = notifyEnabled == 1
		c.RecoveryNotify = recoveryNotify == 1
		c.Enabled = enabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetCloudLoggingCheck(id int64) (*CloudLoggingCheck, error) {
	var c CloudLoggingCheck
	var notifyEnabled, recoveryNotify, enabled int
	err := s.db.QueryRow(`SELECT c.id, c.config_id, COALESCE(cfg.name, ''), c.name, c.filter, c.metric_type, c.lookback_minutes, c.threshold_count, c.interval_sec, c.notify_enabled, c.recovery_notify, c.enabled, c.created_at, c.updated_at FROM cloud_logging_checks c LEFT JOIN cloud_logging_configs cfg ON cfg.id=c.config_id WHERE c.id=?`, id).
		Scan(&c.ID, &c.ConfigID, &c.ConfigName, &c.Name, &c.Filter, &c.MetricType, &c.LookbackMinutes, &c.ThresholdCount, &c.IntervalSec, &notifyEnabled, &recoveryNotify, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	normalizeCloudLoggingCheck(&c)
	c.NotifyEnabled = notifyEnabled == 1
	c.RecoveryNotify = recoveryNotify == 1
	c.Enabled = enabled == 1
	return &c, nil
}

func (s *Store) CreateCloudLoggingCheck(c *CloudLoggingCheck) (int64, error) {
	normalizeCloudLoggingCheck(c)
	res, err := s.db.Exec(`INSERT INTO cloud_logging_checks (config_id, name, filter, metric_type, lookback_minutes, threshold_count, interval_sec, notify_enabled, recovery_notify, enabled) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ConfigID, c.Name, c.Filter, c.MetricType, c.LookbackMinutes, c.ThresholdCount, c.IntervalSec, boolToInt(c.NotifyEnabled), boolToInt(c.RecoveryNotify), boolToInt(c.Enabled))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateCloudLoggingCheck(c *CloudLoggingCheck) error {
	normalizeCloudLoggingCheck(c)
	_, err := s.db.Exec(`UPDATE cloud_logging_checks SET config_id=?, name=?, filter=?, metric_type=?, lookback_minutes=?, threshold_count=?, interval_sec=?, notify_enabled=?, recovery_notify=?, enabled=?, updated_at=datetime('now') WHERE id=?`,
		c.ConfigID, c.Name, c.Filter, c.MetricType, c.LookbackMinutes, c.ThresholdCount, c.IntervalSec, boolToInt(c.NotifyEnabled), boolToInt(c.RecoveryNotify), boolToInt(c.Enabled), c.ID)
	return err
}

func (s *Store) DeleteCloudLoggingCheck(id int64) error {
	_, err := s.db.Exec(`DELETE FROM cloud_logging_checks WHERE id=?`, id)
	return err
}

func (s *Store) ToggleCloudLoggingCheck(id int64) error {
	_, err := s.db.Exec(`UPDATE cloud_logging_checks SET enabled = 1 - enabled, updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *Store) InsertCloudLoggingAlertLog(l *CloudLoggingAlertLog) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO cloud_logging_alert_logs (check_id, check_name, config_id, config_name, status, match_count, threshold_count, filter, sample, error, duration_ms) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		l.CheckID, l.CheckName, l.ConfigID, l.ConfigName, l.Status, l.MatchCount, l.ThresholdCount, l.Filter, l.Sample, l.Error, l.DurationMs)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListCloudLoggingAlertLogs(checkID *int64, page, pageSize int) ([]CloudLoggingAlertLog, int, error) {
	var total int
	where := ""
	var args []any
	if checkID != nil {
		where = " WHERE check_id=?"
		args = append(args, *checkID)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM cloud_logging_alert_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	queryArgs := append(args, pageSize, offset)
	rows, err := s.db.Query(`SELECT id, check_id, check_name, config_id, config_name, status, match_count, threshold_count, filter, sample, error, duration_ms, detected_at FROM cloud_logging_alert_logs`+where+` ORDER BY detected_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []CloudLoggingAlertLog
	for rows.Next() {
		var l CloudLoggingAlertLog
		if err := rows.Scan(&l.ID, &l.CheckID, &l.CheckName, &l.ConfigID, &l.ConfigName, &l.Status, &l.MatchCount, &l.ThresholdCount, &l.Filter, &l.Sample, &l.Error, &l.DurationMs, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, l)
	}
	return list, total, rows.Err()
}

func (s *Store) CountCloudLoggingConfigs() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cloud_logging_configs`).Scan(&count)
	return count, err
}

func (s *Store) CountCloudLoggingChecksRunning() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cloud_logging_checks c JOIN cloud_logging_configs cfg ON cfg.id=c.config_id WHERE c.enabled=1 AND cfg.enabled=1`).Scan(&count)
	return count, err
}

func (s *Store) CountCloudLoggingAlertsToday() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cloud_logging_alert_logs WHERE status='alert' AND datetime(detected_at, 'localtime') >= date('now', 'localtime')`).Scan(&count)
	return count, err
}
