package monitor

import (
	"strings"
	"testing"
	"time"

	"ops-sentinel/internal/store"
)

func TestFinalizeCloudLoggingStatsBuildsPeakEndpointContributions(t *testing.T) {
	base := time.Date(2026, 6, 23, 15, 9, 49, 0, time.UTC)
	requests := []cloudLoggingConcurrencyRequest{
		{start: base.Add(-3 * time.Second), end: base.Add(2 * time.Second), endpoint: "POST /api/v1/cashier/usb/printer/report"},
		{start: base.Add(-2 * time.Second), end: base.Add(3 * time.Second), endpoint: "POST /api/v1/cashier/usb/printer/report"},
		{start: base.Add(-1 * time.Second), end: base.Add(2 * time.Second), endpoint: "GET /api/v1/cashier/call/unprocessed"},
		{start: base, end: base.Add(time.Second), endpoint: "GET /api/v1/cashier/product/list"},
		{start: base.Add(2 * time.Second), end: base.Add(4 * time.Second), endpoint: "GET /api/v1/kitchen/call/unprocessed"},
	}
	events := make([]cloudLoggingConcurrencyEvent, 0, len(requests)*2)
	for _, req := range requests {
		events = append(events,
			cloudLoggingConcurrencyEvent{at: req.start, delta: 1},
			cloudLoggingConcurrencyEvent{at: req.end, delta: -1},
		)
	}
	stats := &CloudLoggingQueryStats{}

	finalizeCloudLoggingStats(stats, events, requests, 0)

	if stats.PeakConcurrency != 4 {
		t.Fatalf("expected peak concurrency 4, got %d", stats.PeakConcurrency)
	}
	if got := len(stats.PeakEndpointContributions); got != 3 {
		t.Fatalf("expected 3 active endpoint rows, got %d", got)
	}
	gotActiveTotal := 0
	for _, row := range stats.PeakEndpointContributions {
		gotActiveTotal += row.ActiveAtPeak
	}
	if gotActiveTotal != stats.PeakConcurrency {
		t.Fatalf("expected contribution total %d, got %d", stats.PeakConcurrency, gotActiveTotal)
	}
	first := stats.PeakEndpointContributions[0]
	if first.Endpoint != "POST /api/v1/cashier/usb/printer/report" || first.ActiveAtPeak != 2 || first.RequestCount != 2 {
		t.Fatalf("unexpected first contribution row: %+v", first)
	}
}

func TestCloudLoggingPeakEndpointNotificationUsesRequestedColumns(t *testing.T) {
	stats := &CloudLoggingQueryStats{
		PeakAt: "2026-06-23T15:09:49Z",
		PeakEndpointContributions: []CloudLoggingPeakEndpointContribution{
			{Endpoint: "POST /api/v1/cashier/usb/printer/report", ActiveAtPeak: 433, RequestCount: 3579},
		},
	}

	got := cloudLoggingPeakEndpointNotification(stats, 5)

	for _, want := range []string{
		"峰值时间: 2026-06-23T15:09:49Z",
		"接口 | 峰值那一刻活跃并发 | 5分钟请求数",
		"POST /api/v1/cashier/usb/printer/report | 433 | 3579",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected notification to contain %q, got:\n%s", want, got)
		}
	}
}

func TestCloudLoggingPeakAlertNotificationIncludesSampleSummary(t *testing.T) {
	cfg := &store.CloudLoggingConfig{Name: "diyl-407103"}
	check := &store.CloudLoggingCheck{
		Name:            "接口最大并发",
		MetricType:      store.CloudLoggingMetricPeakConcurrency,
		LookbackMinutes: 5,
		ThresholdCount:  100,
	}
	result := &CloudLoggingQueryResult{
		ResourceNames: []string{"projects/diyl-407103"},
		Stats: &CloudLoggingQueryStats{
			PeakAt: "2026-06-26T12:44:44Z",
			PeakEndpointContributions: []CloudLoggingPeakEndpointContribution{
				{Endpoint: "POST /api/v1/shop/product_bom/card/add", ActiveAtPeak: 357, RequestCount: 370},
			},
		},
		Entries: []CloudLoggingEntry{
			{
				Timestamp:    "2026-06-26T12:07:12.929000508Z",
				LogName:      "projects/diyl-407103/logs/main_log",
				ResourceType: "gce_instance",
				ResourceLabels: map[string]string{
					"instance_id": "6160442037199944805",
					"project_id":  "diyl-407103",
					"zone":        "asia-southeast1-a",
				},
				Payload: `{"msg":"HandleAddSalesVolume process, AddActualSaleNum failed","error":"Error 1213 (40001): Deadlock found when trying to get lock"}`,
			},
		},
	}

	got := cloudLoggingAlertNotificationMessage(cfg, check, result, 387, "接口最大并发")

	for _, want := range []string{
		"峰值接口贡献:",
		"POST /api/v1/shop/product_bom/card/add | 357 | 370",
		"样例摘要:",
		"projects/diyl-407103/logs/main_log",
		"gce_instance",
		"HandleAddSalesVolume process, AddActualSaleNum failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected notification to contain %q, got:\n%s", want, got)
		}
	}
}
