#!/bin/sh
# silo→B2 备份时效导出（utility-01）：扫描 backup-runner 的 runs 目录，
# 报告最近一次 COMPLETE 的距今小时数。备份悄悄断掉是最经典的
# "恢复时才发现"事故——8-24 之后停跑 11 天就是这么发现的。
set -u
OUT_DIR="${1:-/var/lib/node_exporter/textfile}"
OUT="$OUT_DIR/ttpos_backup.prom"
TMP="$OUT.$$"
VM=$(hostname)
RUNS=/var/lib/ttpos-silo-b2-backup/runs

ok=1
latest=$(find "$RUNS" -maxdepth 2 -name COMPLETE -printf '%T@\n' 2>/dev/null | sort -rn | head -1)
[ -z "$latest" ] && { latest=0; ok=0; }
now=$(date +%s)
age_h=$(awk -v n="$now" -v l="$latest" 'BEGIN{ if (l<=0) {print 999999} else printf "%.1f", (n-l)/3600 }')

{
  echo "# HELP ttpos_backup_last_complete_age_hours 最近一次备份 COMPLETE 距今小时数"
  echo "# TYPE ttpos_backup_last_complete_age_hours gauge"
  echo "ttpos_backup_last_complete_age_hours{vm=\"$VM\",job=\"silo-b2\"} $age_h"
  echo "# HELP ttpos_backup_metrics_ok 备份指标采集是否正常（runs 目录可读且有记录）"
  echo "# TYPE ttpos_backup_metrics_ok gauge"
  echo "ttpos_backup_metrics_ok{vm=\"$VM\"} $ok"
} > "$TMP"
mv "$TMP" "$OUT"
