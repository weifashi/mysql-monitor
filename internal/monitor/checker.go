package monitor

import (
	"database/sql"
	"fmt"
	"strings"

	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

const maxSlowQueries = 20

func GetLongQueries(db *sql.DB, thresholdSec float64) ([]notify.LongQuery, error) {
	query := fmt.Sprintf(`
		SELECT
		  ess.thread_id,
		  t.processlist_id,
		  t.processlist_user,
		  t.processlist_host,
		  COALESCE(t.processlist_db, '') AS db_name,
		  COALESCE(ess.sql_text, '') AS sql_text,
		  ess.timer_wait / 1000000000000 AS exec_sec,
		  ess.lock_time / 1000000000000 AS lock_sec,
		  ess.rows_examined,
		  ess.rows_sent,
		  COALESCE(t.processlist_state, '') AS state
		FROM performance_schema.events_statements_current ess
		JOIN performance_schema.threads t
		  ON ess.thread_id = t.thread_id
		WHERE ess.sql_text IS NOT NULL
		  AND ess.timer_wait / 1000000000000 > ?
		ORDER BY ess.timer_wait DESC
		LIMIT %d
	`, maxSlowQueries)

	rows, err := db.Query(query, thresholdSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []notify.LongQuery
	for rows.Next() {
		var q notify.LongQuery
		err := rows.Scan(
			&q.ThreadID, &q.ProcessID, &q.User, &q.Host, &q.DB,
			&q.SQLText, &q.ExecSec, &q.LockSec,
			&q.RowsExam, &q.RowsSent, &q.State,
		)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return queries, nil
}

func FormatNotificationText(name, host string, port int, queries []notify.LongQuery) string {
	var b strings.Builder
	b.WriteString("MySQL 慢SQL告警\n\n")
	b.WriteString(fmt.Sprintf("数据库: %s (%s:%d)\n", name, host, port))
	b.WriteString(fmt.Sprintf("慢SQL数量: %d\n\n", len(queries)))
	for i, q := range queries {
		b.WriteString(fmt.Sprintf("【慢SQL #%d】\n", i+1))
		b.WriteString(fmt.Sprintf("线程ID: %d | 连接ID: %d\n", q.ThreadID, q.ProcessID))
		b.WriteString(fmt.Sprintf("用户: %s@%s | 数据库: %s\n", q.User, q.Host, q.DB))
		b.WriteString(fmt.Sprintf("执行时间: %.1fs | 锁等待: %.1fs\n", q.ExecSec, q.LockSec))
		b.WriteString(fmt.Sprintf("扫描行数: %d | 返回行数: %d\n", q.RowsExam, q.RowsSent))
		sqlText := q.SQLText
		if len(sqlText) > 150 {
			sqlText = sqlText[:150] + "..."
		}
		b.WriteString(fmt.Sprintf("SQL: %s\n", sqlText))
		b.WriteString(fmt.Sprintf("终止: KILL %d;\n\n", q.ProcessID))
	}
	return b.String()
}

func FormatErrorNotificationText(name, host string, port int, err error) string {
	var b strings.Builder
	b.WriteString("MySQL 连接异常告警\n\n")
	b.WriteString(fmt.Sprintf("数据库: %s (%s:%d)\n", name, host, port))
	b.WriteString(fmt.Sprintf("错误信息: %v\n\n", err))
	b.WriteString("该告警仅发送一次，连接恢复后如再次出现异常将重新通知。")
	return b.String()
}

// NotifierDB is the persistence interface for notified PIDs and ignored SQL.
type NotifierDB interface {
	IsProcessNotified(dbID int64, processID uint64) bool
	MarkProcessNotified(dbID int64, processID uint64)
	ClearNotifiedPIDs(dbID int64, activeProcessIDs []uint64)
	IsSQLIgnored(databaseID int64, fingerprint string) bool
}

// Notifier tracks which process IDs have been notified to avoid duplicate alerts.
type Notifier struct {
	dbID int64
	db   NotifierDB
}

func NewNotifier(dbID int64, db NotifierDB) *Notifier {
	return &Notifier{dbID: dbID, db: db}
}

func (n *Notifier) ClearNotifiedPIDs() {
	n.db.ClearNotifiedPIDs(n.dbID, nil)
}

func (n *Notifier) SyncAndShouldNotify(queries []notify.LongQuery) bool {
	// Sync: remove PIDs no longer active
	activeIDs := make([]uint64, len(queries))
	for i, q := range queries {
		activeIDs[i] = q.ProcessID
	}
	n.db.ClearNotifiedPIDs(n.dbID, activeIDs)

	// Check if any query has not been notified yet
	for _, q := range queries {
		if !n.db.IsProcessNotified(n.dbID, q.ProcessID) {
			return true
		}
	}
	return false
}

func (n *Notifier) MarkPIDsNotified(queries []notify.LongQuery) {
	for _, q := range queries {
		n.db.MarkProcessNotified(n.dbID, q.ProcessID)
	}
}

// FilterIgnored removes queries whose SQL fingerprint matches an ignored pattern.
func (n *Notifier) FilterIgnored(queries []notify.LongQuery) []notify.LongQuery {
	var filtered []notify.LongQuery
	for _, q := range queries {
		fp := store.NormalizeSQL(q.SQLText)
		if !n.db.IsSQLIgnored(n.dbID, fp) {
			filtered = append(filtered, q)
		}
	}
	return filtered
}
