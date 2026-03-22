package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/smtp"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// 常量定义
const (
	databaseName             = "performance_schema"
	maxSlowQueries           = 20
	consoleMaxSQLLength      = 200
	notificationMaxSQLLength = 150
	httpContentType          = "application/json"
	dingTalkMsgType          = "text"
)

// 配置
var (
	host      = flag.String("host", "localhost", "MySQL 主机")
	port      = flag.Int("port", 3306, "MySQL 端口")
	user      = flag.String("user", "", "MySQL 用户")
	password  = flag.String("password", "", "MySQL 密码")
	interval  = flag.Int("interval", 10, "监控间隔（秒）")
	threshold = flag.Int("threshold", 10, "慢查询阈值（秒）")

	// 通知配置
	dingTalkWebhook = flag.String("dingtalk-webhook", "", "钉钉机器人 Webhook URL")
	dingTalkSecret  = flag.String("dingtalk-secret", "", "钉钉机器人签名密钥")
	feishuWebhook   = flag.String("feishu-webhook", "", "飞书机器人 Webhook URL")
	feishuSecret    = flag.String("feishu-secret", "", "飞书机器人签名校验密钥（可选）")
	emailEnabled    = flag.Bool("email-enabled", false, "启用邮件通知")
	emailFrom       = flag.String("email-from", "", "发件人邮箱")
	emailTo         = flag.String("email-to", "", "收件人邮箱（逗号分隔）")
	emailSMTPHost   = flag.String("email-smtp-host", "", "SMTP 服务器地址")
	emailSMTPPort   = flag.Int("email-smtp-port", 587, "SMTP 端口")
	emailUsername   = flag.String("email-username", "", "SMTP 用户名")
	emailPassword   = flag.String("email-password", "", "SMTP 密码")
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

// 通知器：同一 processlist_id（KILL 目标）仅通知一次，直至该连接不再出现在慢查询列表中
type Notifier struct {
	mu           sync.Mutex
	notifiedPIDs map[uint64]struct{}
}

var notifier = &Notifier{}

func main() {
	flag.Parse()

	// 从环境变量读取配置（优先级高于命令行参数）
	loadEnvConfig()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		*user, *password, *host, *port, databaseName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Printf("❌ 连接失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		fmt.Printf("❌ Ping 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║         Performance Schema 实时监控工具                         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\n监控间隔: %ds | 慢查询阈值: %ds | Ctrl+C 退出\n", *interval, *threshold)
	printNotifyConfig()
	fmt.Println()

	// 捕获中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(*interval) * time.Second)
	defer ticker.Stop()

	// 首次执行
	checkLongQueries(db)

	for {
		select {
		case <-ticker.C:
			checkLongQueries(db)
		case <-sigChan:
			fmt.Println("\n\n监控已停止")
			return
		}
	}
}

func loadEnvConfig() {
	// 数据库配置
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		*host = v
	}
	if v := os.Getenv("MYSQL_PORT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", port); err != nil {
			fmt.Printf("⚠️  MYSQL_PORT 格式错误: %v，使用默认值\n", err)
		}
	}
	if v := os.Getenv("MYSQL_USER"); v != "" {
		*user = v
	}
	if v := os.Getenv("MYSQL_PASSWORD"); v != "" {
		*password = v
	}
	if v := os.Getenv("MONITOR_INTERVAL"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", interval); err != nil {
			fmt.Printf("⚠️  MONITOR_INTERVAL 格式错误: %v，使用默认值\n", err)
		}
	}
	if v := os.Getenv("MONITOR_THRESHOLD"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", threshold); err != nil {
			fmt.Printf("⚠️  MONITOR_THRESHOLD 格式错误: %v，使用默认值\n", err)
		}
	}

	// 通知配置
	if v := os.Getenv("DINGTALK_WEBHOOK"); v != "" {
		*dingTalkWebhook = v
	}
	if v := os.Getenv("DINGTALK_SECRET"); v != "" {
		*dingTalkSecret = v
	}
	if v := os.Getenv("FEISHU_WEBHOOK"); v != "" {
		*feishuWebhook = v
	}
	if v := os.Getenv("FEISHU_SECRET"); v != "" {
		*feishuSecret = v
	}
	if v := os.Getenv("EMAIL_ENABLED"); v == "true" || v == "1" {
		*emailEnabled = true
	}
	if v := os.Getenv("EMAIL_FROM"); v != "" {
		*emailFrom = v
	}
	if v := os.Getenv("EMAIL_TO"); v != "" {
		*emailTo = v
	}
	if v := os.Getenv("EMAIL_SMTP_HOST"); v != "" {
		*emailSMTPHost = v
	}
	if v := os.Getenv("EMAIL_SMTP_PORT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", emailSMTPPort); err != nil {
			fmt.Printf("⚠️  EMAIL_SMTP_PORT 格式错误: %v，使用默认值\n", err)
		}
	}
	if v := os.Getenv("EMAIL_USERNAME"); v != "" {
		*emailUsername = v
	}
	if v := os.Getenv("EMAIL_PASSWORD"); v != "" {
		*emailPassword = v
	}
}

