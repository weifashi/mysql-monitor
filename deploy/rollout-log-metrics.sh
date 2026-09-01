#!/bin/bash
# 在物理机上执行：下载一次，push 到本机所有 VM 并安装。
# rehearsal/storage 这类 VM 的 output 链 drop 443 拉不到文件，push 绕过。
set -u
B="https://8008--main--admin-jbcnet--weifashi.coder.tbc.5ok.co/_drop"
cd /tmp
for f in ttpos-log-metrics.sh ttpos-log-exclude.txt install-log-metrics.sh; do
  curl -fsSL --max-time 60 -o "$f" "$B/$f" || { echo "下载失败 $f"; exit 1; }
done
ME=$(case "$(hostname)" in *544310*) echo ph01;; *568389*) echo ph02;; *5014889*) echo ph03;; esac)
for e in $(incus list --format csv -c nsL 2>/dev/null | awk -F, '$2=="RUNNING"{print $1","$3}'); do
  vm="${e%%,*}"; loc="${e##*,}"
  [ "$loc" != "$ME" ] && continue
  printf '  %-18s ' "$vm"
  incus file push /tmp/ttpos-log-metrics.sh "$vm/tmp/ttpos-log-metrics.sh" --mode 0755 2>/dev/null
  incus file push /tmp/ttpos-log-exclude.txt "$vm/tmp/ttpos-log-exclude.txt" --mode 0644 2>/dev/null
  incus file push /tmp/install-log-metrics.sh "$vm/tmp/inst.sh" --mode 0755 2>/dev/null
  incus exec "$vm" -- sh /tmp/inst.sh 2>&1 | tail -1
done
