package notify

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"
	"strings"
	"time"

	"mysql-monitor/internal/store"
)

func SendEmail(cfg store.EmailConfig, message string) error {
	recipients := strings.Split(cfg.To, ",")
	for i, r := range recipients {
		recipients[i] = strings.TrimSpace(r)
	}
	if len(recipients) == 0 || recipients[0] == "" {
		return fmt.Errorf("no recipients")
	}

	from := strings.TrimSpace(cfg.From)
	if from == "" {
		return fmt.Errorf("no sender")
	}

	subject := fmt.Sprintf("MySQL 慢查询告警 - %s", time.Now().Format("2006-01-02 15:04:05"))
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
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	if cfg.SMTPPort == 465 {
		return sendEmailTLS(addr, auth, from, recipients, msg)
	}
	return smtp.SendMail(addr, auth, from, recipients, msg)
}

func sendEmailTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host := strings.Split(addr, ":")[0]
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
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
	if _, err = w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}
