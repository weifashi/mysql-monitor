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

func TestSanitizeFeishuBlockedSleepFunction(t *testing.T) {
	got := sanitizeFeishuBlockedText("SELECT SLEEP(13)")
	want := "SELECT SLEEP (13)"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFeishuLevelTemplate(t *testing.T) {
	cases := map[string]string{
		"critical": "red", "error": "red",
		"warning": "orange",
		"recovery": "green",
		"info": "blue", "test": "blue",
		"": "", "unknown": "",
	}
	for in, want := range cases {
		if got := feishuLevelTemplate(in); got != want {
			t.Errorf("feishuLevelTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}
