package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"mysql-monitor/internal/notify"
	"mysql-monitor/internal/store"
)

type Manager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus
	mu         sync.Mutex
	monitors   map[int64]*dbMonitor
}

type dbMonitor struct {
	cancel context.CancelFunc
}

func NewManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *Manager {
	return &Manager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]*dbMonitor),
	}
}

func (m *Manager) StartAll() error {
	dbs, err := m.store.ListDatabases()
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	for _, db := range dbs {
		if db.Enabled {
			if err := m.StartDatabase(db.ID); err != nil {
				log.Printf("failed to start monitor for %s: %v", db.Name, err)
			}
		}
	}
	return nil
}

func (m *Manager) StartDatabase(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	dbCfg, err := m.store.GetDatabase(id)
	if err != nil {
		return fmt.Errorf("get database %d: %w", id, err)
	}

	mysqlDB, err := sql.Open("mysql", dbCfg.DSN())
	if err != nil {
		return fmt.Errorf("open mysql %s: %w", dbCfg.Name, err)
	}
	mysqlDB.SetMaxOpenConns(2)
	mysqlDB.SetMaxIdleConns(1)
	mysqlDB.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = &dbMonitor{cancel: cancel}

	go m.runMonitor(ctx, id, dbCfg, mysqlDB)
	log.Printf("started monitor for %s (%s:%d)", dbCfg.Name, dbCfg.Host, dbCfg.Port)
	return nil
}

func (m *Manager) StopDatabase(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mon, ok := m.monitors[id]; ok {
		mon.cancel()
		delete(m.monitors, id)
		log.Printf("stopped monitor for database id=%d", id)
	}
}

func (m *Manager) RestartDatabase(id int64) error {
	m.StopDatabase(id)
	time.Sleep(100 * time.Millisecond)
	return m.StartDatabase(id)
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mon := range m.monitors {
		mon.cancel()
		delete(m.monitors, id)
	}
	log.Println("all monitors stopped")
}

func (m *Manager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *Manager) runMonitor(ctx context.Context, dbID int64, dbCfg *store.Database, mysqlDB *sql.DB) {
	defer mysqlDB.Close()

	notifier := NewNotifier(dbID, m.store)
	errorNotified := false // true = error notification already sent, waiting for recovery
	ticker := time.NewTicker(time.Duration(dbCfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	m.doCheck(dbID, dbCfg, mysqlDB, notifier, &errorNotified)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.doCheck(dbID, dbCfg, mysqlDB, notifier, &errorNotified)
		}
	}
}

func (m *Manager) emit(typ string, dbID int64, dbName, message string, data interface{}) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(MonitorEvent{
		Type:       typ,
		DatabaseID: dbID,
		DBName:     dbName,
		Message:    message,
		Timestamp:  time.Now(),
		Data:       data,
	})
}

func (m *Manager) doCheck(dbID int64, dbCfg *store.Database, mysqlDB *sql.DB, notifier *Notifier, errorNotified *bool) {
	m.emit("checking", dbID, dbCfg.Name, "检查中...", nil)

	queries, err := GetLongQueries(mysqlDB, float64(dbCfg.ThresholdSec))
	if err != nil {
		log.Printf("[%s] query error: %v", dbCfg.Name, err)
		m.emit("error", dbID, dbCfg.Name, fmt.Sprintf("查询错误: %v", err), nil)

		// Send error notification only once until recovery
		if !*errorNotified {
			errMsg := FormatErrorNotificationText(dbCfg.Name, dbCfg.Host, dbCfg.Port, err)
			if sendErr := m.dispatcher.SendNotifications(dbID, errMsg); sendErr != nil {
				log.Printf("[%s] error notification send failed: %v", dbCfg.Name, sendErr)
			} else {
				m.emit("notified", dbID, dbCfg.Name, "已发送连接错误通知", nil)
			}
			*errorNotified = true
		}
		return
	}

	// Query succeeded — if previously in error state, send recovery notification
	if *errorNotified {
		*errorNotified = false
		recoveryMsg := fmt.Sprintf("MySQL 连接恢复通知\n\n数据库: %s (%s:%d)\n状态: 连接已恢复正常", dbCfg.Name, dbCfg.Host, dbCfg.Port)
		if sendErr := m.dispatcher.SendNotifications(dbID, recoveryMsg); sendErr != nil {
			log.Printf("[%s] recovery notification send failed: %v", dbCfg.Name, sendErr)
		} else {
			m.emit("notified", dbID, dbCfg.Name, "已发送连接恢复通知", nil)
		}
	}

	if len(queries) == 0 {
		notifier.ClearNotifiedPIDs()
		m.emit("no_queries", dbID, dbCfg.Name, "无慢查询", nil)
		return
	}

	m.emit("found_queries", dbID, dbCfg.Name, fmt.Sprintf("发现 %d 条慢查询", len(queries)), nil)

	for _, q := range queries {
		logEntry := &store.SlowQueryLog{
			DatabaseID:   dbID,
			ThreadID:     q.ThreadID,
			ProcessID:    q.ProcessID,
			User:         q.User,
			Host:         q.Host,
			DBName:       q.DB,
			SQLText:      q.SQLText,
			ExecSec:      q.ExecSec,
			LockSec:      q.LockSec,
			RowsExamined: q.RowsExam,
			RowsSent:     q.RowsSent,
			State:        q.State,
		}
		inserted, err := m.store.InsertSlowQueryLog(logEntry)
		if err != nil {
			log.Printf("[%s] insert error: %v", dbCfg.Name, err)
			continue
		}
		if inserted {
			logEntry.DatabaseName = dbCfg.Name
			logEntry.DetectedAt = time.Now()
			m.emit("slow_query", dbID, dbCfg.Name, "", logEntry)
		}
	}

	if !notifier.SyncAndShouldNotify(queries) {
		return
	}

	log.Printf("[%s] detected %d slow queries, sending notifications", dbCfg.Name, len(queries))
	message := FormatNotificationText(dbCfg.Name, dbCfg.Host, dbCfg.Port, queries)
	if err := m.dispatcher.SendNotifications(dbID, message); err != nil {
		log.Printf("[%s] notification error: %v", dbCfg.Name, err)
		m.emit("error", dbID, dbCfg.Name, fmt.Sprintf("通知发送失败: %v", err), nil)
	} else {
		m.emit("notified", dbID, dbCfg.Name, "已发送通知", nil)
	}
	notifier.MarkPIDsNotified(queries)
}
