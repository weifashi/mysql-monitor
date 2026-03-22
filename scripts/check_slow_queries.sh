#!/bin/bash
#
# 快速检查当前正在执行的慢查询
# 使用 Performance Schema 查看完整的 SQL
#

set -e

MYSQL_HOST="${MYSQL_HOST:-35.240.129.50}"
MYSQL_PORT="${MYSQL_PORT:-58888}"
MYSQL_USER="${MYSQL_USER:-dev}"
MYSQL_PASS="${MYSQL_PASS:-s7cx5CcfBanwq0LbZ9}"
THRESHOLD="${1:-10}"  # 默认 10 秒

MYSQL_CMD="mysql -h${MYSQL_HOST} -P${MYSQL_PORT} -u${MYSQL_USER} -p${MYSQL_PASS} -N -s"

echo "════════════════════════════════════════════════════════════════"
echo "  Performance Schema 慢查询检查"
echo "  阈值: ${THRESHOLD}s | 时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "════════════════════════════════════════════════════════════════"
echo

# 查询正在执行的慢查询
result=$($MYSQL_CMD performance_schema <<SQL 2>/dev/null
SELECT
  CONCAT(
    '【Thread ', ess.thread_id, ' | 连接 ', t.processlist_id, '】\n',
    '用户: ', t.processlist_user, '@', t.processlist_host, '\n',
    '数据库: ', COALESCE(t.processlist_db, 'NULL'), '\n',
    '执行时间: ', ROUND(ess.timer_wait / 1000000000000, 1), 's | ',
    '锁等待: ', ROUND(ess.lock_time / 1000000000000, 1), 's\n',
    '扫描行数: ', ess.rows_examined, ' | 返回行数: ', ess.rows_sent, '\n',
    '状态: ', COALESCE(t.processlist_state, 'NULL'), '\n',
    'SQL: ', LEFT(ess.sql_text, 200), '\n',
    '终止: KILL ', t.processlist_id, ';\n',
    '----------------------------------------'
  ) AS info
FROM performance_schema.events_statements_current ess
JOIN performance_schema.threads t
  ON ess.thread_id = t.thread_id
WHERE ess.sql_text IS NOT NULL
  AND ess.timer_wait / 1000000000000 > ${THRESHOLD}
ORDER BY ess.timer_wait DESC
LIMIT 10;
SQL
)

if [ -z "$result" ]; then
  echo "✅ 没有超过 ${THRESHOLD}s 的慢查询"
else
  echo "🚨 发现慢查询:"
  echo
  echo "$result"
fi

echo
echo "════════════════════════════════════════════════════════════════"

# 显示当前连接数统计
echo
echo "【连接数统计】"
$MYSQL_CMD information_schema <<SQL 2>/dev/null
SELECT
  CONCAT(
    'Threads_connected: ', (SELECT VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME='Threads_connected'), ' | ',
    'Threads_running: ', (SELECT VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME='Threads_running'), ' | ',
    'Queries: ', (SELECT VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME='Queries')
  ) AS stats;
SQL

echo
echo "════════════════════════════════════════════════════════════════"
