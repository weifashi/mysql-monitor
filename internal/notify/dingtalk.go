package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mysql-monitor/internal/store"
)

func SendDingTalk(cfg store.DingTalkConfig, message string) error {
	type dingMsg struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	msg := dingMsg{MsgType: "text"}
	msg.Text.Content = message

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	url := cfg.Webhook
	if cfg.Secret != "" {
		ts := time.Now().UnixMilli()
		sign := dingTalkSign(ts, cfg.Secret)
		url = fmt.Sprintf("%s&timestamp=%d&sign=%s", url, ts, sign)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status: %d", resp.StatusCode)
	}
	return nil
}

func dingTalkSign(timestamp int64, secret string) string {
	str := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(str))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
