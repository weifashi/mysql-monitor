#!/bin/bash
# 为加固 VM 放行 ops-01 访问 9101（错误样本端口）。
# 安全程序：runtime 用 nft add rule 增量加（不碰其它表）；
# 持久化只改配置文件、不执行 nft -f —— 文件首行的 flush ruleset 会连
# Docker 的 NAT 表一起清掉（F1 事故成因）。下次重启由 boot 顺序自然恢复。
set -u
RULE_RT='ip saddr 10.70.20.34 tcp dport 9101 accept'
STAMP=$(date +%Y%m%d-%H%M%S)
for vm in "$@"; do
  printf '  %-18s ' "$vm"
  T=$(incus exec "$vm" -- sh -c 'nft list tables 2>/dev/null | grep -oP "(?<=table inet ).*" | head -1')
  [ -z "$T" ] && { echo "无 inet 表，跳过"; continue; }
  if incus exec "$vm" -- sh -c "nft list chain inet $T input 2>/dev/null | grep -q 'dport 9101'"; then
    echo "已放行"; continue
  fi
  incus exec "$vm" -- nft add rule inet "$T" input $RULE_RT comment '"ops-01 log samples"'
  F=/etc/nftables.conf
  incus exec "$vm" -- sh -c "cp $F ${F}.bak9101-${STAMP}
awk '{print} !d && /dport 9100 accept/ {print \"    ip saddr 10.70.20.34 tcp dport 9101 accept comment \\\"ops-01 log samples\\\"\"; d=1}' $F > ${F}.new && cat ${F}.new > $F && rm -f ${F}.new
nft -c -f $F >/dev/null 2>&1 && echo -n '' || echo '（警告：conf 语法校验失败，重启前需人工检查）'"
  echo "runtime 已放行，conf 已持久化（未重载）"
done
