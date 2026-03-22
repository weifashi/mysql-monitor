# 快速参考手册

## 📊 完整流程概览

```
┌─────────────────────────────────────────────────────────────┐
│                    1. 环境准备 (5-10分钟)                     │
│  ✓ 安装 Docker                                               │
│  ✓ 检查网络连接                                               │
│  ✓ 验证数据库访问                                             │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    2. 获取工具 (1分钟)                        │
│  $ cd mysql-monitor                                          │
│  $ ./scripts/verify.sh              # 验证文件完整性                  │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    3. 配置数据库 (5分钟)                      │
│  $ cp .env.example .env                                      │
│  $ vi .env                                                   │
│                                                              │
│  PROD_MYSQL_HOST=your-mysql-host                              │
│  PROD_MYSQL_PORT=58888                                      │
│  PROD_MYSQL_USER=dev                                        │
│  PROD_MYSQL_PASSWORD=xxxxx                                  │
│  PROD_MONITOR_INTERVAL=10                                   │
│  PROD_MONITOR_THRESHOLD=10                                  │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    4. 配置通知 (10分钟)                       │
│  ┌────────────────┐         ┌────────────────┐              │
│  │  钉钉通知       │    或    │  邮件通知       │              │
│  │                │         │                │              │
│  │ 1. 创建机器人   │         │ 1. 开启SMTP    │              │
│  │ 2. 获取Webhook │         │ 2. 生成授权码  │              │
│  │ 3. 配置到.env  │         │ 3. 配置到.env  │              │
│  └────────────────┘         └────────────────┘              │
│                                                              │
│  PROD_DINGTALK_WEBHOOK=https://...                          │
│  PROD_EMAIL_ENABLED=true                                    │
│  PROD_EMAIL_FROM=123456789@qq.com                           │
│  PROD_EMAIL_TO=admin@company.com                            │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    5. 本地测试 (5分钟)                        │
│  $ make build                # 构建镜像                      │
│  $ make up-prod              # 启动容器                      │
│  $ make logs-prod            # 查看日志                      │
│                                                              │
│  # 测试慢查询检测                                             │
│  mysql> SELECT SLEEP(15);                                   │
│                                                              │
│  # 验证通知                                                   │
│  ✓ 钉钉群收到消息                                             │
│  ✓ 邮箱收到邮件                                               │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    6. 生产部署 (10分钟)                       │
│  方式A: 独立监控服务器 (推荐)                                  │
│  $ scp -r mysql-monitor/ user@monitor-server:/opt/          │
│  $ ssh user@monitor-server                                  │
│  $ cd /opt/mysql-monitor                                    │
│  $ ./scripts/quick_start.sh                                         │
│                                                              │
│  方式B: 应用服务器                                             │
│  $ ./scripts/quick_start.sh                                         │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    7. 验证部署 (2分钟)                        │
│  $ make status               # 检查容器状态                  │
│  $ make logs-prod            # 查看日志                      │
│  mysql> SELECT SLEEP(15);    # 触发测试                      │
│  ✓ 收到钉钉通知                                               │
│  ✓ 收到邮件通知                                               │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    ✅ 部署完成！                              │
│  监控工具已在生产环境运行                                      │
│  开始 24x7 实时监控 MySQL 慢查询                              │
└─────────────────────────────────────────────────────────────┘
```

---

## ⚡ 一键命令速查表

### 初始化和启动

```bash
# 完整流程（一键启动）
./scripts/quick_start.sh

# 或分步执行
make init          # 初始化配置
vi .env            # 编辑配置
make build         # 构建镜像
make up-prod       # 启动生产监控
make logs-prod     # 查看日志
```

### 日常运维

| 命令 | 说明 | 使用场景 |
|------|------|----------|
| `make help` | 查看所有命令 | 忘记命令时 |
| `make status` | 查看容器状态 | 每日检查 |
| `make logs-prod` | 实时日志 | 排查问题 |
| `make logs-tail` | 最近100行日志 | 快速查看 |
| `make restart-prod` | 重启监控 | 应用配置 |
| `make update-config` | 更新配置 | 修改通知 |
| `make stop` | 停止所有 | 维护时 |
| `make up` | 启动所有 | 恢复服务 |

### 故障处理

```bash
# 容器无法启动
docker-compose ps
docker-compose logs monitor-prod

# 无法连接数据库
ping your-mysql-host
nc -zv your-mysql-host 58888
docker exec -it mysql-monitor-prod /bin/sh

# 通知不工作
make logs-prod | grep "钉钉"
make logs-prod | grep "邮件"

# 资源占用过高
docker stats mysql-monitor-prod
```

