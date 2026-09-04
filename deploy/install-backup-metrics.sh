#!/bin/sh
set -u
install -m 0755 /tmp/ttpos-backup-metrics.sh /usr/local/bin/ttpos-backup-metrics.sh
cat > /etc/systemd/system/ttpos-backup-metrics.service <<'UNIT'
[Unit]
Description=Backup freshness metrics
[Service]
Type=oneshot
ExecStart=/usr/local/bin/ttpos-backup-metrics.sh
UNIT
cat > /etc/systemd/system/ttpos-backup-metrics.timer <<'UNIT'
[Unit]
Description=Run backup metrics every 10 minutes
[Timer]
OnBootSec=2min
OnUnitActiveSec=10min
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload && systemctl enable --now ttpos-backup-metrics.timer >/dev/null 2>&1
/usr/local/bin/ttpos-backup-metrics.sh
cat /var/lib/node_exporter/textfile/ttpos_backup.prom | grep -v '^#'
