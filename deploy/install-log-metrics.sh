#!/bin/sh
# 装日志扫描：脚本 + 排除清单 + systemd timer。textfile 采集器已开（容器指标那轮）。
set -u
SD=/var/lib/ttpos-log-metrics
command -v docker >/dev/null 2>&1 || { echo "无 docker，跳过"; exit 0; }
[ -f /etc/systemd/system/node_exporter.service ] || { echo "无 node_exporter，跳过"; exit 0; }
mkdir -p "$SD"
install -m 0755 /tmp/ttpos-log-metrics.sh /usr/local/bin/ttpos-log-metrics.sh
install -m 0644 /tmp/ttpos-log-exclude.txt "$SD/exclude.txt"
cat > /etc/systemd/system/ttpos-log-metrics.service <<EOF
[Unit]
Description=扫描容器日志错误计数导出给 node_exporter
[Service]
Type=oneshot
ExecStart=/usr/local/bin/ttpos-log-metrics.sh /var/lib/node_exporter/textfile
EOF
cat > /etc/systemd/system/ttpos-log-metrics.timer <<EOF
[Unit]
Description=每分钟扫描容器日志错误
[Timer]
OnBootSec=90s
OnUnitActiveSec=1min
AccuracySec=5s
[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now ttpos-log-metrics.timer >/dev/null 2>&1
/usr/local/bin/ttpos-log-metrics.sh /var/lib/node_exporter/textfile
IP=$(ip -4 -o addr show 2>/dev/null | grep -oE '10\.70\.(10|20)\.[0-9]+' | head -1)
N=$(curl -s --max-time 5 "http://$IP:9100/metrics" 2>/dev/null | grep -c '^ttpos_log_')
echo "指标 $N 条  timer=$(systemctl is-active ttpos-log-metrics.timer)"
