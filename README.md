# MySQL Performance Schema 实时监控工具

> 基于 Performance Schema 的 MySQL 慢查询实时监控工具，支持钉钉、飞书、邮件告警通知。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8.svg)](https://go.dev/)
[![Docker](https://img.shields.io/badge/docker-ready-blue.svg)](https://www.docker.com/)

## ✨ 核心特性

- 🔍 **实时监控**：基于 Performance Schema，无需等待查询完成即可检测
- 📊 **详细信息**：线程ID、执行时间、锁等待、扫描行数、完整SQL等
- 🔔 **多种通知**：支持钉钉、飞书机器人与邮件，及时告警
- 🚫 **告警去重**：同一连接（KILL id）仅通知一次，避免刷屏
- 🐳 **容器化部署**：完整的 Docker Compose 配置，开箱即用
- 🔧 **灵活配置**：支持环境变量、命令行参数，多数据库独立配置
- 📚 **完善文档**：详细的部署指南、配置说明、故障排查

## 🚀 快速开始

### 方式 1：一键启动（推荐）

```bash
cd mysql-monitor
./scripts/quick_start.sh
```

脚本会自动：
1. 检查 Docker 环境
2. 初始化配置文件
3. 构建镜像
4. 交互式选择要启动的服务
5. 启动服务并显示状态

### 方式 2：使用 Makefile

```bash
cd mysql-monitor

# 初始化配置
make init

# 编辑配置文件
vi .env

# 启动生产环境监控
make up-prod

# 查看日志
make logs-prod
```

### 方式 3：Docker Compose

```bash
cd mysql-monitor

# 复制配置
cp .env.example .env

# 编辑配置
vi .env

# 构建并启动
docker-compose build
docker-compose up -d monitor-prod

# 查看日志
docker-compose logs -f monitor-prod
```

## 📋 配置说明

### 基础配置

编辑 `.env` 文件：

```bash
# 数据库连接
PROD_MYSQL_HOST=your-mysql-host
PROD_MYSQL_PORT=58888
PROD_MYSQL_USER=dev
PROD_MYSQL_PASSWORD=your-password

# 监控参数
PROD_MONITOR_INTERVAL=10    # 检查间隔（秒）
PROD_MONITOR_THRESHOLD=10   # 慢查询阈值（秒）
```

### 通知配置（可选）

#### 钉钉通知

```bash
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN
PROD_DINGTALK_SECRET=SECxxxxxx  # 可选，加签密钥
```

#### 邮件通知

```bash
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=monitor@company.com
PROD_EMAIL_TO=admin@company.com,ops@company.com
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=monitor@company.com
PROD_EMAIL_PASSWORD=your-authorization-code
```

**详细配置指南**：[docs/NOTIFICATION_QUICK_START.md](docs/NOTIFICATION_QUICK_START.md)

## 🛠️ 常用命令

```bash
# 查看所有命令
make help

# 服务管理
make up           # 启动所有监控
make up-prod      # 仅启动生产环境
make stop         # 停止所有
make restart-prod # 重启生产环境
make down         # 停止并删除

# 日志查看
make logs         # 所有日志
make logs-prod    # 生产环境日志
make logs-tail    # 最近100行

# 状态检查
make status       # 查看运行状态

# 更新
make build        # 重新构建镜像
make update       # 更新代码并重启
```

## 📊 监控效果

### 控制台输出

```
════════════ 2026-03-21 16:30:45 ════════════
🚨 发现 2 个慢查询:

【慢查询 #1】
  线程ID: 12345 | 连接ID: 67890
  用户: dev@10.148.15.195 | 数据库: shop8267304538112000
  执行时间: 15.3s | 锁等待: 0.2s
  扫描行数: 500000 | 返回行数: 100
  状态: Sending data
  SQL: SELECT * FROM ttpos_order WHERE create_time > 1234567890 ...
  终止命令: KILL 67890;

✅ 钉钉通知已发送
✅ 邮件通知已发送
```

### 通知效果

- **钉钉**：实时推送到群聊，格式化展示慢查询详情
- **邮件**：发送到指定邮箱，便于存档和回溯

## 📚 文档索引

| 文档 | 说明 |
|------|------|
| [README.md](README.md) | 本文档，快速开始 |
| [COMPLETE_GUIDE.md](COMPLETE_GUIDE.md) | ⭐ **完整使用流程**（强烈推荐）|
| [docs/NOTIFICATION_QUICK_START.md](docs/NOTIFICATION_QUICK_START.md) | 5分钟配置通知 |
| [docs/NOTIFICATION_GUIDE.md](docs/NOTIFICATION_GUIDE.md) | 详细通知配置指南 |
| [docs/README_DOCKER.md](docs/README_DOCKER.md) | Docker 部署完整指南 |
| [docs/PERFORMANCE_SCHEMA_SETUP.md](docs/PERFORMANCE_SCHEMA_SETUP.md) | Performance Schema 使用指南 |
| [docs/FEATURES.md](docs/FEATURES.md) | 完整功能特性 |
| [docs/CHANGELOG.md](docs/CHANGELOG.md) | 更新日志 |
| [MIGRATION.md](MIGRATION.md) | 工作区迁移说明 |

## 🎯 适用场景

- ✅ **生产环境监控**：实时监控慢查询，及时发现性能问题
- ✅ **性能优化**：发现需要优化的 SQL，分析查询模式
- ✅ **故障排查**：快速定位"卡死"的查询，提供 KILL 命令
- ✅ **数据库运维**：多数据库集中监控，自动告警通知

## 🏗️ 技术架构

- **语言**：Go 1.23+
- **数据源**：MySQL Performance Schema
- **容器化**：Docker + Docker Compose
- **镜像**：基于 Alpine Linux（~20MB）
- **通知**：钉钉 Webhook + SMTP 邮件
- **部署**：独立容器运行，非 root 用户

## 📦 项目结构

```
mysql-monitor/
├── README.md                           # 主文档（本文件）
├── docker-compose.yml                  # Docker Compose 配置
├── Dockerfile                          # Docker 镜像构建
├── Makefile                            # 便捷管理命令
├── .env.example                        # 配置模板
├── main/                               # Go 源码目录
│   └── performance_schema_monitor.go   # 监控程序源码
├── scripts/                            # 脚本目录
│   ├── quick_start.sh                  # 一键启动脚本
│   ├── check_slow_queries.sh           # 快速检查脚本
│   ├── deploy_to_monitoring_server.sh  # 独立服务器部署脚本
│   ├── test_monitor.sh                 # 测试脚本
│   └── verify.sh                       # 验证脚本
└── docs/                               # 文档目录
    ├── NOTIFICATION_QUICK_START.md     # 通知快速配置
    ├── NOTIFICATION_GUIDE.md           # 通知详细指南
    ├── README_DOCKER.md                # Docker 部署指南
    ├── PERFORMANCE_SCHEMA_SETUP.md     # Performance Schema 指南
    ├── FEATURES.md                     # 功能特性
    └── CHANGELOG.md                    # 更新日志
```

## 🔍 故障排查

### 容器无法启动

```bash
# 查看日志
make logs-prod

# 检查容器状态
docker ps -a | grep monitor
```

### 无法连接数据库

```bash
# 测试网络连接
ping your-mysql-host

# 检查环境变量
docker inspect mysql-monitor-prod | grep -A 20 "Env"
```

### 通知不工作

```bash
# 查看日志确认配置
make logs-prod | grep "通知方式"

# 详细错误信息
make logs-prod | grep -E "钉钉|邮件"
```

**详细故障排查**：[docs/README_DOCKER.md](docs/README_DOCKER.md)

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License

## 🙏 致谢

- MySQL Performance Schema 官方文档
- Go 社区
- Docker 社区

---

## ⚡ 下一步

1. **配置数据库连接**：编辑 `.env` 文件
2. **配置通知**：[5分钟配置指南](docs/NOTIFICATION_QUICK_START.md)
3. **启动监控**：运行 `./scripts/quick_start.sh`
4. **查看日志**：运行 `make logs-prod`
5. **测试告警**：执行 `SELECT SLEEP(15);` 触发慢查询

**有问题？** 查看 [docs/README_DOCKER.md](docs/README_DOCKER.md) 获取详细帮助。
