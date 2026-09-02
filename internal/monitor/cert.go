package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"ops-sentinel/internal/notify"
	"ops-sentinel/internal/store"
)

// CertManager 检查 TLS 证书到期时间。
//
// 证书过期是少数"确定会发生、且能提前几十天知道"的故障，
// 但没有监控时往往在过期当天才被用户发现。检查成本极低：
// 建立一次 TLS 握手读证书链即可，不需要发业务请求。
type CertManager struct {
	store      *store.Store
	dispatcher *notify.Dispatcher
	eventBus   *EventBus

	mu       sync.Mutex
	monitors map[int64]context.CancelFunc
}

func NewCertManager(s *store.Store, d *notify.Dispatcher, eb *EventBus) *CertManager {
	return &CertManager{
		store:      s,
		dispatcher: d,
		eventBus:   eb,
		monitors:   make(map[int64]context.CancelFunc),
	}
}

func (m *CertManager) StartAll() error {
	checks, err := m.store.ListCertChecks()
	if err != nil {
		return fmt.Errorf("list cert checks: %w", err)
	}
	for _, c := range checks {
		if c.Enabled {
			if err := m.Start(c.ID); err != nil {
				log.Printf("[cert] start check %s failed: %v", c.Name, err)
			}
		}
	}
	return nil
}

func (m *CertManager) Start(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.monitors[id]; ok {
		return nil
	}

	cfg, err := m.store.GetCertCheck(id)
	if err != nil {
		return fmt.Errorf("get cert check %d: %w", id, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.monitors[id] = cancel

	go m.runMonitor(ctx, cfg)
	log.Printf("[cert] started check %s (%s)", cfg.Name, cfg.Endpoint)
	return nil
}

func (m *CertManager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.monitors[id]; ok {
		cancel()
		delete(m.monitors, id)
	}
}

func (m *CertManager) Restart(id int64) error {
	m.Stop(id)
	return m.Start(id)
}

func (m *CertManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.monitors {
		cancel()
		delete(m.monitors, id)
	}
}

func (m *CertManager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.monitors[id]
	return ok
}

func (m *CertManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.monitors)
}

func (m *CertManager) runMonitor(ctx context.Context, cfg *store.CertCheck) {
	interval := time.Duration(cfg.IntervalSec) * time.Second
	if interval <= 0 {
		interval = time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.doCheck(cfg.ID)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[cert] stopped check %d", cfg.ID)
			return
		case <-ticker.C:
			m.doCheck(cfg.ID)
		}
	}
}

func (m *CertManager) doCheck(id int64) {
	cfg, err := m.store.GetCertCheck(id)
	if err != nil {
		log.Printf("[cert] reload check %d failed: %v", id, err)
		return
	}
	if !cfg.Enabled {
		return
	}

	result := InspectCertificate(cfg)
	prevStatus, hasPrev, _ := m.store.LastCertCheckStatus(cfg.ID)
	m.store.InsertCertCheckLog(&result)

	if m.eventBus != nil {
		m.eventBus.Publish(MonitorEvent{
			Type:       "cert_check",
			DatabaseID: cfg.ID,
			DBName:     cfg.Name,
			Message:    result.Message,
			Timestamp:  time.Now(),
			Data: map[string]interface{}{
				"status":    result.Status,
				"days_left": result.DaysLeft,
				"endpoint":  cfg.Endpoint,
			},
		})
	}

	isAlert := result.Status == "warning" || result.Status == "critical" || result.Status == "error"
	wasAlert := hasPrev && (prevStatus == "warning" || prevStatus == "critical" || prevStatus == "error")

	if !cfg.NotifyEnabled {
		return
	}

	// 证书状态变化缓慢，每天检查一次即可；只在状态跃迁时推送，避免每天重复刷屏
	if isAlert {
		sev := "warning"
		if result.Status == "critical" || result.Status == "error" {
			sev = "critical"
		}
		m.store.UpsertFiringEvent(&store.AlertEvent{
			Source: "cert", CheckID: cfg.ID, CheckName: cfg.Name,
			Title: cfg.Name, TargetID: cfg.ID, TargetName: cfg.Endpoint,
			Dimension: "cert", Severity: sev,
			Value: fmt.Sprintf("剩余 %d 天", result.DaysLeft), Message: result.Message,
		}, false)
	} else if wasAlert {
		m.store.ResolveEvent("cert", cfg.ID, fmt.Sprintf("剩余 %d 天", result.DaysLeft))
	}

	if isAlert && (!hasPrev || prevStatus != result.Status) {
		card := &notify.AlertCard{
			Title: "证书 " + cfg.Name, Level: result.Status,
			Fields: [][2]string{
				{"端点", cfg.Endpoint},
				{"剩余天数", fmt.Sprintf("%d 天", result.DaysLeft)},
				{"状态", result.Status},
				{"检查时间", time.Now().Format("2006-01-02 15:04:05")},
			},
			Note:       result.Message,
			DetailPath: "/#/cert-checks",
		}
		if err := m.dispatcher.SendGlobalAlertCard(card); err != nil {
			log.Printf("[cert] notify failed for %s: %v", cfg.Name, err)
		}
		return
	}

	if !isAlert && wasAlert {
		card := &notify.AlertCard{
			Title: "证书 " + cfg.Name, Level: "recovery",
			Fields: [][2]string{
				{"端点", cfg.Endpoint},
				{"剩余天数", fmt.Sprintf("%d 天", result.DaysLeft)},
				{"恢复时间", time.Now().Format("2006-01-02 15:04:05")},
			},
			DetailPath: "/#/cert-checks",
		}
		if err := m.dispatcher.SendGlobalAlertCard(card); err != nil {
			log.Printf("[cert] recovery notify failed for %s: %v", cfg.Name, err)
		}
	}
}

