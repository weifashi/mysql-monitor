#!/bin/sh
# 把容器的 cgroup 内存数据导出成 Prometheus 文本，交给 node_exporter 的
# textfile 采集器暴露。
#
# 为什么不用 cAdvisor：它是常驻服务，约 100~200MB。三台物理机当前可用内存
# 只有 1~3G 且已经在换页，加不起。这个脚本每分钟跑几百毫秒，常驻内存为零。
#
# 为什么脚本自己算比率：告警引擎的比率计算要求分子分母用同一个标签过滤，
# 按容器分别配规则就得一容器一条（全集群 80 条），而 Coolify 的容器名带
# 随机后缀、重新部署就变。让脚本算好，规则按机器配 aggregate=max 即可。
set -u
OUT_DIR="${1:-/var/lib/node_exporter/textfile}"
OUT="$OUT_DIR/ttpos_container.prom"
TMP="$OUT.$$"
mkdir -p "$OUT_DIR"

VM=$(hostname)

{
  echo "# HELP ttpos_container_memory_bytes 容器当前内存用量"
  echo "# TYPE ttpos_container_memory_bytes gauge"
  echo "# HELP ttpos_container_memory_limit_bytes 容器内存上限（无限制的不输出）"
  echo "# TYPE ttpos_container_memory_limit_bytes gauge"
  echo "# HELP ttpos_container_memory_usage_ratio 用量占上限的百分比"
  echo "# TYPE ttpos_container_memory_usage_ratio gauge"
  echo "# HELP ttpos_container_oom_kills_total 该容器 cgroup 内发生的 OOM kill 次数"
  echo "# TYPE ttpos_container_oom_kills_total counter"
} > "$TMP"

# 容器 ID -> 名字。用 --no-trunc 拿全 ID，才能和 cgroup 目录名对上。
docker ps --no-trunc --format '{{.ID}} {{.Names}}' 2>/dev/null > /tmp/.ctr_names.$$ || : > /tmp/.ctr_names.$$

n=0
while read -r cid cname; do
  [ -z "$cid" ] && continue
  # cgroup v2：systemd 驱动是 system.slice/docker-<id>.scope，
  # cgroupfs 驱动是 /sys/fs/cgroup/docker/<id>
  base=""
  for cand in \
    "/sys/fs/cgroup/system.slice/docker-$cid.scope" \
    "/sys/fs/cgroup/docker/$cid"
  do
    [ -f "$cand/memory.current" ] && { base="$cand"; break; }
  done
  [ -z "$base" ] && continue

  cur=$(cat "$base/memory.current" 2>/dev/null) || continue
  max=$(cat "$base/memory.max" 2>/dev/null)
  oom=$(awk '/^oom_kill /{print $2}' "$base/memory.events" 2>/dev/null)
  [ -z "$oom" ] && oom=0

  echo "ttpos_container_memory_bytes{vm=\"$VM\",container=\"$cname\"} $cur" >> "$TMP"
  echo "ttpos_container_oom_kills_total{vm=\"$VM\",container=\"$cname\"} $oom" >> "$TMP"

  # memory.max 为 "max" 表示没有限制，比率无从谈起，只输出用量
  if [ "$max" != "max" ] && [ -n "$max" ] && [ "$max" -gt 0 ] 2>/dev/null; then
    echo "ttpos_container_memory_limit_bytes{vm=\"$VM\",container=\"$cname\"} $max" >> "$TMP"
    awk -v c="$cur" -v m="$max" -v v="$VM" -v n="$cname" \
      'BEGIN{printf "ttpos_container_memory_usage_ratio{vm=\"%s\",container=\"%s\"} %.2f\n", v, n, c*100/m}' >> "$TMP"
  fi
  n=$((n+1))
done < /tmp/.ctr_names.$$
rm -f /tmp/.ctr_names.$$

# 容器运行状态：docker ps -a 含已停止的容器（cgroup 目录随停止消失，上面
# 的循环看不到它们）。停止=0 让「掉线」规则有值可评；容器被彻底删除时
# 序列消失，由告警引擎的 absent_as_zero 兜底。
{
  echo "# HELP ttpos_container_up 容器运行状态（1=running，其它=0）"
  echo "# TYPE ttpos_container_up gauge"
} >> "$TMP"
docker ps -a --format '{{.Names}} {{.State}}' 2>/dev/null | while read -r cname cstate; do
  [ -z "$cname" ] && continue
  if [ "$cstate" = "running" ]; then up=1; else up=0; fi
  echo "ttpos_container_up{vm=\"$VM\",container=\"$cname\"} $up" >> "$TMP"
done

{
  echo "# HELP ttpos_container_metrics_containers 本次采集到的容器数"
  echo "# TYPE ttpos_container_metrics_containers gauge"
  echo "ttpos_container_metrics_containers{vm=\"$VM\"} $n"
  echo "# HELP ttpos_container_metrics_epoch 本次写入的 unix 时间戳（排查用）"
  echo "# TYPE ttpos_container_metrics_epoch gauge"
  echo "ttpos_container_metrics_epoch{vm=\"$VM\"} $(date +%s)"
} >> "$TMP"

# 原子替换：textfile 采集器随时可能在读，写了一半的文件会被判为格式错误
mv "$TMP" "$OUT"
