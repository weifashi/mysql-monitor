package monitor

import (
	"strings"
	"testing"
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
