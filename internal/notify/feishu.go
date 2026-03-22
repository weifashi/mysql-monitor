package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mysql-monitor/internal/store"
)

func SendFeishu(cfg store.FeishuConfig, message string) error {
	type fsBody struct {
		Timestamp string `json:"timestamp,omitempty"`
		Sign      string `json:"sign,omitempty"`
		MsgType   string `json:"msg_type"`
		Content   struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	body := fsBody{MsgType: "text"}
	body.Content.Text = message

	if strings.TrimSpace(cfg.Secret) != "" {
		sec := time.Now().Unix()
		body.Timestamp = fmt.Sprintf("%d", sec)
		body.Sign = feishuSign(sec, cfg.Secret)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	resp, err := http.Post(cfg.Webhook, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status: %d, body: %s", resp.StatusCode, string(respBody))
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
		return fmt.Errorf("parse feishu response: %w, body: %s", err, string(respBody))
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
		return fmt.Errorf("feishu api code=%d: %s", apiCode, errMsg)
	}
	return nil
}

func feishuSign(timestampSec int64, secret string) string {
	str := fmt.Sprintf("%d\n%s", timestampSec, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(str))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
