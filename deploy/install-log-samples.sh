#!/bin/sh
# 装错误样本端口（:9101，socket activation）。前置：log-metrics 已装。
set -u
[ -f /etc/systemd/system/ttpos-log-metrics.timer ] || { echo "log-metrics 未装，跳过"; exit 0; }
install -m 0755 /tmp/ttpos-log-samples.sh /usr/local/bin/ttpos-log-samples.sh
IP=$(ip -4 -o addr show 2>/dev/null | grep -oE '10\.70\.20\.[0-9]+' | head -1)
[ -n "$IP" ] || { echo "无管理网地址，跳过"; exit 0; }

cat > /etc/systemd/system/ttpos-log-samples.socket <<EOF
[Unit]
Description=错误样本查询端口（socket activation，零常驻）
[Socket]
ListenStream=${IP}:9101
Accept=yes
[Install]
WantedBy=sockets.target
EOF

cat > '/etc/systemd/system/ttpos-log-samples@.service' <<EOF
[Unit]
Description=吐出最近错误样本
[Service]
ExecStart=/usr/local/bin/ttpos-log-samples.sh
StandardInput=socket
StandardOutput=socket
StandardError=journal
DynamicUser=yes
ReadOnlyPaths=/var/lib/ttpos-log-metrics
EOF

systemctl daemon-reload
systemctl enable --now ttpos-log-samples.socket >/dev/null 2>&1
sleep 1
C=$(curl -s --max-time 5 "http://${IP}:9101/" | head -c 60)
echo "socket=$(systemctl is-active ttpos-log-samples.socket)  响应: ${C:-（空）}"
