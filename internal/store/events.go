package store

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// AlertEvent 把「触发 → 持续 → 恢复」建模成一条事件。
//
// 之前告警只有逐次判定的流水（prom_alert_logs 等 5 张表），值班的人想知道
// "现在有什么问题、持续多久了"，得自己从几千行流水里聚合。事件表把一次
// 告警episode 收敛成一行：首次触发时间、最近一次确认、峰值、恢复时间。
// 同一条规则的一个 episode 只有一行，firing 期间反复更新它而不是插新行。
type AlertEvent struct {
	ID         int64  `json:"id"`
	Source     string `json:"source"` // prom / health / custom_sql / cert
	CheckID    int64  `json:"check_id"`
	CheckName  string `json:"check_name"`
	Title      string `json:"title"` // 规则名去掉对象前缀，用于跨对象聚合
	TargetID   int64  `json:"target_id"`
	TargetName string `json:"target_name"`
	Dimension  string `json:"dimension"`
	Severity   string `json:"severity"`
	Status     string `json:"status"` // firing / resolved
	Value      string `json:"value"`  // 最近一次的值
	Detail     string `json:"detail"` // 聚合来源（如具体是哪个容器）
	PeakValue  string `json:"peak_value"`
	Threshold  string `json:"threshold"`
	Message    string `json:"message"`

	FirstAt     time.Time  `json:"first_at"`
	LastAt      time.Time  `json:"last_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	NotifyCount int        `json:"notify_count"`
}

// UpsertFiringEvent 打开或续写一个 firing 事件。
// 同一 (source, check_id) 已有 firing 事件则更新 last_at / value / peak，
// 否则新开一条。notified 为 true 时给通知计数 +1。
func (s *Store) UpsertFiringEvent(e *AlertEvent, notified bool) {
	var id int64
	var peak string
	err := s.db.QueryRow(`SELECT id, peak_value FROM alert_events
		WHERE source = ? AND check_id = ? AND status = 'firing'`,
		e.Source, e.CheckID).Scan(&id, &peak)

	notifyInc := 0
	if notified {
		notifyInc = 1
	}

	if err == sql.ErrNoRows {
		if e.PeakValue == "" {
			e.PeakValue = e.Value
		}
		s.db.Exec(`INSERT INTO alert_events
			(source, check_id, check_name, title, target_id, target_name, dimension,
			 severity, status, value, detail, peak_value, threshold, message,
			 first_at, last_at, notify_count)
			VALUES (?,?,?,?,?,?,?,?,'firing',?,?,?,?,?,datetime('now'),datetime('now'),?)`,
			e.Source, e.CheckID, e.CheckName, e.Title, e.TargetID, e.TargetName,
			e.Dimension, e.Severity, e.Value, e.Detail, e.PeakValue, e.Threshold, e.Message, notifyInc)
		return
	}
	if err != nil {
		return
	}

	// 峰值只对能解析成数字的值有意义
	newPeak := peak
	if cur, curErr := strconv.ParseFloat(strings.TrimSpace(e.Value), 64); curErr == nil {
		if old, oldErr := strconv.ParseFloat(strings.TrimSpace(peak), 64); oldErr != nil || cur > old {
			newPeak = e.Value
		}
	}
	// threshold/target_name 一并刷新：老事件是在规则改配置前创建的，不刷会一直展示旧值/空值
	s.db.Exec(`UPDATE alert_events SET
		value = ?, detail = ?, peak_value = ?, message = ?, threshold = ?, target_name = ?,
		last_at = datetime('now'), notify_count = notify_count + ?
		WHERE id = ?`,
		e.Value, e.Detail, newPeak, e.Message, e.Threshold, e.TargetName, notifyInc, id)
}

// ResolveEvent 关闭 (source, check_id) 的 firing 事件（若有）。
func (s *Store) ResolveEvent(source string, checkID int64, finalValue string) {
	s.db.Exec(`UPDATE alert_events SET
		status = 'resolved', resolved_at = datetime('now'), last_at = datetime('now'),
		value = CASE WHEN ? <> '' THEN ? ELSE value END
		WHERE source = ? AND check_id = ? AND status = 'firing'`,
		finalValue, finalValue, source, checkID)
}

const alertEventColumns = `id, source, check_id, check_name, title, target_id, target_name,
	dimension, severity, status, value, detail, peak_value, threshold, message,
	first_at, last_at, resolved_at, notify_count`

func scanAlertEvent(sc interface{ Scan(...any) error }) (AlertEvent, error) {
	var e AlertEvent
	err := sc.Scan(&e.ID, &e.Source, &e.CheckID, &e.CheckName, &e.Title, &e.TargetID,
		&e.TargetName, &e.Dimension, &e.Severity, &e.Status, &e.Value, &e.Detail, &e.PeakValue,
		&e.Threshold, &e.Message, &e.FirstAt, &e.LastAt, &e.ResolvedAt, &e.NotifyCount)
	return e, err
}

// ListAlertEvents 按状态列出事件。status 为空表示全部。
func (s *Store) ListAlertEvents(status string, limit int) ([]AlertEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT ` + alertEventColumns + ` FROM alert_events `
	args := []any{}
	if status != "" {
		q += `WHERE status = ? `
		args = append(args, status)
	}
	q += `ORDER BY (status = 'firing') DESC, last_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertEvent{}
	for rows.Next() {
		if e, err := scanAlertEvent(rows); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// ListAlertEventsByTarget 给对象详情页用。
func (s *Store) ListAlertEventsByTarget(source string, targetID int64, limit int) ([]AlertEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+alertEventColumns+` FROM alert_events
		WHERE source = ? AND target_id = ?
		ORDER BY (status = 'firing') DESC, last_at DESC LIMIT ?`,
		source, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertEvent{}
	for rows.Next() {
		if e, err := scanAlertEvent(rows); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// CountFiringBySeverity 给顶栏状态条用。
func (s *Store) CountFiringBySeverity() (critical, warning int) {
	rows, err := s.db.Query(`SELECT severity, COUNT(*) FROM alert_events
		WHERE status = 'firing' GROUP BY severity`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sev string
		var n int
		if rows.Scan(&sev, &n) == nil {
			if sev == "critical" || sev == "error" {
				critical += n
			} else {
				warning += n
			}
		}
	}
	return
}

// PurgeOldAlertEvents 清理已恢复且超过留存期的事件。firing 的永不清。
func (s *Store) PurgeOldAlertEvents() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM alert_events
		WHERE status = 'resolved' AND resolved_at < datetime('now', '-90 days')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
