package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ops-sentinel/internal/store"
)

func sampleCard() *AlertCard {
	return &AlertCard{
		Title: "data-01 应用日志错误增长", Level: "critical",
		Fields: [][2]string{
			{"实例", "data-01 主机指标"},
			{"当前值", "709"},
		},
		Note:       "错误持续增长",
		Code:       "[ttpos-mysql] [ERROR] Aborting",
		DetailPath: "/#/objects/7",
	}
}

func TestAlertCardPlainText(t *testing.T) {
	got := sampleCard().PlainText("https://sentinel.example.com")
	for _, want := range []string{
		"[CRITICAL] data-01 应用日志错误增长",
		"实例: data-01 主机指标",
		"查看详情: https://sentinel.example.com/#/objects/7",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PlainText 缺少 %q:\n%s", want, got)
		}
	}
}

func TestSendFeishuCardPayload(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &payload)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	if err := SendFeishuCard(store.FeishuConfig{Webhook: srv.URL}, sampleCard(), "https://sentinel.example.com"); err != nil {
		t.Fatalf("send: %v", err)
	}
	card := payload["card"].(map[string]any)
	header := card["header"].(map[string]any)
	if header["template"] != "red" {
		t.Errorf("critical 应为红头，got %v", header["template"])
	}
	title := header["title"].(map[string]any)["content"].(string)
	if !strings.HasPrefix(title, "[CRITICAL] ") {
		t.Errorf("标题应带级别前缀，got %q", title)
	}
	raw, _ := json.Marshal(card["elements"])
	for _, want := range []string{"实例", "709", "查看详情", "/#/objects/7", "Aborting"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("elements 缺少 %q", want)
		}
	}
}
