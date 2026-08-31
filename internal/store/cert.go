package store

import (
	"database/sql"
	"time"
)

// CertCheck 是一条 TLS 证书到期检查。
// 证书过期属于"确定会发生、且能提前很久知道"的故障，
// 但没有监控时往往在过期当天才发现。
type CertCheck struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Endpoint      string    `json:"endpoint"`    // host:port，如 coolify.ttpos.org:443
	ServerName    string    `json:"server_name"` // SNI，留空则从 endpoint 推断
	WarnDays      int       `json:"warn_days"`
	CriticalDays  int       `json:"critical_days"`
	IntervalSec   int       `json:"interval_sec"`
	NotifyEnabled bool      `json:"notify_enabled"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CertCheckLog struct {
	ID         int64      `json:"id"`
	CheckID    int64      `json:"check_id"`
	CheckName  string     `json:"check_name"`
	Endpoint   string     `json:"endpoint"`
	Status     string     `json:"status"` // ok / warning / critical / error
	Issuer     string     `json:"issuer"`
	Subject    string     `json:"subject"`
	NotAfter   *time.Time `json:"not_after"`
	DaysLeft   int        `json:"days_left"`
	Message    string     `json:"message"`
	Error      string     `json:"error"`
	DetectedAt time.Time  `json:"detected_at"`
}

const certCheckColumns = `id, name, endpoint, server_name, warn_days, critical_days,
	interval_sec, notify_enabled, enabled, created_at, updated_at`

func scanCertCheck(sc interface{ Scan(...interface{}) error }) (CertCheck, error) {
	var c CertCheck
	err := sc.Scan(&c.ID, &c.Name, &c.Endpoint, &c.ServerName, &c.WarnDays, &c.CriticalDays,
		&c.IntervalSec, &c.NotifyEnabled, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) ListCertChecks() ([]CertCheck, error) {
	rows, err := s.db.Query(`SELECT ` + certCheckColumns + ` FROM cert_checks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CertCheck, 0)
	for rows.Next() {
		c, err := scanCertCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCertCheck(id int64) (*CertCheck, error) {
	row := s.db.QueryRow(`SELECT `+certCheckColumns+` FROM cert_checks WHERE id = ?`, id)
	c, err := scanCertCheck(row)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateCertCheck(c *CertCheck) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO cert_checks
		(name, endpoint, server_name, warn_days, critical_days, interval_sec, notify_enabled, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Endpoint, c.ServerName, c.WarnDays, c.CriticalDays,
		c.IntervalSec, c.NotifyEnabled, c.Enabled)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateCertCheck(c *CertCheck) error {
	_, err := s.db.Exec(`UPDATE cert_checks SET
		name = ?, endpoint = ?, server_name = ?, warn_days = ?, critical_days = ?,
		interval_sec = ?, notify_enabled = ?, enabled = ?, updated_at = datetime('now')
		WHERE id = ?`,
		c.Name, c.Endpoint, c.ServerName, c.WarnDays, c.CriticalDays,
		c.IntervalSec, c.NotifyEnabled, c.Enabled, c.ID)
	return err
}

func (s *Store) DeleteCertCheck(id int64) error {
	_, err := s.db.Exec(`DELETE FROM cert_checks WHERE id = ?`, id)
	return err
}

func (s *Store) ToggleCertCheck(id int64) error {
	_, err := s.db.Exec(`UPDATE cert_checks SET enabled = NOT enabled, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) InsertCertCheckLog(l *CertCheckLog) {
	_, err := s.db.Exec(`INSERT INTO cert_check_logs
		(check_id, check_name, endpoint, status, issuer, subject, not_after, days_left, message, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.CheckID, l.CheckName, l.Endpoint, l.Status, l.Issuer, l.Subject,
		l.NotAfter, l.DaysLeft, l.Message, l.Error)
	if err != nil {
		logInsertFailure("cert_check_logs", err)
	}
}

func (s *Store) LastCertCheckStatus(checkID int64) (string, bool, error) {
	var status string
	err := s.db.QueryRow(`SELECT status FROM cert_check_logs WHERE check_id = ?
		ORDER BY id DESC LIMIT 1`, checkID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

func (s *Store) ListCertCheckLogs(checkID *int64, page, pageSize int) ([]CertCheckLog, int, error) {
	where := ` WHERE 1 = 1`
	args := []interface{}{}
	if checkID != nil {
		where += ` AND check_id = ?`
		args = append(args, *checkID)
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM cert_check_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := s.db.Query(`SELECT id, check_id, check_name, endpoint, status, issuer, subject,
		not_after, days_left, message, error, detected_at
		FROM cert_check_logs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]CertCheckLog, 0)
	for rows.Next() {
		var l CertCheckLog
		var notAfter sql.NullTime
		if err := rows.Scan(&l.ID, &l.CheckID, &l.CheckName, &l.Endpoint, &l.Status, &l.Issuer,
			&l.Subject, &notAfter, &l.DaysLeft, &l.Message, &l.Error, &l.DetectedAt); err != nil {
			return nil, 0, err
		}
		if notAfter.Valid {
			t := notAfter.Time
			l.NotAfter = &t
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

func (s *Store) PurgeOldCertCheckLogs() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM cert_check_logs WHERE detected_at < datetime('now', '-90 days')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) CountCertChecks() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cert_checks WHERE enabled = 1`).Scan(&n)
	return n, err
}

// CountExpiringCerts 统计当前处于告警状态的证书数（最近一次检查为 warning 或 critical）。
func (s *Store) CountExpiringCerts() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT check_id, status, MAX(id) FROM cert_check_logs GROUP BY check_id
	) WHERE status IN ('warning', 'critical')`).Scan(&n)
	return n, err
}
