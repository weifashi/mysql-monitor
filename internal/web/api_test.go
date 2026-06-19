package web

import (
	"testing"

	"ops-sentinel/internal/store"
)

func TestCustomSQLMetricStateResetNeeded(t *testing.T) {
	base := &store.CustomSQLCheck{
		DatabaseID:       1,
		Name:             "慢 SQL / 请求量监控",
		SQLText:          "SELECT 1 AS questions",
		ResultField:      "questions",
		IntervalSec:      30,
		TimeoutSec:       10,
		AlertStrategy:    "increase",
		Condition:        "gt",
		ExpectedValue:    "0",
		AlertDeltaValue:  "150000",
		AlertConsecutive: 1,
		AlertRules:       `[{"result_field":"questions","alert_strategy":"increase"}]`,
		NotifyEnabled:    true,
		RecoveryNotify:   true,
		MessageTemplate:  "",
		Enabled:          true,
	}

	metadataOnly := *base
	metadataOnly.Name = "请求量监控"
	metadataOnly.IntervalSec = 5
	metadataOnly.NotifyEnabled = false
	if customSQLMetricStateResetNeeded(base, &metadataOnly) {
		t.Fatalf("metadata-only changes should keep metric baseline")
	}

	sqlChanged := *base
	sqlChanged.SQLText = "SHOW GLOBAL STATUS LIKE 'Questions'"
	if !customSQLMetricStateResetNeeded(base, &sqlChanged) {
		t.Fatalf("SQL changes should reset metric baseline")
	}

	fieldChanged := *base
	fieldChanged.ResultField = "Value"
	if !customSQLMetricStateResetNeeded(base, &fieldChanged) {
		t.Fatalf("result field changes should reset metric baseline")
	}

	rulesSame := *base
	rulesSame.AlertRules = `[
		{
			"result_field": "questions",
			"alert_strategy": "increase"
		}
	]`
	if customSQLMetricStateResetNeeded(base, &rulesSame) {
		t.Fatalf("equivalent rule JSON should keep metric baseline")
	}

	rulesChanged := *base
	rulesChanged.AlertRules = `[{"result_field":"Value","alert_strategy":"increase"}]`
	if !customSQLMetricStateResetNeeded(base, &rulesChanged) {
		t.Fatalf("rule value-source changes should reset metric baseline")
	}
}
