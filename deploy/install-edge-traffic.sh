#!/bin/sh
set -u
python3 /tmp/edge-status-fix.py || exit 1
for c in $(docker ps --format '{{.Names}}' | grep gateway); do
  docker exec "$c" nginx -t >/dev/null 2>&1 && docker exec "$c" nginx -s reload && echo "$c reloaded"
done
install -m 0755 /tmp/ttpos-edge-traffic.sh /usr/local/bin/ttpos-edge-traffic.sh
cat > /etc/systemd/system/ttpos-edge-traffic.service <<'UNIT'
[Unit]
Description=Edge gateway traffic metrics
[Service]
Type=oneshot
ExecStart=/usr/local/bin/ttpos-edge-traffic.sh
UNIT
cat > /etc/systemd/system/ttpos-edge-traffic.timer <<'UNIT'
[Unit]
Description=Run edge traffic metrics every minute
[Timer]
OnBootSec=1min
OnUnitActiveSec=1min
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload && systemctl enable --now ttpos-edge-traffic.timer >/dev/null 2>&1
/usr/local/bin/ttpos-edge-traffic.sh
echo "序列: $(grep -c '^ttpos_nginx' /var/lib/node_exporter/textfile/ttpos_edge_traffic.prom 2>/dev/null || echo 0) 条"
