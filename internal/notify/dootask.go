package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"mysql-monitor/internal/store"
)

var dootaskClient = &http.Client{Timeout: 30 * time.Second}

func SendDooTask(cfg store.DooTaskConfig, message string) error {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	apiURL := baseURL + "/api/dialog/msg/sendtext"

	body, err := json.Marshal(map[string]string{
		"dialog_id": cfg.DialogID,
		"text":      message,
	})
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("version", "1.6.89")
	req.Header.Set("token", cfg.Token)

	resp, err := dootaskClient.Do(req)
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

	var result struct {
		Ret  int    `json:"ret"`
		Msg  string `json:"msg"`
		Data any    `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Printf("dootask: unexpected non-JSON response: %s", string(respBody[:min(len(respBody), 200)]))
		return nil
	}
	if result.Ret != 1 {
		return fmt.Errorf("dootask api ret=%d: %s", result.Ret, result.Msg)
	}
	return nil
}
