#!/bin/bash
#
# 部署 Performance Schema 监控工具到监控服务器
# 使用方法: ./deploy_to_monitoring_server.sh user@monitoring-server
#

set -e

# 切换到项目根目录（支持从 scripts/ 或项目根目录运行）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

if [ -z "$1" ]; then
    echo "用法: $0 user@monitoring-server"
    exit 1
fi

REMOTE_HOST="$1"
REMOTE_DIR="/opt/mysql-monitor"

echo "════════════════════════════════════════════════════════════════"
echo "  部署 Performance Schema 监控工具"
echo "  目标: $REMOTE_HOST:$REMOTE_DIR"
echo "════════════════════════════════════════════════════════════════"
echo

# 1. 创建远程目录
echo "【步骤 1】创建远程目录..."
ssh "$REMOTE_HOST" "sudo mkdir -p $REMOTE_DIR && sudo chown \$(whoami) $REMOTE_DIR"
echo "✅ 目录创建完成"
echo

# 2. 上传监控工具
echo "【步骤 2】上传监控工具..."
scp perf_monitor "$REMOTE_HOST:$REMOTE_DIR/"
ssh "$REMOTE_HOST" "chmod +x $REMOTE_DIR/perf_monitor"
echo "✅ 上传完成"
echo

# 3. 上传文档
echo "【步骤 3】上传文档..."
scp docs/PERFORMANCE_SCHEMA_SETUP.md "$REMOTE_HOST:$REMOTE_DIR/" 2>/dev/null || true
echo "✅ 文档上传完成"
echo

# 4. 创建 systemd service 文件
echo "【步骤 4】创建 systemd service 文件..."
cat > /tmp/mysql-perf-monitor.service << 'EOF'
[Unit]
Description=MySQL Performance Schema Monitor
After=network.target

[Service]
Type=simple
User=nobody
Group=nogroup
WorkingDirectory=/opt/mysql-monitor
ExecStart=/opt/mysql-monitor/perf_monitor -threshold=10 -interval=10
Restart=always
RestartSec=10
StandardOutput=append:/var/log/mysql-perf-monitor.log
StandardError=append:/var/log/mysql-perf-monitor.log

[Install]
WantedBy=multi-user.target
EOF

scp /tmp/mysql-perf-monitor.service "$REMOTE_HOST:/tmp/"
ssh "$REMOTE_HOST" "sudo mv /tmp/mysql-perf-monitor.service /etc/systemd/system/"
echo "✅ systemd service 创建完成"
echo

# 5. 启动服务
echo "【步骤 5】启动服务..."
ssh "$REMOTE_HOST" << 'REMOTE_COMMANDS'
sudo systemctl daemon-reload
sudo systemctl enable mysql-perf-monitor
sudo systemctl start mysql-perf-monitor
sleep 2
sudo systemctl status mysql-perf-monitor --no-pager
REMOTE_COMMANDS
echo

echo "════════════════════════════════════════════════════════════════"
echo "  部署完成！"
echo "════════════════════════════════════════════════════════════════"
echo
echo "管理命令:"
echo "  查看状态: ssh $REMOTE_HOST 'sudo systemctl status mysql-perf-monitor'"
echo "  查看日志: ssh $REMOTE_HOST 'sudo tail -f /var/log/mysql-perf-monitor.log'"
echo "  停止服务: ssh $REMOTE_HOST 'sudo systemctl stop mysql-perf-monitor'"
echo "  重启服务: ssh $REMOTE_HOST 'sudo systemctl restart mysql-perf-monitor'"
echo