func printNotifyConfig() {
	var notifyMethods []string
	if *dingTalkWebhook != "" {
		notifyMethods = append(notifyMethods, "钉钉")
	}
	if *feishuWebhook != "" {
		notifyMethods = append(notifyMethods, "飞书")
	}
	if *emailEnabled && *emailFrom != "" && *emailTo != "" {
		notifyMethods = append(notifyMethods, "邮件")
	}
	if len(notifyMethods) > 0 {
		fmt.Printf("通知方式: %s | 去重: 同一连接(KILL id)仅告警一次\n", strings.Join(notifyMethods, "、"))
	} else {
		fmt.Println("通知方式: 未配置（仅控制台输出）")
	}
}

func checkLongQueries(db *sql.DB) {
	queries, err := getLongQueries(db, float64(*threshold))
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
		return
	}

	fmt.Printf("════════════ %s ════════════\n", time.Now().Format("2006-01-02 15:04:05"))

	if len(queries) == 0 {
		notifier.clearNotifiedPIDs()
		fmt.Printf("✅ 没有超过 %ds 的慢查询\n\n", *threshold)
		return
	}

	shouldNotify := notifier.syncAndShouldNotify(queries)

	fmt.Printf("🚨 发现 %d 个慢查询:\n\n", len(queries))

	var slowQueryText strings.Builder
	slowQueryText.WriteString(fmt.Sprintf("🚨 数据库慢查询告警\n\n"))
	slowQueryText.WriteString(fmt.Sprintf("数据库: %s:%d\n", *host, *port))
	slowQueryText.WriteString(fmt.Sprintf("时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	slowQueryText.WriteString(fmt.Sprintf("慢查询数量: %d\n\n", len(queries)))

	for i, q := range queries {
		// 控制台输出
		fmt.Printf("【慢查询 #%d】\n", i+1)
		fmt.Printf("  线程ID: %d | 连接ID: %d\n", q.ThreadID, q.ProcessID)
		fmt.Printf("  用户: %s@%s | 数据库: %s\n", q.User, q.Host, q.DB)
		fmt.Printf("  执行时间: %.1fs | 锁等待: %.1fs\n", q.ExecSec, q.LockSec)
		fmt.Printf("  扫描行数: %d | 返回行数: %d\n", q.RowsExam, q.RowsSent)
		fmt.Printf("  状态: %s\n", q.State)
		fmt.Printf("  SQL: %s\n", truncateSQL(q.SQLText, consoleMaxSQLLength))
		fmt.Printf("  终止命令: KILL %d;\n", q.ProcessID)
		fmt.Println(strings.Repeat("-", 80))

		// 通知文本
		slowQueryText.WriteString(fmt.Sprintf("【慢查询 #%d】\n", i+1))
		slowQueryText.WriteString(fmt.Sprintf("线程ID: %d | 连接ID: %d\n", q.ThreadID, q.ProcessID))
		slowQueryText.WriteString(fmt.Sprintf("用户: %s@%s | 数据库: %s\n", q.User, q.Host, q.DB))
		slowQueryText.WriteString(fmt.Sprintf("执行时间: %.1fs | 锁等待: %.1fs\n", q.ExecSec, q.LockSec))
		slowQueryText.WriteString(fmt.Sprintf("扫描行数: %d | 返回行数: %d\n", q.RowsExam, q.RowsSent))
		slowQueryText.WriteString(fmt.Sprintf("状态: %s\n", q.State))
		slowQueryText.WriteString(fmt.Sprintf("SQL: %s\n", truncateSQL(q.SQLText, notificationMaxSQLLength)))
		slowQueryText.WriteString(fmt.Sprintf("终止: KILL %d;\n\n", q.ProcessID))
	}

	fmt.Println()

	if !shouldNotify {
		fmt.Println("⏭  均为已告警过的连接（相同 KILL id），跳过外部通知")
		return
	}

	sendNotification(slowQueryText.String(), queries)
}

func (n *Notifier) clearNotifiedPIDs() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notifiedPIDs = nil
}

