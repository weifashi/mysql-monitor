# MySQL Performance Schema 监控工具 - Docker 部署指南

## 📦 文件清单

| 文件 | 说明 |
|------|------|
| `docker-compose.yml` | Docker Compose 配置文件 |
| `Dockerfile.monitor` | 监控工具 Docker 镜像构建文件 |
| `.env.monitor.example` | 环境变量配置模板 |
| `Makefile` | Make 命令管理工具 |
| `scripts/quick_start.sh` | 一键启动脚本 |
| `DOCKER_DEPLOY.md` | 详细部署文档 |

## 🚀 三种启动方式

### 方式 1：一键启动（推荐新手）

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

### 方式 2：使用 Makefile（推荐日常使用）

```bash
cd mysql-monitor

# 首次使用：初始化配置
make init

# 编辑配置
vi .env.monitor

# 启动所有监控
make up

# 或者只启动特定环境
make up-prod

# 查看帮助
make help
```

### 方式 3：直接使用 Docker Compose

```bash
cd mysql-monitor

# 复制配置
cp .env.monitor.example .env.monitor

# 编辑配置
vi .env.monitor

# 构建镜像
docker-compose -f docker-compose.yml build

# 启动服务
docker-compose -f docker-compose.yml up -d

# 查看日志
docker-compose -f docker-compose.yml logs -f
```

## 📋 配置说明

### 1. 创建配置文件

```bash
cd mysql-monitor
cp .env.monitor.example .env.monitor
```

### 2. 编辑数据库连接

每个环境配置格式：

```bash
# 生产环境
PROD_MYSQL_HOST=your-mysql-host
PROD_MYSQL_PORT=58888
PROD_MYSQL_USER=dev
PROD_MYSQL_PASSWORD=your-password
PROD_MONITOR_INTERVAL=10    # 检查间隔（秒）
PROD_MONITOR_THRESHOLD=10   # 慢查询阈值（秒）
```

### 3. 配置通知（可选）

#### 钉钉通知

```bash
# 钉钉 Webhook（必填）
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN

# 钉钉加签密钥（可选，更安全）
PROD_DINGTALK_SECRET=SECxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

#### 邮件通知

```bash
# 启用邮件通知
PROD_EMAIL_ENABLED=true

# 发件人邮箱
PROD_EMAIL_FROM=monitor@example.com

# 收件人邮箱（多个用逗号分隔）
PROD_EMAIL_TO=admin@example.com,ops@example.com

# SMTP 配置
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=monitor@example.com
PROD_EMAIL_PASSWORD=your-authorization-code  # 授权码
```

#### 通知间隔

```bash
# 通知间隔（秒），避免告警风暴
PROD_NOTIFY_INTERVAL=300
```

**详细配置指南**：查看 [NOTIFICATION_GUIDE.md](NOTIFICATION_GUIDE.md)

### 4. 调整监控参数

| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `MONITOR_INTERVAL` | 检查间隔 | 生产: 10s, 开发: 30s |
| `MONITOR_THRESHOLD` | 慢查询阈值 | 生产: 10s, 开发: 30s |

## 🛠️ 常用命令

### 使用 Makefile

```bash
# 查看所有命令
make help

# 服务管理
make up          # 启动所有
make up-prod     # 启动生产
make stop        # 停止
make restart     # 重启
make down        # 停止并删除

# 日志查看
make logs        # 所有日志
make logs-prod   # 生产日志
make logs-tail   # 最近100行

# 状态检查
make status      # 查看状态

# 更新
make update      # 更新代码并重建
make update-config  # 只更新配置

# 清理
make clean       # 清理所有资源
```

### 使用 Docker Compose

```bash
# 服务管理
docker-compose -f docker-compose.yml up -d          # 启动
docker-compose -f docker-compose.yml up -d monitor-prod  # 启动特定服务
docker-compose -f docker-compose.yml stop          # 停止
docker-compose -f docker-compose.yml restart       # 重启
docker-compose -f docker-compose.yml down          # 停止并删除

# 日志查看
docker-compose -f docker-compose.yml logs -f       # 实时日志
docker-compose -f docker-compose.yml logs -f monitor-prod  # 特定服务
docker-compose -f docker-compose.yml logs --tail=100  # 最近100行

# 状态检查
docker-compose -f docker-compose.yml ps            # 查看状态

# 重新构建
docker-compose -f docker-compose.yml build         # 构建镜像
docker-compose -f docker-compose.yml up -d --force-recreate  # 重新创建容器
```

## 📊 监控管理

### 查看实时日志

```bash
# 方式 1：使用 Makefile
make logs-prod

# 方式 2：使用 docker-compose
docker-compose -f docker-compose.yml logs -f monitor-prod

# 方式 3：直接使用 docker
docker logs -f mysql-monitor-prod
```

### 检查服务状态

```bash
# 查看所有监控服务
make status

# 或者
docker-compose -f docker-compose.yml ps
```

### 进入容器调试

```bash
# 使用 Makefile
make shell-prod

