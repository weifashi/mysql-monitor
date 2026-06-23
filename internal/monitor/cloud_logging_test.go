package monitor

import (
	"strings"
	"testing"
	"time"
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
