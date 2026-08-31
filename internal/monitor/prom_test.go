package monitor

import (
	"math"
	"testing"

	"ops-sentinel/internal/store"
)

func TestParsePromText(t *testing.T) {
	body := `# HELP node_memory_SwapFree_bytes Memory information.
# TYPE node_memory_SwapFree_bytes gauge
node_memory_SwapFree_bytes 1.2247424e+07
node_memory_SwapTotal_bytes 1.7716736e+07
node_filesystem_avail_bytes{device="/dev/sda2",mountpoint="/"} 3.3e+10
node_filesystem_avail_bytes{device="/dev/sdb",mountpoint="/mnt/data"} 1.9e+11
http_requests_total{method="GET",path="/api/v1",status="200"} 1234
http_requests_total{method="GET",path="/api/v1",status="500"} 7
# 带时间戳的样本，时间戳应被忽略
process_open_fds 512 1717171717000
`

	families := parsePromText(body)

	if len(families) != 5 {
		t.Fatalf("expected 5 metric families, got %d", len(families))
	}

	// 无标签指标
	if got := families["node_memory_SwapFree_bytes"]; len(got) != 1 || got[0].Value != 1.2247424e+07 {
		t.Errorf("swap free parsed wrong: %+v", got)
	}

	// 多样本 + 标签
	fs := families["node_filesystem_avail_bytes"]
	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystem samples, got %d", len(fs))
	}
	if fs[0].Labels["mountpoint"] != "/" || fs[1].Labels["device"] != "/dev/sdb" {
		t.Errorf("filesystem labels parsed wrong: %+v", fs)
	}

	// 时间戳应被忽略，取第一段作为值
	if got := families["process_open_fds"]; len(got) != 1 || got[0].Value != 512 {
		t.Errorf("timestamp should be ignored: %+v", got)
	}
}

func TestParsePromTextEdgeCases(t *testing.T) {
	body := `metric_with_comma{msg="a,b",other="c"} 1
metric_inf +Inf
metric_nan NaN
`
	families := parsePromText(body)

	// 引号内的逗号不能被当成标签分隔符
	s := families["metric_with_comma"]
	if len(s) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(s))
	}
	if s[0].Labels["msg"] != "a,b" || s[0].Labels["other"] != "c" {
		t.Errorf("quoted comma mishandled: %+v", s[0].Labels)
	}

	if v := families["metric_inf"]; len(v) != 1 || !math.IsInf(v[0].Value, 1) {
		t.Errorf("+Inf not parsed: %+v", v)
	}
	if v := families["metric_nan"]; len(v) != 1 || !math.IsNaN(v[0].Value) {
		t.Errorf("NaN not parsed: %+v", v)
	}
}

func TestAggregateMetric(t *testing.T) {
	families := map[string][]promSample{
		"m": {
			{Labels: map[string]string{"d": "a"}, Value: 10},
			{Labels: map[string]string{"d": "b"}, Value: 30},
			{Labels: map[string]string{"d": "c"}, Value: 20},
		},
	}

	cases := []struct {
		aggregate string
		want      float64
	}{
		{"sum", 60},
		{"avg", 20},
		{"max", 30},
		{"min", 10},
		{"count", 3},
		{"last", 20},
		{"", 20}, // 默认 last
	}

	for _, c := range cases {
		got, err := aggregateMetric("m", "", c.aggregate, families)
		if err != nil {
			t.Fatalf("aggregate %q failed: %v", c.aggregate, err)
		}
		if got != c.want {
			t.Errorf("aggregate %q = %v, want %v", c.aggregate, got, c.want)
		}
	}

	// 标签过滤
	got, err := aggregateMetric("m", `d="b"`, "sum", families)
	if err != nil || got != 30 {
		t.Errorf("label filter failed: got %v err %v", got, err)
	}

	// 前缀匹配，容器名带随机后缀时用
	got, err = aggregateMetric("m", `d="a*"`, "sum", families)
	if err != nil || got != 10 {
		t.Errorf("prefix filter failed: got %v err %v", got, err)
	}

	// 指标不存在应报错而不是返回 0，否则会被误判成"低于阈值"
	if _, err := aggregateMetric("nonexistent", "", "sum", families); err == nil {
		t.Error("missing metric should return error, not zero")
	}

	// 过滤后无样本同样应报错
	if _, err := aggregateMetric("m", `d="zzz"`, "sum", families); err == nil {
		t.Error("no matching sample should return error")
	}
}

