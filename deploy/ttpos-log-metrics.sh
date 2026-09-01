#!/bin/bash
# 扫描容器日志里的 error/fatal/panic，计数导出给 node_exporter textfile 采集器。
# Cloud Logging「错误日志」规则的自建等价物：走与容器内存监控同一条管道。
#
# 三个关键设计：
#   1) 增量扫描：按容器记 offset，每轮只读新增部分。文件变小（轮转）时归零。
#   2) 首见跳过历史：新容器第一次见到时 offset 直接设到文件末尾——
#      否则第一轮会把上限 100MB 的旧日志全算成"新错误"，触发一轮假告警。
#   3) 覆盖四类格式（不只结构化日志）：
#        Go zap JSON        {"level":"error"}          -> error/fatal/panic
#        nginx error.log    [error] / [crit]/[alert]/[emerg]（后三档更严重，归 fatal）
#        MySQL/MariaDB      [ERROR]
#        php-fpm            "] ERROR:" / "PHP Fatal error"
#      nginx 访问行的 5xx 不在这里数——那由应用指标的 5xx 规则覆盖。
#   4) 排除业务噪音：17 条清单来自生产 Cloud Logging 配置——这些在代码里是
#      error 级别、业务上是正常路径，不排掉第一天就会被淹没。
set -u
TF="${1:-/var/lib/node_exporter/textfile}"
SD=/var/lib/ttpos-log-metrics
EXCL="$SD/exclude.txt"
OUT="$TF/ttpos_logs.prom"
TMP="$OUT.$$"
mkdir -p "$TF" "$SD"
[ -f "$EXCL" ] || : > "$EXCL"

VM=$(hostname)
START=$(date +%s%N)

{
  echo "# HELP ttpos_log_errors_total 容器日志中的错误行数（已排除业务噪音）"
  echo "# TYPE ttpos_log_errors_total counter"
} > "$TMP"

n=0
while read -r cid cname; do
  [ -z "$cid" ] && continue
  lp=$(docker inspect --format '{{.LogPath}}' "$cid" 2>/dev/null)
  [ -f "$lp" ] || continue
  size=$(stat -c %s "$lp" 2>/dev/null) || continue

  sf="$SD/$cid.state"
  if [ -f "$sf" ]; then
    read -r offset ce cf cp < "$sf"
  else
    offset=$size; ce=0; cf=0; cp=0     # 首见：跳过历史
  fi
  [ "$size" -lt "$offset" ] && offset=0   # 日志轮转，从头读

  if [ "$size" -gt "$offset" ]; then
    chunk=$((size - offset))
    # 单轮上限 5MB：异常刷屏时保护 CPU；漏掉的下一轮追不上就认，告警本来就会响
    if [ "$chunk" -gt 5242880 ]; then
      offset=$((size - 5242880)); chunk=5242880
    fi
    # docker json-file 里应用日志被转义：\"level\":\"error\"；也兼容 logfmt 的 level=error
    read -r ie iff ip <<< "$(tail -c +$((offset + 1)) "$lp" | head -c "$chunk" \
      | grep -a -v -F -f "$EXCL" \
      | awk '
        /level\\":\\"panic|level=panic|"level":"panic/  {p++; next}
        /level\\":\\"fatal|level=fatal|"level":"fatal|PHP Fatal error|\[crit\]|\[alert\]|\[emerg\]/  {f++; next}
        /level\\":\\"error|level=error|"level":"error|\[error\]|\[ERROR\]|\] ERROR:/  {e++}
        END{printf "%d %d %d", e+0, f+0, p+0}')"
    ce=$((ce + ie)); cf=$((cf + iff)); cp=$((cp + ip))
  fi

  echo "$size $ce $cf $cp" > "$sf"
  {
    echo "ttpos_log_errors_total{vm=\"$VM\",container=\"$cname\",kind=\"error\"} $ce"
    echo "ttpos_log_errors_total{vm=\"$VM\",container=\"$cname\",kind=\"fatal\"} $cf"
    echo "ttpos_log_errors_total{vm=\"$VM\",container=\"$cname\",kind=\"panic\"} $cp"
  } >> "$TMP"
  n=$((n + 1))
done < <(docker ps --no-trunc --format '{{.ID}} {{.Names}}' 2>/dev/null)

# 已消失容器的状态文件清理（避免无限累积）
find "$SD" -name '*.state' -mtime +7 -delete 2>/dev/null

DUR=$(( ($(date +%s%N) - START) / 1000000 ))
{
  echo "# HELP ttpos_log_scan_containers 本轮扫描的容器数"
  echo "# TYPE ttpos_log_scan_containers gauge"
  echo "ttpos_log_scan_containers{vm=\"$VM\"} $n"
  echo "# HELP ttpos_log_scan_duration_ms 本轮扫描耗时"
  echo "# TYPE ttpos_log_scan_duration_ms gauge"
  echo "ttpos_log_scan_duration_ms{vm=\"$VM\"} $DUR"
} >> "$TMP"

mv "$TMP" "$OUT"   # 原子替换
