#!/bin/bash
#
# 验证工作区结构和配置
#

set -e

# 切换到项目根目录（支持从 scripts/ 或项目根目录运行）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "════════════════════════════════════════════════════════════════"
echo "  MySQL 监控工具工作区验证"
echo "════════════════════════════════════════════════════════════════"
echo

# 检查必需文件
echo "【检查 1】验证必需文件..."
required_files=(
    "README.md"
    "docker-compose.yml"
    "Dockerfile"
    "Makefile"
    ".env.example"
    "main/performance_schema_monitor.go"
    "scripts/quick_start.sh"
    ".gitignore"
)

missing_files=0
for file in "${required_files[@]}"; do
    if [ -f "$file" ]; then
        echo "  ✅ $file"
    else
        echo "  ❌ $file (缺失)"
        missing_files=$((missing_files + 1))
    fi
done

if [ $missing_files -gt 0 ]; then
    echo "❌ 有 $missing_files 个文件缺失"
    exit 1
fi

echo

# 检查文档目录
echo "【检查 2】验证文档目录..."
doc_files=(
    "docs/NOTIFICATION_QUICK_START.md"
    "docs/NOTIFICATION_GUIDE.md"
    "docs/README_DOCKER.md"
    "docs/PERFORMANCE_SCHEMA_SETUP.md"
    "docs/FEATURES.md"
    "docs/CHANGELOG.md"
)

missing_docs=0
for file in "${doc_files[@]}"; do
    if [ -f "$file" ]; then
        echo "  ✅ $file"
    else
        echo "  ❌ $file (缺失)"
        missing_docs=$((missing_docs + 1))
    fi
done

if [ $missing_docs -gt 0 ]; then
    echo "❌ 有 $missing_docs 个文档缺失"
    exit 1
fi

echo

# 检查可执行文件
echo "【检查 3】验证可执行权限..."
executable_files=(
    "scripts/quick_start.sh"
    "scripts/check_slow_queries.sh"
    "scripts/deploy_to_monitoring_server.sh"
    "scripts/test_monitor.sh"
)

for file in "${executable_files[@]}"; do
    if [ -x "$file" ]; then
        echo "  ✅ $file (可执行)"
    else
        echo "  ⚠️  $file (不可执行，正在修复...)"
        chmod +x "$file"
    fi
done

echo

# 检查配置文件
echo "【检查 4】验证配置模板..."
if [ -f ".env.example" ]; then
    env_vars=$(grep -c "^[A-Z_]*=" .env.example || true)
    echo "  ✅ .env.example 包含 $env_vars 个配置项"
else
    echo "  ❌ .env.example 不存在"
    exit 1
fi

echo

# 检查 Docker 文件
echo "【检查 5】验证 Docker 配置..."

# 检查 docker-compose.yml
if grep -q "monitor-dev:" docker-compose.yml && \
   grep -q "monitor-test:" docker-compose.yml && \
   grep -q "monitor-prod:" docker-compose.yml; then
    echo "  ✅ docker-compose.yml 包含 3 个服务"
else
    echo "  ❌ docker-compose.yml 配置不完整"
    exit 1
fi

# 检查 Dockerfile
if grep -q "FROM golang:1.23-alpine" Dockerfile && \
   grep -q "FROM alpine:3.19" Dockerfile; then
    echo "  ✅ Dockerfile 多阶段构建配置正确"
else
    echo "  ❌ Dockerfile 配置不正确"
    exit 1
fi

echo

# 检查 Makefile
echo "【检查 6】验证 Makefile 命令..."
if grep -q "^help:.*##" Makefile && \
   grep -q "^up:.*##" Makefile && \
   grep -q "^logs:.*##" Makefile; then
    echo "  ✅ Makefile 包含帮助和常用命令"
else
    echo "  ❌ Makefile 配置不完整"
    exit 1
fi

echo

# 检查 Go 源码
echo "【检查 7】验证 Go 源码..."
if grep -q "package main" main/performance_schema_monitor.go && \
   grep -q "DINGTALK_WEBHOOK" main/performance_schema_monitor.go && \
   grep -q "EMAIL_ENABLED" main/performance_schema_monitor.go; then
    echo "  ✅ performance_schema_monitor.go 包含通知功能"
else
    echo "  ❌ performance_schema_monitor.go 配置不完整"
    exit 1
fi

echo

# 统计信息
echo "════════════════════════════════════════════════════════════════"
echo "  验证结果"
echo "════════════════════════════════════════════════════════════════"
echo
echo "📁 项目结构:"
echo "  - 核心文件: $(ls -1 docker-compose.yml main/*.go Dockerfile Makefile 2>/dev/null | wc -l) 个"
echo "  - 脚本文件: $(ls -1 scripts/*.sh 2>/dev/null | wc -l) 个"
echo "  - 文档文件: $(find docs -name "*.md" 2>/dev/null | wc -l) 个"
echo "  - 总文件数: $(find . -type f ! -path "./.git/*" 2>/dev/null | wc -l) 个"
echo
echo "📊 代码统计:"
if [ -f "main/performance_schema_monitor.go" ]; then
    go_lines=$(wc -l < main/performance_schema_monitor.go)
    echo "  - Go 代码: $go_lines 行"
fi
if [ -f "docker-compose.yml" ]; then
    yaml_lines=$(wc -l < docker-compose.yml)
    echo "  - Docker Compose: $yaml_lines 行"
fi
echo
echo "✅ 所有检查通过！工作区结构正确。"
echo
echo "下一步:"
echo "  1. 复制配置: cp .env.example .env"
echo "  2. 编辑配置: vi .env"
echo "  3. 一键启动: ./scripts/quick_start.sh"
echo "  或者查看帮助: make help"
echo
echo "════════════════════════════════════════════════════════════════"