// 清理已结束的连接；若存在尚未告警过的 processlist_id 则返回 true
func (n *Notifier) syncAndShouldNotify(queries []LongQuery) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.notifiedPIDs == nil {
		n.notifiedPIDs = make(map[uint64]struct{})
	}

	current := make(map[uint64]struct{}, len(queries))
	for _, q := range queries {
		current[q.ProcessID] = struct{}{}
	}
	for pid := range n.notifiedPIDs {
		if _, ok := current[pid]; !ok {
			delete(n.notifiedPIDs, pid)
		}
	}
	for _, q := range queries {
		if _, ok := n.notifiedPIDs[q.ProcessID]; !ok {
			return true
		}
	}
	return false
}

func (n *Notifier) markPIDsNotified(queries []LongQuery) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.notifiedPIDs == nil {
		n.notifiedPIDs = make(map[uint64]struct{})
	}
	for _, q := range queries {
		n.notifiedPIDs[q.ProcessID] = struct{}{}
	}
}

func sendNotification(message string, queries []LongQuery) {
	var notified bool

	// 发送钉钉通知
	if *dingTalkWebhook != "" {
		if err := sendDingTalkNotification(message); err != nil {
			fmt.Printf("❌ 钉钉通知发送失败: %v\n", err)
		} else {
			fmt.Println("✅ 钉钉通知已发送")
			notified = true
		}
	}

	// 发送飞书通知
	if *feishuWebhook != "" {
		if err := sendFeishuNotification(message); err != nil {
			fmt.Printf("❌ 飞书通知发送失败: %v\n", err)
		} else {
			fmt.Println("✅ 飞书通知已发送")
			notified = true
		}
	}

	// 发送邮件通知
	if *emailEnabled && *emailFrom != "" && *emailTo != "" {
		if err := sendEmailNotification(message); err != nil {
			fmt.Printf("❌ 邮件通知发送失败: %v\n", err)
		} else {
			fmt.Println("✅ 邮件通知已发送")
			notified = true
		}
	}

	if notified {
		notifier.markPIDsNotified(queries)
	}
}

// 钉钉通知
func sendDingTalkNotification(message string) error {
	type DingTalkMessage struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
	}

	msg := DingTalkMessage{
		MsgType: "text",
	}
	msg.Text.Content = message

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	// 如果配置了签名密钥，添加签名
	webhookURL := *dingTalkWebhook
	if *dingTalkSecret != "" {
		timestamp := time.Now().UnixMilli()
		sign := generateDingTalkSign(timestamp, *dingTalkSecret)
		webhookURL = fmt.Sprintf("%s&timestamp=%d&sign=%s", webhookURL, timestamp, sign)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP 状态码: %d", resp.StatusCode)
	}

	return nil
}

