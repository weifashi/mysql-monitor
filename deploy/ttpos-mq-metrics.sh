#!/bin/sh
# RocketMQ 业务指标导出（platform-0X 专用）：借 broker 容器里的 mqadmin
# 查集群状态与消费堆积，写成 textfile 交给 node_exporter。
#
# 每次 mqadmin 是一个短命 JVM（实测 clusterList ~14s），所以 2 分钟一跑、
# 每步带 timeout；consumerProgress 是集群级数据，只在 platform-01 跑一份，
# 其余两台只出 broker 视角指标，避免三份重复序列。
set -u
OUT_DIR="${1:-/var/lib/node_exporter/textfile}"
OUT="$OUT_DIR/ttpos_mq.prom"
TMP="$OUT.$$"
VM=$(hostname)
CLUSTER=ttpos-ovh

B=$(docker ps --format '{{.Names}}' 2>/dev/null | grep '^rocketmq-broker' | head -1)
if [ -z "$B" ]; then
  # broker 容器不在：指标整体撤下，掉线由 ttpos_container_up 规则负责
  rm -f "$OUT"
  exit 0
fi

MQHOME=$(docker exec "$B" sh -c 'echo $ROCKETMQ_HOME' 2>/dev/null)
NS=$(docker exec "$B" sh -c "grep '^namesrvAddr' \$ROCKETMQ_HOME/conf/broker-standalone.conf | cut -d= -f2" 2>/dev/null)
[ -z "$MQHOME" ] || [ -z "$NS" ] && { rm -f "$OUT"; exit 0; }

mq() { docker exec "$B" sh -c "NAMESRV_ADDR='$NS' timeout 45 sh $MQHOME/bin/mqadmin $1 2>/dev/null"; }

ok=1
{
  echo "# HELP ttpos_mq_broker_activated ttpos-ovh 集群中 ACTIVATED 的 broker 数"
  echo "# TYPE ttpos_mq_broker_activated gauge"
  echo "# HELP ttpos_mq_broker_in_tps broker 写入 TPS（clusterList）"
  echo "# TYPE ttpos_mq_broker_in_tps gauge"
  echo "# HELP ttpos_mq_consumer_lag 消费组总堆积（Diff Total，仅 platform-01 输出）"
  echo "# TYPE ttpos_mq_consumer_lag gauge"
  echo "# HELP ttpos_mq_scrape_ok 本轮 mqadmin 采集是否全部成功"
  echo "# TYPE ttpos_mq_scrape_ok gauge"
} > "$TMP"

# --- 集群视角：activated broker 数 + 各 broker TPS ---
CL=$(mq "clusterList -c $CLUSTER")
if [ -n "$CL" ]; then
  echo "$CL" | awk -v vm="$VM" -v cl="$CLUSTER" '
    $1==cl && $NF=="true" {
      n++
      tps=$6; sub(/\(.*/,"",tps)
      printf "ttpos_mq_broker_in_tps{vm=\"%s\",broker=\"%s\"} %s\n", vm, $2, tps
    }
    END { printf "ttpos_mq_broker_activated{vm=\"%s\",cluster=\"%s\"} %d\n", vm, cl, n }
  ' >> "$TMP"
else
  ok=0
fi

# --- 消费堆积：集群级，只在 platform-01 出一份 ---
if [ "$VM" = "platform-01" ]; then
  CP=$(mq consumerProgress)
  if [ -n "$CP" ]; then
    echo "$CP" | awk -v vm="$VM" '
      /^#/ { next }
      NF >= 6 && $NF ~ /^[0-9]+$/ {
        printf "ttpos_mq_consumer_lag{vm=\"%s\",group=\"%s\"} %s\n", vm, $1, $NF
      }
    ' >> "$TMP"
  else
    ok=0
  fi
fi

echo "ttpos_mq_scrape_ok{vm=\"$VM\"} $ok" >> "$TMP"
mv "$TMP" "$OUT"
