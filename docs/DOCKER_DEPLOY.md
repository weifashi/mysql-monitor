# Docker Compose 部署 MySQL 监控工具

## 快速开始

### 1. 准备配置文件

```bash
cd mysql-monitor
cp .env.monitor.example .env.monitor
```

编辑 `.env.monitor`，配置需要监控的数据库连接信息。

### 2. 构建并启动所有监控服务

```bash
# 启动所有监控实例（dev, test, prod）
docker-compose -f docker-compose.yml up -d

# 或者只启动特定环境
docker-compose -f docker-compose.yml up -d monitor-prod
```

### 3. 查看运行状态

```bash
# 查看所有容器状态
docker-compose -f docker-compose.yml ps

# 查看实时日志（所有监控）
docker-compose -f docker-compose.yml logs -f

# 查看特定监控的日志
docker-compose -f docker-compose.yml logs -f monitor-prod

# 查看最近 100 行日志
docker-compose -f docker-compose.yml logs --tail=100 monitor-prod
```

## 管理命令

### 启动/停止服务

```bash
# 启动所有
docker-compose -f docker-compose.yml start

# 停止所有
docker-compose -f docker-compose.yml stop

# 重启所有
docker-compose -f docker-compose.yml restart

# 重启特定服务
docker-compose -f docker-compose.yml restart monitor-prod
```

### 更新配置

修改 `.env.monitor` 后：

```bash
# 重新创建容器（应用新配置）
docker-compose -f docker-compose.yml up -d --force-recreate

# 或者只重启特定服务
docker-compose -f docker-compose.yml up -d --force-recreate monitor-prod
```

### 更新代码

修改 `main/performance_schema_monitor.go` 后：

```bash
# 重新构建镜像
docker-compose -f docker-compose.yml build

# 重新创建容器
docker-compose -f docker-compose.yml up -d --force-recreate
```

### 完全清理

```bash
# 停止并删除所有容器
docker-compose -f docker-compose.yml down

# 同时删除镜像
docker-compose -f docker-compose.yml down --rmi all

# 同时删除数据卷
docker-compose -f docker-compose.yml down -v
```

## 配置说明

### 环境变量

每个监控实例支持以下环境变量：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MONITOR_NAME` | 监控实例名称（日志标识） | MySQL Monitor |
| `MYSQL_HOST` | MySQL 主机地址 | localhost |
| `MYSQL_PORT` | MySQL 端口 | 3306 |
| `MYSQL_USER` | MySQL 用户名 | dev |
| `MYSQL_PASSWORD` | MySQL 密码 | （空） |
| `MONITOR_INTERVAL` | 检查间隔（秒） | 10 |
| `MONITOR_THRESHOLD` | 慢查询阈值（秒） | 10 |
| `TZ` | 时区 | Asia/Shanghai |

### 日志配置

日志自动轮转配置：
- 最大文件大小：10MB
- 保留文件数：3 个
- 总日志大小上限：30MB

查看容器日志：

```bash
# 方式 1：使用 docker-compose
docker-compose -f docker-compose.yml logs -f monitor-prod

# 方式 2：直接使用 docker
docker logs -f mysql-monitor-prod

# 方式 3：查看 JSON 日志文件（在宿主机上）
# 日志位置：/var/lib/docker/containers/{container-id}/{container-id}-json.log
```

## 添加新的监控实例

### 方式 1：修改 docker-compose.yml

在 `docker-compose.yml` 中添加新服务：

```yaml
services:
  monitor-custom:
    build:
      context: .
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

在 `.env.monitor` 中添加配置：

```bash
CUSTOM_MYSQL_HOST=your-host
CUSTOM_MYSQL_PORT=3306
CUSTOM_MYSQL_USER=your-user
CUSTOM_MYSQL_PASSWORD=your-password
CUSTOM_MONITOR_INTERVAL=10
CUSTOM_MONITOR_THRESHOLD=10
```

启动新实例：

```bash
docker-compose -f docker-compose.yml up -d monitor-custom
```

### 方式 2：使用 docker run（临时监控）

```bash
docker run -d \
  --name mysql-monitor-temp \
  --restart unless-stopped \
  -e MONITOR_NAME="Temp Database" \
  -e MYSQL_HOST=your-host \
  -e MYSQL_PORT=3306 \
  -e MYSQL_USER=your-user \
  -e MYSQL_PASSWORD=your-password \
  -e MONITOR_INTERVAL=10 \
  -e MONITOR_THRESHOLD=10 \
  -e TZ=Asia/Shanghai \
  $(docker build -q -f Dockerfile.monitor .)
```

## 监控告警集成

### 方式 1：解析日志并发送告警

创建告警脚本 `alert_monitor.sh`：

