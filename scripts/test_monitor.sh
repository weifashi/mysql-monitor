#!/bin/bash
#
# Performance Schema 监控工具测试脚本
#

set -e

# 切换到项目根目录（支持从 scripts/ 或项目根目录运行）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "════════════════════════════════════════════════════════════════"
echo "  Performance Schema 监控工具 - 功能测试"
echo "════════════════════════════════════════════════════════════════"
echo

# 测试 1: 检查编译是否成功
echo "【测试 1】检查编译文件..."
if [ -f "./perf_monitor" ]; then
    echo "✅ perf_monitor 已编译"
    ls -lh perf_monitor
else
    echo "❌ perf_monitor 未找到，需要先编译"
    exit 1
fi
echo

# 测试 2: 测试连接
echo "【测试 2】测试数据库连接..."
if ./perf_monitor -threshold=999 2>&1 | grep -q "Performance Schema"; then
    echo "✅ 数据库连接成功"
else
    echo "❌ 数据库连接失败"
    exit 1
fi
echo

# 测试 3: 检查当前慢查询（阈值 5 秒）
echo "【测试 3】检查当前慢查询（阈值 5s）..."
timeout 3s ./perf_monitor -threshold=5 -interval=1 2>&1 | head -20 || true
echo

# 测试 4: 检查当前慢查询（阈值 10 秒）
echo "【测试 4】检查当前慢查询（阈值 10s）..."
timeout 3s ./perf_monitor -threshold=10 -interval=1 2>&1 | head -20 || true
echo

# 测试 5: 查看帮助信息
echo "【测试 5】查看命令参数..."
./perf_monitor -h 2>&1 | head -15 || true
echo

echo "════════════════════════════════════════════════════════════════"
echo "  测试完成"
echo "════════════════════════════════════════════════════════════════"
echo
echo "✅ 所有测试通过！"
echo
echo "使用方法："
echo "  1. 快速检查: ./perf_monitor"
echo "  2. 自定义阈值: ./perf_monitor -threshold=30"
echo "  3. 自定义间隔: ./perf_monitor -interval=5"
echo "  4. 后台运行: nohup ./perf_monitor > monitor.log 2>&1 &"
echo
