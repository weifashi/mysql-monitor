#!/bin/bash
#
# MySQL Performance Schema 监控工具快速启动脚本
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "════════════════════════════════════════════════════════════════"
echo "  MySQL Performance Schema 监控工具 - 快速启动"
echo "════════════════════════════════════════════════════════════════"
echo

# 检查 Docker 和 Docker Compose
if ! command -v docker &> /dev/null; then
    echo "❌ 错误: 未安装 Docker"
    echo "请先安装 Docker: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo "❌ 错误: 未安装 Docker Compose"
    echo "请先安装 Docker Compose: https://docs.docker.com/compose/install/"
    exit 1
fi

# 使用 docker compose 或 docker-compose
if docker compose version &> /dev/null; then
    DOCKER_COMPOSE="docker compose"
else
    DOCKER_COMPOSE="docker-compose"
fi

# 检查配置文件
if [ ! -f ".env" ]; then
    echo "📝 首次运行，初始化配置文件..."
    cp .env.example .env
    echo "✅ 已创建 .env"
    echo ""
    echo "⚠️  请编辑 .env 配置数据库连接信息"
    echo ""
    read -p "是否现在编辑配置文件？(y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        ${EDITOR:-vi} .env
    else
        echo "请稍后手动编辑 .env 后再次运行此脚本"
        exit 0
    fi
fi

echo "🔨 构建监控工具镜像..."
$DOCKER_COMPOSE -f docker-compose.yml build

echo ""
echo "请选择要启动的监控服务："
echo "  1) 开发环境 (dev)"
echo "  2) 测试环境 (test)"
echo "  3) 生产环境 (prod)"
echo "  4) 所有环境"
echo "  5) 自定义选择"
echo ""
read -p "请输入选项 [1-5]: " choice

case $choice in
    1)
        echo "🚀 启动开发环境监控..."
        $DOCKER_COMPOSE -f docker-compose.yml up -d monitor-dev
        SERVICES="monitor-dev"
        ;;
    2)
        echo "🚀 启动测试环境监控..."
        $DOCKER_COMPOSE -f docker-compose.yml up -d monitor-test
        SERVICES="monitor-test"
        ;;
    3)
        echo "🚀 启动生产环境监控..."
        $DOCKER_COMPOSE -f docker-compose.yml up -d monitor-prod
        SERVICES="monitor-prod"
        ;;
    4)
        echo "🚀 启动所有监控服务..."
        $DOCKER_COMPOSE -f docker-compose.yml up -d
        SERVICES="所有服务"
        ;;
    5)
        echo "可用服务: monitor-dev, monitor-test, monitor-prod"
        read -p "请输入要启动的服务（空格分隔）: " custom_services
        echo "🚀 启动监控服务: $custom_services"
        $DOCKER_COMPOSE -f docker-compose.yml up -d $custom_services
        SERVICES="$custom_services"
        ;;
    *)
        echo "❌ 无效选项"
        exit 1
        ;;
esac

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  ✅ 监控服务启动成功！"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "已启动服务: $SERVICES"
echo ""
echo "📊 查看服务状态:"
$DOCKER_COMPOSE -f docker-compose.yml ps
echo ""
echo "常用命令:"
echo "  查看日志:   $DOCKER_COMPOSE -f docker-compose.yml logs -f"
echo "  停止服务:   $DOCKER_COMPOSE -f docker-compose.yml stop"
echo "  重启服务:   $DOCKER_COMPOSE -f docker-compose.yml restart"
echo "  删除服务:   $DOCKER_COMPOSE -f docker-compose.yml down"
echo ""
echo "或使用 Makefile:"
echo "  make -f Makefile help     # 查看所有命令"
echo "  make -f Makefile logs     # 查看日志"
echo "  make -f Makefile status   # 查看状态"
echo ""
echo "════════════════════════════════════════════════════════════════"
echo ""
read -p "是否查看实时日志？(y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "📋 实时日志（按 Ctrl+C 退出）..."
    echo ""
    $DOCKER_COMPOSE -f docker-compose.yml logs -f
fi
