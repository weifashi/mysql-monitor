package notify

import "testing"

func TestFeishuTemplatePrefersAlertTitle(t *testing.T) {
	got := feishuTemplate("服务异常告警", "该告警仅发送一次，恢复后如再次异常将重新通知。")
	if got != "red" {
		t.Fatalf("expected red alert template, got %q", got)
	}
}

func TestFeishuTemplateUsesGreenForRecoveryTitle(t *testing.T) {
	got := feishuTemplate("服务恢复通知", "服务: web1\n状态: 已恢复正常")
	if got != "green" {
		t.Fatalf("expected green recovery template, got %q", got)
	}
}
