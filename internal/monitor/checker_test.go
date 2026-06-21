package monitor

import (
	"testing"

	"ops-sentinel/internal/notify"
)

func TestFilterBuiltinIgnoredLongQueriesRemovesInspectionSQL(t *testing.T) {
	queries := []notify.LongQuery{
		{
			ProcessID: 1,
			SQLText: `SELECT
				command,
				COUNT(*) AS cnt,
				sys.format_bytes(SUM(current_memory)) AS memory
				FROM sys.processlist
				GROUP BY command
				ORDER BY SUM(current_memory) DESC`,
		},
		{
			ProcessID: 2,
			SQLText:   "SHOW ENGINE INNODB STATUS",
		},
		{
			ProcessID: 3,
			SQLText:   "UPDATE ttpos_sale_order SET status = 1 WHERE uuid = 123",
		},
	}

	filtered := filterBuiltinIgnoredLongQueries(queries)
	if len(filtered) != 1 {
		t.Fatalf("expected one business query after filtering, got %d", len(filtered))
	}
	if filtered[0].ProcessID != 3 {
		t.Fatalf("expected process 3 to remain, got %d", filtered[0].ProcessID)
	}
}
