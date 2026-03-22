# Performance Schema 启用和使用指南

## ✅ 当前状态

- **Performance Schema**: ✅ 已启用
- **events_statements_current**: ✅ 已启用（查看正在执行的 SQL）
- **events_statements_history**: ✅ 已启用（每个线程最近 10 条 SQL）
- **events_statements_history_long**: ❌ 未启用（可选，最近 10000 条 SQL）

---

## 🚀 快速开始（3 种使用方式）

### 方式 1: 快速检查（Shell 脚本）⭐ 推荐

最简单的方式，一行命令即可：

```bash
# 检查超过 10 秒的慢查询
./scripts/check_slow_queries.sh

# 检查超过 5 秒的慢查询
./scripts/check_slow_queries.sh 5

# 检查超过 30 秒的慢查询
./scripts/check_slow_queries.sh 30
```

**输出示例**：
```
════════════════════════════════════════════════════════════════
  Performance Schema 慢查询检查
  阈值: 10s | 时间: 2026-03-21 10:30:45
════════════════════════════════════════════════════════════════

🚨 发现慢查询:

【Thread 12345 | 连接 67890】
用户: dev@10.148.15.195
数据库: shop8267304538112000
执行时间: 28.5s | 锁等待: 0.1s
扫描行数: 1234567 | 返回行数: 100
状态: Sending data
SQL: SELECT * FROM ttpos_order WHERE create_time > 1234567890 ...
终止: KILL 67890;
```

---

### 方式 2: 实时监控（Go 程序）

持续监控，每 10 秒检查一次：

```bash
# 编译
go build -o perf_monitor ./main

# 运行（默认配置）
./perf_monitor

# 自定义配置
./perf_monitor \
  -host=your-mysql-host \
  -port=58888 \
  -user=dev \
  -password=your-password \
  -interval=10 \
  -threshold=10
```

**参数说明**：
- `-interval`: 检查间隔（秒），默认 10
- `-threshold`: 慢查询阈值（秒），默认 10

**输出示例**：
```
╔════════════════════════════════════════════════════════════════╗
║         Performance Schema 实时监控工具                         ║
╚════════════════════════════════════════════════════════════════╝

监控间隔: 10s | 慢查询阈值: 10s | Ctrl+C 退出

════════════ 2026-03-21 10:30:45 ════════════
✅ 没有超过 10s 的慢查询

════════════ 2026-03-21 10:30:55 ════════════
🚨 发现 1 个慢查询:

【慢查询 #1】
  线程ID: 12345 | 连接ID: 67890
  用户: dev@10.148.15.195 | 数据库: shop8267304538112000
  执行时间: 15.3s | 锁等待: 0.2s
  扫描行数: 500000 | 返回行数: 100
  状态: Sending data
  SQL: SELECT * FROM ttpos_product WHERE ...
  终止命令: KILL 67890;
```

---

### 方式 3: 手动 SQL 查询

直接在 MySQL 客户端执行：

```sql
-- 查看正在执行超过 10 秒的 SQL
SELECT
  t.processlist_id AS conn_id,
  ess.sql_text,
  ROUND(ess.timer_wait / 1000000000000, 1) AS exec_sec,
  ROUND(ess.lock_time / 1000000000000, 1) AS lock_sec,
  ess.rows_examined,
  ess.rows_sent,
  t.processlist_user,
  t.processlist_db,
  t.processlist_state
FROM performance_schema.events_statements_current ess
JOIN performance_schema.threads t
  ON ess.thread_id = t.thread_id
WHERE ess.sql_text IS NOT NULL
  AND ess.timer_wait / 1000000000000 > 10
ORDER BY ess.timer_wait DESC
LIMIT 10;
```

---

## 🔧 优化配置（可选）

### 启用长期历史记录

如果需要查看更多历史 SQL（最近 10000 条），需要 DBA 执行：

```sql
-- 动态启用（无需重启，但重启后失效）
UPDATE performance_schema.setup_consumers
SET ENABLED = 'YES'
WHERE NAME = 'events_statements_history_long';

-- 验证
SELECT NAME, ENABLED
FROM performance_schema.setup_consumers
WHERE NAME LIKE 'events_statements%';
```

**启用后可以查询历史慢查询**：

```sql
-- 查看最近 1 小时内执行过的慢查询（即使已经完成）
SELECT
  LEFT(sql_text, 100) AS query,
  ROUND(timer_wait / 1000000000000, 1) AS exec_sec,
  rows_examined,
  rows_sent,
  FROM_UNIXTIME(TRUNCATE(timer_start/1000000000000,0)) AS start_time
FROM performance_schema.events_statements_history_long
WHERE timer_wait / 1000000000000 > 10
  AND timer_start/1000000000000 > UNIX_TIMESTAMP() - 3600
ORDER BY timer_wait DESC
LIMIT 20;
```