// 飞书自定义机器人（Webhook v2）文本消息；签名为秒级时间戳 + "\\n" + secret 的 HMAC-SHA256 Base64
func sendFeishuNotification(message string) error {
	type fsContent struct {
		Text string `json:"text"`
	}
	type fsBody struct {
		Timestamp string    `json:"timestamp,omitempty"`
		Sign      string    `json:"sign,omitempty"`
		MsgType   string    `json:"msg_type"`
		Content   fsContent `json:"content"`
	}

	sec := time.Now().Unix()
	body := fsBody{
		MsgType: "text",
		Content: fsContent{Text: message},
	}
	if strings.TrimSpace(*feishuSecret) != "" {
		body.Timestamp = fmt.Sprintf("%d", sec)
		body.Sign = generateFeishuSign(sec, *feishuSecret)
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	resp, err := http.Post(*feishuWebhook, httpContentType, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP 状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
	}

	var fr struct {
		Code          int    `json:"code"`
		StatusCode    int    `json:"StatusCode"`
		Msg           string `json:"msg"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(respBody, &fr); err != nil {
		if len(bytes.TrimSpace(respBody)) == 0 {
			return nil
		}
		return fmt.Errorf("飞书响应解析失败: %w, 正文: %s", err, string(respBody))
	}
	apiCode := fr.Code
	if apiCode == 0 {
		apiCode = fr.StatusCode
	}
	if apiCode != 0 {
		errMsg := fr.Msg
		if errMsg == "" {
			errMsg = fr.StatusMessage
		}
		return fmt.Errorf("飞书 API code=%d: %s", apiCode, errMsg)
	}
	return nil
}

func generateFeishuSign(timestampSec int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestampSec, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// 生成钉钉签名
func generateDingTalkSign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// 邮件通知（信头需符合 RFC5322；QQ 邮箱会校验 From，缺省会报 550）
func sendEmailNotification(message string) error {
	recipients := strings.Split(*emailTo, ",")
	for i, r := range recipients {
		recipients[i] = strings.TrimSpace(r)
	}
	if len(recipients) == 0 || recipients[0] == "" {
		return fmt.Errorf("收件人 EMAIL_TO 为空")
	}

	from := strings.TrimSpace(*emailFrom)
	if from == "" {
		return fmt.Errorf("发件人 EMAIL_FROM 为空")
	}

	subject := fmt.Sprintf("🚨 MySQL 慢查询告警 - %s", time.Now().Format("2006-01-02 15:04:05"))
	subjectHdr := mime.QEncoding.Encode("utf-8", subject)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(recipients, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subjectHdr)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&buf, "\r\n")
	buf.WriteString(message)

	msg := buf.Bytes()
	auth := smtp.PlainAuth("", *emailUsername, *emailPassword, *emailSMTPHost)

	addr := fmt.Sprintf("%s:%d", *emailSMTPHost, *emailSMTPPort)
	if *emailSMTPPort == 465 {
		return sendEmailWithTLS(addr, auth, from, recipients, msg)
	}

	return smtp.SendMail(addr, auth, from, recipients, msg)
}

// 使用 TLS 发送邮件（端口 465）
func sendEmailWithTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	// 简化实现：使用标准 smtp.SendMail
	// 实际生产环境建议使用专门的邮件库如 gomail
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName: strings.Split(addr, ":")[0],
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, strings.Split(addr, ":")[0])
	if err != nil {
		return err
	}
	defer client.Quit()

	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}

	if err = client.Mail(from); err != nil {
		return err
	}

	for _, addr := range to {
		if err = client.Rcpt(addr); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}

	_, err = w.Write(msg)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return client.Quit()
}

func getLongQueries(db *sql.DB, thresholdSec float64) ([]LongQuery, error) {
	query := `
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
	`
	query = fmt.Sprintf(query, maxSlowQueries)

	rows, err := db.Query(query, thresholdSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []LongQuery
	for rows.Next() {
		var q LongQuery
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

	return queries, nil
}

func truncateSQL(sql string, maxLen int) string {
	// 移除多余的空白字符
	sql = strings.Join(strings.Fields(sql), " ")

	if len(sql) <= maxLen {
		return sql
	}
	return sql[:maxLen] + "..."
}