# 或直接使用 docker
docker exec -it mysql-monitor-prod /bin/sh
```

## 🔧 高级配置

### 添加新的监控实例

1. 编辑 `docker-compose.yml`：

```yaml
services:
  monitor-custom:
    build:
      context: ..
      dockerfile: Dockerfile.monitor
    container_name: mysql-monitor-custom
    restart: unless-stopped
    environment:
      MONITOR_NAME: "Custom Database"
      MYSQL_HOST: ${CUSTOM_MYSQL_HOST}
      MYSQL_PORT: ${CUSTOM_MYSQL_PORT}
      MYSQL_USER: ${CUSTOM_MYSQL_USER}
      MYSQL_PASSWORD: ${CUSTOM_MYSQL_PASSWORD}
      MONITOR_INTERVAL: ${CUSTOM_MONITOR_INTERVAL:-10}
      MONITOR_THRESHOLD: ${CUSTOM_MONITOR_THRESHOLD:-10}
      TZ: Asia/Shanghai
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
    networks:
      - monitor-network
```

2. 在 `.env.monitor` 中添加配置：

```bash
CUSTOM_MYSQL_HOST=your-host
CUSTOM_MYSQL_PORT=3306
CUSTOM_MYSQL_USER=your-user
CUSTOM_MYSQL_PASSWORD=your-password
CUSTOM_MONITOR_INTERVAL=10
CUSTOM_MONITOR_THRESHOLD=10
```

3. 启动新实例：

```bash
docker-compose -f docker-compose.yml up -d monitor-custom
```

### 资源限制

在 `docker-compose.yml` 中添加：

```yaml
services:
  monitor-prod:
    # ... 其他配置
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 128M
        reservations:
          cpus: '0.1'
          memory: 64M
```

### 日志配置

调整日志轮转策略：

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "5m"    # 单个日志文件最大 5MB
    max-file: "2"     # 保留 2 个日志文件
```

## 🔍 故障排查

### 容器无法启动

```bash
# 查看容器日志
docker-compose -f docker-compose.yml logs monitor-prod

# 检查容器状态
docker inspect mysql-monitor-prod
```

### 无法连接数据库

```bash
# 进入容器测试
docker exec -it mysql-monitor-prod /bin/sh

# 测试网络连接
ping -c 3 your-mysql-host

# 测试端口
nc -zv your-mysql-host 58888
```

### 查看配置是否正确

```bash
# 查看容器环境变量
docker inspect mysql-monitor-prod | grep -A 20 "Env"
```

## 📈 集成告警

### 方式 1：解析日志发送告警

创建 `alert_monitor.sh`：

```bash
#!/bin/bash
CONTAINER="mysql-monitor-prod"
WEBHOOK="https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN"

docker logs -f "$CONTAINER" 2>&1 | while read line; do
  if echo "$line" | grep -q "🚨 发现"; then
    curl -X POST "$WEBHOOK" \
      -H "Content-Type: application/json" \
      -d "{\"msgtype\":\"text\",\"text\":{\"content\":\"$line\"}}"
  fi
done
```

运行：

```bash
chmod +x alert_monitor.sh
nohup ./alert_monitor.sh &
```

### 方式 2：使用 Grafana Loki

修改 logging driver：

```yaml
logging:
  driver: loki
  options:
    loki-url: "http://loki:3100/loki/api/v1/push"
```

## 📝 最佳实践

1. **独立部署**: 将监控服务部署到独立的监控服务器（与 Prometheus/Grafana 同机器）
2. **日志管理**: 配置合理的日志轮转策略，避免磁盘占满
3. **资源限制**: 设置 CPU 和内存限制，避免监控服务占用过多资源
4. **告警集成**: 配置自动告警通知（钉钉/Slack/企业微信）
5. **定期检查**: 定期查看日志，确认监控正常运行
6. **备份配置**: 将 `.env.monitor` 加入备份计划（但不要提交到 git）

## 🆘 获取帮助

```bash
# 查看 Makefile 帮助
make help

# 查看详细文档
cat DOCKER_DEPLOY.md

# 查看 Performance Schema 使用指南
cat PERFORMANCE_SCHEMA_SETUP.md
```

## 📌 注意事项

1. **配置文件安全**: `.env.monitor` 包含数据库密码，已加入 `.gitignore`，不要提交到 git
2. **网络连接**: 确保监控服务器能访问目标数据库
3. **数据库权限**: 需要 `SELECT` 权限访问 `performance_schema` 库
4. **资源占用**: 每个监控实例占用约 50-100MB 内存，根据服务器资源调整
5. **时区设置**: 已配置为 `Asia/Shanghai`，可根据需要调整

## ✅ 快速检查清单

启动前检查：
- [ ] Docker 和 Docker Compose 已安装
- [ ] 已创建 `.env.monitor` 配置文件
- [ ] 数据库连接信息正确
- [ ] 网络连通性正常
- [ ] 数据库用户有 Performance Schema 权限

启动后检查：
- [ ] 容器状态为 `Up`
- [ ] 日志无错误信息
- [ ] 能看到周期性的检查输出
- [ ] 慢查询检测正常工作

## 🎯 下一步

1. 启动监控服务
2. 查看日志确认正常运行
3. 配置告警通知（可选）
4. 集成到监控系统（Grafana/Prometheus）
5. 定期审查监控数据