### 持久化配置（需要 DBA）

在 `my.cnf` 中添加（重启后仍然生效）：

```ini
[mysqld]
performance_schema = ON
performance-schema-consumer-events-statements-current = ON
performance-schema-consumer-events-statements-history = ON
performance-schema-consumer-events-statements-history-long = ON
```

---

## 📊 常用查询示例

### 1. 查找"卡死"的查询

```sql
-- 找出执行超过 30 秒的，准备 KILL
SELECT
  CONCAT('KILL ', t.processlist_id, ';') AS kill_cmd,
  ROUND(ess.timer_wait / 1000000000000, 1) AS exec_sec,
  LEFT(ess.sql_text, 100) AS query
FROM performance_schema.events_statements_current ess
JOIN performance_schema.threads t ON ess.thread_id = t.thread_id
WHERE ess.timer_wait / 1000000000000 > 30;
```

### 2. 查看锁等待

```sql
-- 查看哪些查询在等锁
SELECT
  t.processlist_id,
  ROUND(ess.lock_time / 1000000000000, 1) AS lock_sec,
  LEFT(ess.sql_text, 100) AS query
FROM performance_schema.events_statements_current ess
JOIN performance_schema.threads t ON ess.thread_id = t.thread_id
WHERE ess.lock_time / 1000000000000 > 5;
```

### 3. Top 慢查询统计（历史）

```sql
-- 哪些 SQL 模式最慢（需要启用 history_long）
SELECT
  DIGEST_TEXT AS query_pattern,
  COUNT_STAR AS count,
  ROUND(SUM_TIMER_WAIT / 1000000000000, 1) AS total_sec,
  ROUND(AVG_TIMER_WAIT / 1000000000000, 2) AS avg_sec,
  ROUND(MAX_TIMER_WAIT / 1000000000000, 1) AS max_sec
FROM performance_schema.events_statements_summary_by_digest
ORDER BY SUM_TIMER_WAIT DESC
LIMIT 10;
```

---

## 🎯 集成到告警系统

### 方式 1: 定时执行脚本 + 钉钉告警

```bash
#!/bin/bash
# /home/deploy/monitor_slow_queries.sh

RESULT=$(./scripts/check_slow_queries.sh 10)

if echo "$RESULT" | grep -q "🚨 发现慢查询"; then
  # 发送钉钉告警
  curl -X POST "https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{
      \"msgtype\": \"text\",
      \"text\": {
        \"content\": \"🚨 生产数据库慢查询告警\\n\\n$RESULT\"
      }
    }"
fi
```

**添加到 crontab**：
```bash
# 每分钟检查一次
* * * * * /home/deploy/monitor_slow_queries.sh
```

### 方式 2: Grafana 告警规则

使用 Prometheus + MySQL Exporter：

```yaml
- alert: MySQLLongRunningQuery
  expr: mysql_info_schema_processlist_seconds > 30
  for: 1m
  labels:
    severity: warning
  annotations:
    summary: "MySQL 长时间运行的查询"
    description: "连接 {{ $labels.id }} 已运行 {{ $value }}s"
```

---

## 🐛 故障排查

### Q1: 查询返回空结果

**可能原因**：
- Performance Schema 未启用
- 当前确实没有慢查询

**解决方法**：
```bash
# 检查状态
./scripts/check_slow_queries.sh 0  # 阈值设为 0，查看所有正在执行的查询
```

### Q2: 权限错误

**错误信息**：
```
ERROR 1142 (42000): SELECT command denied to user 'dev'@'...' for table 'events_statements_current'
```

**解决方法**：
需要授予 Performance Schema 的 SELECT 权限：
```sql
GRANT SELECT ON performance_schema.* TO 'dev'@'%';
```

### Q3: 性能开销过大

Performance Schema 开销约 5-10%，如果不可接受：

```sql
-- 只保留最基础的监控
UPDATE performance_schema.setup_consumers
SET ENABLED = 'NO'
WHERE NAME = 'events_statements_history_long';
```

---

## 📝 总结

**Performance Schema 已启用，现在你可以**：

✅ **实时查看**正在执行的 SQL（即使它还没完成）
✅ **快速定位**卡死的查询（执行超过 30 秒的）
✅ **分析锁等待**（哪个查询在等锁）
✅ **统计慢查询**（哪些 SQL 模式最慢）

**推荐的使用流程**：

1. **日常监控**：运行 `./perf_monitor` 持续监控
2. **紧急排查**：运行 `./scripts/check_slow_queries.sh` 快速检查
3. **深度分析**：手动执行 SQL 查询，分析具体问题

**下一步**：
- [ ] 将监控脚本部署到生产环境
- [ ] 配置告警通知（钉钉/Slack/企业微信）
- [ ] 可选：启用 `events_statements_history_long` 获得更多历史数据
