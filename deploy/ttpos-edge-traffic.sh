#!/bin/sh
# 网关流量导出：curl 各网关的 stub_status，写 textfile。
set -u
OUT_DIR="${1:-/var/lib/node_exporter/textfile}"
OUT="$OUT_DIR/ttpos_edge_traffic.prom"
TMP="$OUT.$$"
VM=$(hostname)
{
  echo "# HELP ttpos_nginx_connections_active 网关当前活跃连接（并发）"
  echo "# TYPE ttpos_nginx_connections_active gauge"
  echo "# HELP ttpos_nginx_requests_total 网关累计请求数"
  echo "# TYPE ttpos_nginx_requests_total counter"
  echo "# HELP ttpos_nginx_connections_waiting 网关空闲保活连接"
  echo "# TYPE ttpos_nginx_connections_waiting gauge"
} > "$TMP"
for e in regional-origin:8083 core-shadow:8084 release-object:8085; do
  gw="${e%%:*}"; port="${e##*:}"
  st=$(curl -s --max-time 3 "http://127.0.0.1:$port/stub_status") || continue
  [ -z "$st" ] && continue
  echo "$st" | awk -v vm="$VM" -v gw="$gw" '
    /^Active connections/ { printf "ttpos_nginx_connections_active{vm=\"%s\",gateway=\"%s\"} %s\n", vm, gw, $3 }
    /^ [0-9]+ [0-9]+ [0-9]+/ { printf "ttpos_nginx_requests_total{vm=\"%s\",gateway=\"%s\"} %s\n", vm, gw, $3 }
    /Waiting/ { for(i=1;i<=NF;i++) if($i=="Waiting:") printf "ttpos_nginx_connections_waiting{vm=\"%s\",gateway=\"%s\"} %s\n", vm, gw, $(i+1) }
  ' >> "$TMP"
done
mv "$TMP" "$OUT"
