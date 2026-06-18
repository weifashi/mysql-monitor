package monitor

import (
	"strings"
	"testing"

	"ops-sentinel/internal/store"
)

func TestValidateCustomSQLRejectsUnsafeStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "write operation", sql: "UPDATE orders SET status = 1", want: "只允许"},
		{name: "sleep", sql: "SELECT SLEEP(10)", want: "SLEEP"},
		{name: "select star", sql: "SELECT * FROM orders LIMIT 1", want: "SELECT *"},
		{name: "full scan select", sql: "SELECT id, name FROM orders", want: "必须带 LIMIT"},
		{name: "outfile", sql: "SELECT id FROM orders LIMIT 1 INTO OUTFILE '/tmp/a'", want: "INTO OUTFILE"},
		{name: "for update", sql: "SELECT id FROM orders WHERE id = 1 FOR UPDATE", want: "FOR UPDATE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCustomSQL(tt.sql)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q in %q", tt.want, err.Error())
			}
		})
	}
}

func TestValidateCustomSQLAllowsSafeMonitoringQueries(t *testing.T) {
	tests := []string{
		"SELECT COUNT(*) AS process_count FROM information_schema.PROCESSLIST",
		"SELECT id, name FROM orders LIMIT 1",
		"SHOW STATUS LIKE 'Threads_connected'",
		"EXPLAIN SELECT id FROM orders WHERE id = 1",
	}
	for _, sql := range tests {
		t.Run(sql, func(t *testing.T) {
			if err := ValidateCustomSQL(sql); err != nil {
				t.Fatalf("expected valid SQL, got %v", err)
			}
		})
	}
}

func TestCustomSQLAlertRuleAcceptsFrontendFieldNames(t *testing.T) {
	cfg := &store.CustomSQLCheck{
		Name: "lock waits",
		AlertRules: `[
			{
				"name": "MySQL 行锁等待次数突增",
				"result_field": "innodb_row_lock_waits",
				"alert_strategy": "increase",
				"condition": "gt",
				"expected_value": "",
				"alert_delta_value": "500"
			}
		]`,
	}

	rules := customSQLAlertRulesFromConfig(cfg)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Strategy != "increase" {
		t.Fatalf("expected strategy increase, got %q", rules[0].Strategy)
	}
	if rules[0].DeltaValue != "500" {
		t.Fatalf("expected delta value 500, got %q", rules[0].DeltaValue)
	}

	ruleCfg := customSQLCheckForAlertRule(cfg, rules[0])
	state := &healthMetricState{Field: ruleCfg.ResultField}

	matched, reason := EvaluateCustomSQLRule("1360602", ruleCfg, state)
	if matched {
		t.Fatalf("first sample should only seed state, got alert: %s", reason)
	}

	matched, reason = EvaluateCustomSQLRule("1361000", ruleCfg, state)
	if matched {
		t.Fatalf("delta below 500 should not alert: %s", reason)
	}
	if !strings.Contains(reason, "变化 398") {
		t.Fatalf("expected increase reason with delta, got %q", reason)
	}

	matched, reason = EvaluateCustomSQLRule("1361601", ruleCfg, state)
	if !matched {
		t.Fatalf("delta >= 500 should alert: %s", reason)
	}
	if !strings.Contains(reason, "变化量阈值 500") {
		t.Fatalf("expected delta threshold in reason, got %q", reason)
	}
}