func TestComputePromValueRatio(t *testing.T) {
	families := map[string][]promSample{
		"used":  {{Labels: map[string]string{}, Value: 25}},
		"total": {{Labels: map[string]string{}, Value: 100}},
		"free":  {{Labels: map[string]string{}, Value: 25}},
	}

	// ratio：used/total 以百分比返回
	v, err := computePromValue(&store.PromCheck{
		Metric: "used", ExprKind: "ratio", ExprDenominator: "total",
	}, families)
	if err != nil || v != 25 {
		t.Errorf("ratio = %v (err %v), want 25", v, err)
	}

	// available_ratio：free/total 翻转成使用率
	v, err = computePromValue(&store.PromCheck{
		Metric: "free", ExprKind: "available_ratio", ExprDenominator: "total",
	}, families)
	if err != nil || v != 75 {
		t.Errorf("available_ratio = %v (err %v), want 75", v, err)
	}

	// raw
	v, err = computePromValue(&store.PromCheck{Metric: "used", ExprKind: "raw"}, families)
	if err != nil || v != 25 {
		t.Errorf("raw = %v (err %v), want 25", v, err)
	}

	// 分母为 0 必须报错，不能产生 Inf 后被阈值误判
	zero := map[string][]promSample{
		"a": {{Labels: map[string]string{}, Value: 1}},
		"b": {{Labels: map[string]string{}, Value: 0}},
	}
	if _, err := computePromValue(&store.PromCheck{
		Metric: "a", ExprKind: "ratio", ExprDenominator: "b",
	}, zero); err == nil {
		t.Error("zero denominator should return error")
	}
}

func TestEvaluatePromCondition(t *testing.T) {
	cases := []struct {
		value     float64
		condition string
		threshold string
		want      bool
	}{
		{10, "gt", "5", true},
		{10, "gt", "10", false},
		{10, "gte", "10", true},
		{10, "lt", "20", true},
		{10, "lte", "10", true},
		{10, "eq", "10", true},
		{10, "ne", "5", true},
		{10, "", "5", true}, // 默认 gt
	}

	for _, c := range cases {
		got, _ := evaluatePromCondition(c.value, c.condition, c.threshold)
		if got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.value, c.condition, c.threshold, got, c.want)
		}
	}

	// 阈值缺失或非数字时不应命中，否则会误报
	if got, _ := evaluatePromCondition(10, "gt", ""); got {
		t.Error("empty threshold should not match")
	}
	if got, _ := evaluatePromCondition(10, "gt", "abc"); got {
		t.Error("non-numeric threshold should not match")
	}
}

func TestEvaluatePromRuleSustained(t *testing.T) {
	check := &store.PromCheck{
		AlertStrategy: "sustained", AlertCondition: "gt",
		AlertValue: "5", AlertConsecutive: 3,
	}
	st := &promMetricState{}

	// 前两次越界但未达连续次数，不应告警 —— 这是过滤抖动的关键
	for i := 1; i <= 2; i++ {
		if matched, _ := evaluatePromRule(10, check, st); matched {
			t.Fatalf("should not alert at attempt %d", i)
		}
	}
	if matched, _ := evaluatePromRule(10, check, st); !matched {
		t.Error("should alert at 3rd consecutive breach")
	}

	// 一次回落即清零，重新计数
	if matched, _ := evaluatePromRule(1, check, st); matched {
		t.Error("should not alert when value recovers")
	}
	if st.ConsecutiveMatched != 0 {
		t.Errorf("counter should reset, got %d", st.ConsecutiveMatched)
	}
}

func TestEvaluatePromRuleIncrease(t *testing.T) {
	check := &store.PromCheck{
		AlertStrategy: "increase", AlertDeltaValue: "100",
	}
	st := &promMetricState{}

	// 首次采集只建立基线，不能告警
	if matched, _ := evaluatePromRule(1000, check, st); matched {
		t.Error("first sample should not alert")
	}
	st.LastValue = 1000
	st.HasLast = true

	if matched, _ := evaluatePromRule(1050, check, st); matched {
		t.Error("delta 50 should not alert with threshold 100")
	}
	if matched, _ := evaluatePromRule(1200, check, st); !matched {
		t.Error("delta 200 should alert with threshold 100")
	}

	// 变化率
	pct := &store.PromCheck{AlertStrategy: "increase", AlertDeltaPercent: "50"}
	st2 := &promMetricState{LastValue: 100, HasLast: true}
	if matched, _ := evaluatePromRule(200, pct, st2); !matched {
		t.Error("100% increase should alert with 50% threshold")
	}
}

func TestNormalizeCertEndpoint(t *testing.T) {
	cases := map[string]string{
		"example.com":                    "example.com:443",
		"example.com:8443":               "example.com:8443",
		"https://example.com":            "example.com:443",
		"https://example.com/path?a=b":   "example.com:443",
		"http://example.com:8080/health": "example.com:8080",
	}
	for in, want := range cases {
		if got := normalizeCertEndpoint(in); got != want {
			t.Errorf("normalizeCertEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseJSONStringMap(t *testing.T) {
	if got := parseJSONStringMap(`{"Authorization":"Bearer x"}`); got["Authorization"] != "Bearer x" {
		t.Errorf("valid json parsed wrong: %+v", got)
	}
	// 非法配置不应让采集失败，返回空即可
	if got := parseJSONStringMap("not json"); got != nil {
		t.Errorf("invalid json should return nil, got %+v", got)
	}
	if got := parseJSONStringMap("{}"); got != nil {
		t.Errorf("empty object should return nil, got %+v", got)
	}
}