---

## 📋 配置清单

### 必需配置

```bash
# 数据库连接 (必填)
PROD_MYSQL_HOST=your-mysql-host
PROD_MYSQL_PORT=58888
PROD_MYSQL_USER=dev
PROD_MYSQL_PASSWORD=your-password

# 监控参数 (必填)
PROD_MONITOR_INTERVAL=10      # 检查间隔（秒）
PROD_MONITOR_THRESHOLD=10     # 慢查询阈值（秒）
```

### 通知配置（推荐）

```bash
# 钉钉通知
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxx
PROD_DINGTALK_SECRET=SECxxx  # 可选

# 邮件通知
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=monitor@company.com
PROD_EMAIL_TO=admin@company.com,ops@company.com
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=monitor@company.com
PROD_EMAIL_PASSWORD=your-authorization-code

# 通知间隔
PROD_NOTIFY_INTERVAL=180     # 3分钟（推荐）
```

---

## 🎯 推荐配置值

| 环境 | 监控间隔 | 慢查询阈值 | 通知间隔 | 通知方式 |
|------|---------|-----------|---------|---------|
| **开发** | 30秒 | 30秒 | 10分钟 | 仅日志 |
| **测试** | 10秒 | 15秒 | 5分钟 | 钉钉 |
| **生产** | 10秒 | 10秒 | 3分钟 | 钉钉+邮件 |

---

## 🔍 常见问题 5 秒解决

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 容器启动失败 | 配置错误 | `cat .env` 检查配置 |
| 连接数据库失败 | 网络/权限 | `ping HOST` 和 `mysql -h HOST` 测试 |
| 钉钉通知失败 | Webhook 错误 | `curl POST WEBHOOK` 测试 |
| 邮件发送失败 | 授权码错误 | 使用授权码，不是密码 |
| 告警太频繁 | 间隔太短 | 增加 `NOTIFY_INTERVAL` |
| 无法检测慢查询 | PS 未启用 | 联系 DBA 启用 Performance Schema |

---

## 📞 获取帮助的优先级

```
1. 查看本文档                    ← 90% 的问题可以解决
   ↓
2. 查看 COMPLETE_GUIDE.md       ← 详细步骤和故障排查
   ↓
3. 运行 ./scripts/verify.sh       ← 自动验证工具
   ↓
4. 查看日志 make logs-prod       ← 查看具体错误信息
   ↓
5. 查看 docs/ 目录文档           ← 专项指南
   ↓
6. 联系团队/提 Issue             ← 复杂问题
```

---

## 🎓 5 分钟快速上手

```bash
# 1. 准备配置文件（2分钟）
cd mysql-monitor
cp .env.example .env
vi .env  # 填写数据库信息

# 2. 创建钉钉机器人（2分钟）
# 钉钉群 → 群设置 → 机器人 → 自定义
# 复制 Webhook 到 .env 的 PROD_DINGTALK_WEBHOOK

# 3. 一键启动（1分钟）
./scripts/quick_start.sh

# ✅ 完成！开始监控
```

---

## 📖 文档地图

```
mysql-monitor/
├── README.md                      # 快速开始
├── .env.example                   # 配置模板
├── main/                          # Go 源码
│   └── performance_schema_monitor.go
├── scripts/                       # 脚本
│   ├── quick_start.sh             # 一键启动
│   ├── verify.sh                  # 验证工具
│   └── ...
└── docs/
    ├── NOTIFICATION_QUICK_START.md    # 5分钟配置通知
    ├── NOTIFICATION_GUIDE.md          # 详细通知指南
    ├── README_DOCKER.md               # Docker 部署
    ├── PERFORMANCE_SCHEMA_SETUP.md    # PS 使用指南
    ├── FEATURES.md                    # 功能特性
    └── CHANGELOG.md                   # 更新日志
```

**推荐阅读顺序**：

1. **新手**：README.md → COMPLETE_GUIDE.md → 开始使用
2. **快速部署**：本文档（QUICK_REFERENCE.md）→ 直接开始
3. **配置通知**：NOTIFICATION_QUICK_START.md
4. **深入了解**：FEATURES.md + docs/ 下的其他文档

---

## 💡 记住这 3 条

1. **配置文件使用授权码**，不是邮箱密码
2. **生产环境必须配置通知**，否则无法及时发现问题
3. **定期测试告警**，确保通知渠道畅通

---

## 🎉 现在开始

```bash
cd mysql-monitor
./scripts/quick_start.sh
```

30 秒后，你的 MySQL 监控工具就会开始工作！