// InspectCertificate 建立一次 TLS 握手，读取叶子证书的到期时间。
// 导出以便 Web 层的"立即测试"复用同一套判定逻辑。
func InspectCertificate(cfg *store.CertCheck) store.CertCheckLog {
	logEntry := store.CertCheckLog{
		CheckID:   cfg.ID,
		CheckName: cfg.Name,
		Endpoint:  cfg.Endpoint,
	}

	endpoint := normalizeCertEndpoint(cfg.Endpoint)
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		if host, _, err := net.SplitHostPort(endpoint); err == nil {
			serverName = host
		} else {
			serverName = endpoint
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", endpoint, &tls.Config{
		ServerName: serverName,
		// 只读取证书信息，不做信任链校验 —— 自签名证书同样需要监控到期
		InsecureSkipVerify: true,
	})
	if err != nil {
		logEntry.Status = "error"
		logEntry.Error = err.Error()
		logEntry.Message = fmt.Sprintf("[CRITICAL] 证书检查失败 %s\n%s\n错误：%v",
			cfg.Name, endpoint, err)
		return logEntry
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		logEntry.Status = "error"
		logEntry.Error = "服务端未返回证书"
		logEntry.Message = fmt.Sprintf("[CRITICAL] 证书检查失败 %s\n%s\n服务端未返回证书",
			cfg.Name, endpoint)
		return logEntry
	}

	leaf := certs[0]
	notAfter := leaf.NotAfter
	logEntry.NotAfter = &notAfter
	logEntry.Issuer = leaf.Issuer.CommonName
	logEntry.Subject = leaf.Subject.CommonName

	daysLeft := int(time.Until(notAfter).Hours() / 24)
	logEntry.DaysLeft = daysLeft

	warnDays := cfg.WarnDays
	if warnDays <= 0 {
		warnDays = 30
	}
	criticalDays := cfg.CriticalDays
	if criticalDays <= 0 {
		criticalDays = 7
	}

	switch {
	case daysLeft < 0:
		logEntry.Status = "critical"
		logEntry.Message = fmt.Sprintf("[CRITICAL] 证书已过期 %s\n%s\n过期于：%s（已过期 %d 天）\n签发者：%s",
			cfg.Name, endpoint, notAfter.Format("2006-01-02 15:04"), -daysLeft, logEntry.Issuer)
	case daysLeft <= criticalDays:
		logEntry.Status = "critical"
		logEntry.Message = fmt.Sprintf("[CRITICAL] 证书即将过期 %s\n%s\n剩余 %d 天（到期 %s）\n签发者：%s",
			cfg.Name, endpoint, daysLeft, notAfter.Format("2006-01-02 15:04"), logEntry.Issuer)
	case daysLeft <= warnDays:
		logEntry.Status = "warning"
		logEntry.Message = fmt.Sprintf("[WARNING] 证书临近到期 %s\n%s\n剩余 %d 天（到期 %s）\n签发者：%s",
			cfg.Name, endpoint, daysLeft, notAfter.Format("2006-01-02 15:04"), logEntry.Issuer)
	default:
		logEntry.Status = "ok"
		logEntry.Message = fmt.Sprintf("证书正常 %s，剩余 %d 天", cfg.Name, daysLeft)
	}

	return logEntry
}

// normalizeCertEndpoint 容忍用户填 https://host/path 或 host 这类写法，
// 统一成 host:port，端口缺省 443。
func normalizeCertEndpoint(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(s, "443")
	}
	return s
}