```bash
#!/bin/bash
# 监控容器日志并发送钉钉告警

CONTAINER_NAME="mysql-monitor-prod"
DINGTALK_WEBHOOK="https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN"

docker logs -f "$CONTAINER_NAME" 2>&1 | while read line; do
  if echo "$line" | grep -q "🚨 发现"; then
    curl -X POST "$DINGTALK_WEBHOOK" \
      -H "Content-Type: application/json" \
      -d "{
        \"msgtype\": \"text\",
        \"text\": {
          \"content\": \"🚨 生产数据库慢查询告警\n\n$line\"
        }
      }"
  fi
done
```

后台运行：

```bash
chmod +x alert_monitor.sh
nohup ./alert_monitor.sh > /dev/null 2>&1 &
```

### 方式 2：使用 Grafana Loki 收集日志

配置 Docker 日志驱动为 Loki：

```yaml
services:
  monitor-prod:
    # ... 其他配置
    logging:
      driver: loki
      options:
        loki-url: "http://loki:3100/loki/api/v1/push"
        loki-retries: "5"
        loki-batch-size: "400"
```

在 Grafana 中配置告警规则。

## 故障排查

### 容器无法启动

```bash
# 查看容器日志
docker-compose -f docker-compose.yml logs monitor-prod

# 查看容器详细信息
docker inspect mysql-monitor-prod

# 检查网络连接
docker exec mysql-monitor-prod ping -c 3 your-mysql-host
```

### 无法连接数据库

```bash
# 进入容器测试连接
docker exec -it mysql-monitor-prod /bin/sh

# 在容器内测试（需要先安装 mysql-client）
apk add mysql-client
mysql -h ${MYSQL_HOST} -P ${MYSQL_PORT} -u ${MYSQL_USER} -p${MYSQL_PASSWORD}
```

### 修改监控参数

临时修改（容器重启后失效）：

```bash
# 停止容器
docker stop mysql-monitor-prod

# 使用新参数启动
docker run -d \
  --name mysql-monitor-prod \
  --restart unless-stopped \
  -e MONITOR_INTERVAL=5 \
  -e MONITOR_THRESHOLD=5 \
  # ... 其他参数
  your-image-name
```

永久修改：编辑 `.env.monitor` 后执行：

```bash
docker-compose -f docker-compose.yml up -d --force-recreate monitor-prod
```

## 性能优化

### 减少资源占用

设置容器资源限制：

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

### 优化日志存储

如果日志量过大，调整日志配置：

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "5m"    # 减小单文件大小
    max-file: "2"     # 减少保留文件数
```

## 生产环境建议

1. **独立部署**：将监控服务部署到独立的监控服务器（与 Prometheus/Grafana 同机器）
2. **资源限制**：设置合理的 CPU 和内存限制
3. **日志管理**：配置日志轮转或使用集中日志系统（Loki/ELK）
4. **告警集成**：配置自动告警通知（钉钉/Slack/企业微信）
5. **监控覆盖**：对所有数据库环境（开发/测试/生产）都启用监控
6. **定期检查**：定期查看日志，确认监控正常运行

## 示例：完整部署流程

```bash
# 1. 进入工具目录
cd mysql-monitor

# 2. 复制配置文件
cp .env.monitor.example .env.monitor

# 3. 编辑配置（使用你喜欢的编辑器）
vim .env.monitor

# 4. 构建镜像
docker-compose -f docker-compose.yml build

# 5. 启动所有监控
docker-compose -f docker-compose.yml up -d

# 6. 查看状态
docker-compose -f docker-compose.yml ps

# 7. 查看日志（确认正常运行）
docker-compose -f docker-compose.yml logs -f

# 8. 测试：在数据库执行慢查询，观察监控是否告警
# 9. 配置告警通知（可选）
```

## 常见问题

**Q: 如何只监控生产环境？**

A: 只启动 monitor-prod：
```bash
docker-compose -f docker-compose.yml up -d monitor-prod
```

**Q: 如何临时禁用某个监控？**

A: 停止对应容器：
```bash
docker-compose -f docker-compose.yml stop monitor-dev
```

**Q: 如何查看历史日志？**

A: 使用 `--since` 或 `--until` 参数：
```bash
docker-compose -f docker-compose.yml logs --since 2h monitor-prod
docker-compose -f docker-compose.yml logs --since "2024-03-20T10:00:00" monitor-prod
```

**Q: 容器重启后配置丢失？**

A: 确保使用 `.env.monitor` 文件，不要直接修改容器环境变量。修改后使用 `--force-recreate` 重新创建容器。

**Q: 如何更新 Go 代码？**

A:
```bash
# 1. 修改 main/performance_schema_monitor.go
# 2. 重新构建
docker-compose -f docker-compose.yml build
# 3. 重新创建容器
docker-compose -f docker-compose.yml up -d --force-recreate
```
