# MySQL 监控工具完整使用流程

> 从零开始到生产部署的完整指南

## 📋 目录

- [1. 环境准备](#1-环境准备)
- [2. 获取工具](#2-获取工具)
- [3. 配置数据库](#3-配置数据库)
- [4. 配置通知](#4-配置通知)
- [5. 本地测试](#5-本地测试)
- [6. 生产部署](#6-生产部署)
- [7. 日常运维](#7-日常运维)
- [8. 故障处理](#8-故障处理)
- [9. 进阶配置](#9-进阶配置)
- [10. 最佳实践](#10-最佳实践)

---

## 1. 环境准备

### 1.1 检查系统要求

```bash
# 操作系统：Linux/macOS/Windows（WSL2）
uname -a

# 最小配置
# - CPU: 1 核
# - 内存: 512MB
# - 磁盘: 1GB
```

### 1.2 安装 Docker

#### Ubuntu/Debian

```bash
# 安装 Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# 启动 Docker
sudo systemctl start docker
sudo systemctl enable docker

# 添加当前用户到 docker 组
sudo usermod -aG docker $USER
newgrp docker

# 验证安装
docker --version
docker-compose --version
```

#### macOS

```bash
# 下载并安装 Docker Desktop
# https://www.docker.com/products/docker-desktop

# 启动 Docker Desktop

# 验证
docker --version
docker-compose --version
```

#### CentOS/RHEL

```bash
# 安装 Docker
sudo yum install -y yum-utils
sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo yum install -y docker-ce docker-ce-cli containerd.io

# 启动
sudo systemctl start docker
sudo systemctl enable docker

# 验证
docker --version
```

### 1.3 检查网络连接

```bash
# 测试能否访问目标数据库
ping -c 3 your-mysql-host

# 测试端口（如果有 nc/telnet）
nc -zv your-mysql-host 58888

# 或使用 telnet
telnet your-mysql-host 58888
```

---

## 2. 获取工具

### 2.1 从 Git 仓库获取

```bash
# 克隆整个项目
git clone <repository-url>
cd ttpos-server-go/mysql-monitor

# 或只获取监控工具目录
# 如果已经在项目中
cd mysql-monitor
```

### 2.2 验证文件完整性

```bash
# 运行验证脚本
./scripts/verify.sh

# 期望输出：
# ✅ 所有检查通过！工作区结构正确。
```

### 2.3 了解项目结构

```bash
# 查看主文档
cat README.md

# 查看帮助
make help

# 查看配置模板
cat .env.example
```

---

## 3. 配置数据库

### 3.1 准备数据库信息

收集以下信息：

| 项目 | 说明 | 示例 |
|------|------|------|
| 主机地址 | MySQL 服务器 IP/域名 | your-mysql-host |
| 端口 | MySQL 端口 | 58888 |
| 用户名 | 有权限的用户 | dev |
| 密码 | 用户密码 | your-password |
| 数据库 | performance_schema | performance_schema |

### 3.2 检查数据库权限

```bash
# 连接数据库测试
mysql -h your-mysql-host -P 58888 -u dev -p

# 在 MySQL 中执行
USE performance_schema;
SELECT * FROM setup_consumers WHERE NAME LIKE 'events_statements%';

# 期望输出：events_statements_current 为 YES
```

### 3.3 如果 Performance Schema 未启用

联系 DBA 执行：

```sql
-- 1. 检查是否启用
SHOW VARIABLES LIKE 'performance_schema';

-- 2. 如果未启用，需要在 my.cnf 中添加并重启
-- [mysqld]
-- performance_schema = ON

-- 3. 启用 events_statements_current
UPDATE performance_schema.setup_consumers
SET ENABLED = 'YES'
WHERE NAME = 'events_statements_current';

-- 4. 验证
SELECT NAME, ENABLED
FROM performance_schema.setup_consumers
WHERE NAME LIKE 'events_statements%';
```

### 3.4 创建配置文件

```bash
# 复制配置模板
cp .env.example .env

# 编辑配置
vi .env
```

### 3.5 配置开发环境数据库（可选）

```bash
# 编辑 .env 文件
vi .env

# 配置开发环境
DEV_MYSQL_HOST=localhost
DEV_MYSQL_PORT=13306
DEV_MYSQL_USER=saas
DEV_MYSQL_PASSWORD=your-password
DEV_MONITOR_INTERVAL=30       # 开发环境可以长一点
DEV_MONITOR_THRESHOLD=30      # 阈值也可以宽松一些

# 开发环境不配置通知（或配置到测试群）
DEV_DINGTALK_WEBHOOK=
DEV_EMAIL_ENABLED=false
```

### 3.6 配置测试环境数据库（可选）

```bash
# 配置测试环境
TEST_MYSQL_HOST=test-mysql-host
TEST_MYSQL_PORT=58888
TEST_MYSQL_USER=root2
TEST_MYSQL_PASSWORD=your-password
TEST_MONITOR_INTERVAL=10
TEST_MONITOR_THRESHOLD=10

# 测试环境配置到测试钉钉群
TEST_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=TEST_TOKEN
TEST_EMAIL_ENABLED=false
```

### 3.7 配置生产环境数据库（必需）

```bash
# 配置生产环境
PROD_MYSQL_HOST=your-mysql-host
PROD_MYSQL_PORT=58888
PROD_MYSQL_USER=dev
PROD_MYSQL_PASSWORD=your-password
PROD_MONITOR_INTERVAL=10      # 生产环境建议 10 秒
PROD_MONITOR_THRESHOLD=10     # 慢查询阈值 10 秒

# 生产环境必须配置通知（后续步骤）
```

---

## 4. 配置通知

### 4.1 配置钉钉通知（推荐）

#### 步骤 1：创建钉钉机器人

1. 打开钉钉 PC 端
2. 选择目标群聊（建议创建专门的告警群）
3. 点击右上角 `...` → `群设置`
4. 点击 `智能群助手` → `添加机器人` → `自定义`
5. 机器人名称：`MySQL 慢查询告警`
6. 安全设置选择：

**方式 A：自定义关键词（简单）**
```
添加关键词：慢查询
```

**方式 B：加签（推荐，更安全）**
```
选择"加签"，系统会生成一个 SEC 开头的密钥
复制密钥备用
```

7. 点击完成，复制 Webhook 地址

#### 步骤 2：配置到 .env

```bash
# 编辑配置文件
vi .env

# 添加钉钉配置
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxxxxxxxxxxxx

# 如果使用了加签，添加密钥
PROD_DINGTALK_SECRET=SECxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# 如果使用关键词，留空即可
PROD_DINGTALK_SECRET=
```

#### 步骤 3：测试钉钉 Webhook

```bash
# 使用 curl 测试
curl -X POST "https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"msgtype":"text","text":{"content":"🧪 测试通知：MySQL 监控工具测试"}}'

# 检查钉钉群是否收到消息
```

### 4.2 配置邮件通知（可选）

#### 步骤 1：准备邮箱

以 QQ 邮箱为例：

1. 登录 [QQ 邮箱](https://mail.qq.com)
2. 设置 → 账户 → POP3/IMAP/SMTP 服务
3. 开启 `SMTP 服务`
4. 点击 `生成授权码`
5. 按提示用手机发送短信
6. 复制生成的授权码（16 位字符）

#### 步骤 2：配置到 .env

```bash
# 编辑配置文件
vi .env

# 启用邮件通知
PROD_EMAIL_ENABLED=true

# 发件人邮箱（你的 QQ 邮箱）
PROD_EMAIL_FROM=123456789@qq.com

# 收件人邮箱（可以多个，逗号分隔）
PROD_EMAIL_TO=admin@company.com,dba@company.com,ops@company.com

# SMTP 配置
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587

# SMTP 认证（使用授权码，不是邮箱密码！）
PROD_EMAIL_USERNAME=123456789@qq.com
PROD_EMAIL_PASSWORD=abcdefghijklmnop  # 这是授权码

# 通知间隔（秒）
PROD_NOTIFY_INTERVAL=300  # 5分钟内只发送一次
```

#### 常用邮箱 SMTP 配置

| 邮箱 | SMTP 地址 | 端口 | 说明 |
|------|----------|------|------|
| QQ | smtp.qq.com | 587 | 需要授权码 |
| 163 | smtp.163.com | 465 | 需要授权码 |
| Gmail | smtp.gmail.com | 587 | 需要应用密码 |
| 腾讯企业邮 | smtp.exmail.qq.com | 465 | 企业邮箱密码 |
| 阿里云 | smtp.aliyun.com | 465 | 企业邮箱密码 |

### 4.3 配置通知间隔

```bash
# 避免告警风暴，设置通知间隔

# 开发环境：10 分钟
DEV_NOTIFY_INTERVAL=600

# 测试环境：5 分钟
TEST_NOTIFY_INTERVAL=300

# 生产环境：3 分钟（推荐）
PROD_NOTIFY_INTERVAL=180
```

---

## 5. 本地测试

### 5.1 构建镜像

```bash
# 构建 Docker 镜像
make build

# 或使用 docker-compose
docker-compose build

# 查看镜像
docker images | grep monitor
```

### 5.2 启动测试

```bash
# 仅启动生产环境监控（用于测试）
make up-prod

# 或
docker-compose up -d monitor-prod
```

### 5.3 查看日志

```bash
# 实时查看日志
make logs-prod

# 或
docker-compose logs -f monitor-prod

# 期望看到：
# ╔════════════════════════════════════════════════════════════════╗
# ║         Performance Schema 实时监控工具                         ║
# ╚════════════════════════════════════════════════════════════════╝
#
# 监控间隔: 10s | 慢查询阈值: 10s | Ctrl+C 退出
# 通知方式: 钉钉、邮件 | 通知间隔: 180s
```

### 5.4 测试数据库连接

如果日志显示连接失败：

```bash
# 检查容器状态
docker ps -a | grep monitor

# 查看详细日志
docker logs mysql-monitor-prod

# 进入容器测试
docker exec -it mysql-monitor-prod /bin/sh

# 在容器内测试
ping -c 3 your-mysql-host
```

### 5.5 测试慢查询检测

连接到数据库执行慢查询：

```bash
# 方式 1：连接数据库执行
mysql -h your-mysql-host -P 58888 -u dev -p

# 执行慢查询（15 秒）
SELECT SLEEP(15);

# 方式 2：降低阈值测试
# 编辑 .env
PROD_MONITOR_THRESHOLD=1  # 改为 1 秒

# 重启监控
make restart-prod

# 执行任意查询
SELECT * FROM performance_schema.setup_consumers;
```

### 5.6 测试通知功能

执行慢查询后，检查：

1. **钉钉群**：是否收到告警消息
2. **邮箱**：是否收到告警邮件
3. **控制台日志**：
   ```
   🚨 发现 1 个慢查询:
   ...
   ✅ 钉钉通知已发送
   ✅ 邮件通知已发送
   ```

### 5.7 测试通过后停止

```bash
# 停止测试容器
make stop

# 或
docker-compose stop
```

---

## 6. 生产部署

### 6.1 选择部署方式

#### 方式 A：部署到独立监控服务器（推荐）

```bash
# 1. 打包整个目录
cd ..
tar -czf mysql-monitor.tar.gz mysql-monitor/

# 2. 上传到监控服务器
scp mysql-monitor.tar.gz user@monitor-server:/opt/

# 3. SSH 到监控服务器
ssh user@monitor-server

# 4. 解压
cd /opt
tar -xzf mysql-monitor.tar.gz
cd mysql-monitor

# 5. 配置（复制本地的 .env 文件，或重新配置）
vi .env

# 6. 启动
./scripts/quick_start.sh
```

#### 方式 B：部署到应用服务器

```bash
# 如果监控服务器和应用服务器是同一台
cd mysql-monitor
./scripts/quick_start.sh
```

### 6.2 配置系统服务（可选）

如果希望开机自启动：

```bash
# Docker Compose 容器配置了 restart: unless-stopped
# 已经会自动重启，无需额外配置

# 如果需要确保 Docker 服务开机启动
sudo systemctl enable docker
```

### 6.3 启动所有环境监控

```bash
# 启动所有环境（dev, test, prod）
make up

# 或只启动生产
make up-prod
```

### 6.4 验证部署

```bash
# 检查容器状态
make status

# 期望输出：
# NAME                  STATE    PORTS
# mysql-monitor-prod    Up       (healthy)

# 查看日志
make logs-prod

# 测试慢查询
mysql -h your-mysql-host -P 58888 -u dev -p -e "SELECT SLEEP(15);"

# 检查通知
# - 钉钉群收到消息
# - 邮箱收到邮件
```

### 6.5 配置防火墙（如果需要）

```bash
# 监控工具是出站连接，通常不需要开放端口
# 但如果有严格的防火墙策略，确保允许：

# 出站到 MySQL（58888）
# 出站到钉钉 API（443）
# 出站到 SMTP 服务器（587 或 465）
```

---

## 7. 日常运维

### 7.1 查看监控状态

```bash
# 查看容器状态
make status

# 查看最近日志
make logs-tail

# 查看实时日志
make logs-prod
```

### 7.2 查看历史日志

```bash
# Docker 日志（最近 100 行）
docker logs --tail 100 mysql-monitor-prod

# 查看特定时间的日志
docker logs --since "2026-03-21T10:00:00" mysql-monitor-prod

# 导出日志到文件
docker logs mysql-monitor-prod > monitor-prod.log
```

### 7.3 重启监控

```bash
# 重启特定环境
make restart-prod

# 重启所有
make restart
```

### 7.4 更新配置

```bash
# 1. 编辑配置
vi .env

# 2. 应用配置（不重新构建）
make update-config

# 3. 或重新构建并应用（如果修改了代码）
make update
```

### 7.5 查看资源占用

```bash
# 查看容器资源使用
docker stats mysql-monitor-prod

# 期望：
# - CPU: < 5%
# - 内存: 50-100MB
# - 网络: < 1KB/s
```

### 7.6 备份配置

```bash
# 定期备份 .env 文件
cp .env .env.backup.$(date +%Y%m%d)

# 或加入到备份计划
# crontab -e
# 0 2 * * * cp /opt/mysql-monitor/.env /backup/mysql-monitor.env.$(date +\%Y\%m\%d)
```

---

## 8. 故障处理

### 8.1 容器无法启动

**症状**：`docker-compose up` 失败

**排查步骤**：

```bash
# 1. 查看容器状态
docker-compose ps

# 2. 查看详细日志
docker-compose logs monitor-prod

# 3. 常见原因：

# 配置文件错误
cat .env | grep PROD_

# 端口冲突
netstat -tunlp | grep 3306

# Docker 服务问题
sudo systemctl status docker
```

### 8.2 无法连接数据库

**症状**：日志显示 "连接失败" 或 "Ping 失败"

**排查步骤**：

```bash
# 1. 测试网络连接
ping your-mysql-host

# 2. 测试端口
nc -zv your-mysql-host 58888

# 3. 测试账号密码
mysql -h your-mysql-host -P 58888 -u dev -p

# 4. 检查防火墙
sudo iptables -L

# 5. 检查环境变量
docker inspect mysql-monitor-prod | grep -A 10 "Env"
```

### 8.3 通知不工作

#### 钉钉通知失败

```bash
# 1. 查看日志
docker logs mysql-monitor-prod | grep "钉钉"

# 2. 测试 Webhook
curl -X POST "YOUR_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{"msgtype":"text","text":{"content":"测试"}}'

# 3. 检查关键词设置
# 如果配置了关键词，确保通知内容包含关键词（如"慢查询"）

# 4. 检查加签
# 如果配置了加签，确保 DINGTALK_SECRET 正确
```

#### 邮件通知失败

```bash
# 1. 查看日志
docker logs mysql-monitor-prod | grep "邮件"

# 2. 检查常见错误

# 535 Error: authentication failed
# 原因：密码错误，确认使用的是授权码而不是邮箱密码

# 530 Must issue a STARTTLS
# 原因：端口配置错误，QQ 邮箱应该用 587

# 554 DT:SPM
# 原因：QQ 邮箱判定为垃圾邮件，更换内容或使用其他邮箱

# 3. 测试 SMTP 连接
docker exec -it mysql-monitor-prod /bin/sh
apk add busybox-extras
telnet smtp.qq.com 587
```

### 8.4 监控延迟过高

**症状**：慢查询已经执行很久才收到告警

**解决方案**：

```bash
# 减小监控间隔
vi .env
PROD_MONITOR_INTERVAL=5  # 改为 5 秒

# 重启应用配置
make restart-prod
```

### 8.5 告警风暴

**症状**：短时间收到大量重复告警

**解决方案**：

```bash
# 增加通知间隔
vi .env
PROD_NOTIFY_INTERVAL=600  # 改为 10 分钟

# 或临时停止通知
vi .env
PROD_DINGTALK_WEBHOOK=
PROD_EMAIL_ENABLED=false

# 重启
make restart-prod
```

### 8.6 容器占用资源过高

**症状**：CPU 或内存占用异常

**解决方案**：

```bash
# 1. 检查是否是监控间隔太短
vi .env
PROD_MONITOR_INTERVAL=10  # 不要低于 5 秒

# 2. 限制容器资源（编辑 docker-compose.yml）
vi docker-compose.yml

# 添加资源限制
deploy:
  resources:
    limits:
      cpus: '0.5'
      memory: 128M

# 3. 重启
make down
make up-prod
```

---

## 9. 进阶配置

### 9.1 监控多个数据库

```bash
# 方式 1：使用预定义的 dev/test/prod
make up  # 启动所有三个环境

# 方式 2：添加自定义监控实例
vi docker-compose.yml

# 添加新服务（复制 monitor-prod 并修改）
services:
  monitor-custom:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: mysql-monitor-custom
    restart: unless-stopped
    environment:
      MONITOR_NAME: "Custom Database"
      MYSQL_HOST: ${CUSTOM_MYSQL_HOST}
      MYSQL_PORT: ${CUSTOM_MYSQL_PORT}
      # ... 其他配置

# 添加配置到 .env
vi .env
CUSTOM_MYSQL_HOST=your-host
CUSTOM_MYSQL_PORT=3306
CUSTOM_MYSQL_USER=your-user
CUSTOM_MYSQL_PASSWORD=your-password
# ...

# 启动
docker-compose up -d monitor-custom
```

### 9.2 集成到 Grafana

```bash
# 1. 配置 Docker 日志驱动为 Loki
vi docker-compose.yml

logging:
  driver: loki
  options:
    loki-url: "http://loki:3100/loki/api/v1/push"

# 2. 在 Grafana 中创建告警规则
# 基于日志内容 "🚨 发现" 触发告警

# 3. 配置通知渠道
# Grafana → Alerting → Contact points
```

### 9.3 自定义告警模板

编辑 `main/performance_schema_monitor.go`：

```go
// 修改通知内容格式
func checkLongQueries(db *sql.DB) {
    // ...
    slowQueryText.WriteString(fmt.Sprintf("🚨 【紧急】数据库慢查询告警\n\n"))
    slowQueryText.WriteString(fmt.Sprintf("环境: 生产环境\n"))
    slowQueryText.WriteString(fmt.Sprintf("严重程度: 高\n"))
    // ...
}
```

重新构建：

```bash
make build
make restart-prod
```

### 9.4 配置告警静默时间

如果希望在特定时间段不发送通知（如凌晨维护时段）：

```bash
# 方式 1：临时停止通知
make stop

# 方式 2：修改代码增加静默时段判断
# 编辑 main/performance_schema_monitor.go
# 在 sendNotification 函数中添加时间判断

# 方式 3：使用 cron 定时停止/启动
crontab -e

# 每天凌晨 1-3 点停止
0 1 * * * cd /opt/mysql-monitor && make stop
0 3 * * * cd /opt/mysql-monitor && make up-prod
```

### 9.5 配置分级告警

根据慢查询严重程度发送不同级别的通知：

```bash
# 在 .env 中配置多个阈值
vi .env

# 警告级别：10 秒
PROD_MONITOR_THRESHOLD=10

# 严重级别：30 秒（需要修改代码支持）
PROD_CRITICAL_THRESHOLD=30

# 不同级别发送到不同群/邮箱
PROD_DINGTALK_WEBHOOK_WARNING=xxx
PROD_DINGTALK_WEBHOOK_CRITICAL=xxx
```

---

## 10. 最佳实践

### 10.1 部署建议

✅ **推荐做法**：

1. **独立部署**：部署到独立的监控服务器，与数据库和应用分离
2. **多环境监控**：dev/test/prod 都配置监控
3. **双重通知**：钉钉 + 邮件，确保告警不遗漏
4. **合理阈值**：
   - 开发环境：30 秒
   - 测试环境：15 秒
   - 生产环境：10 秒
5. **适度间隔**：
   - 监控间隔：10 秒
   - 通知间隔：3-5 分钟

❌ **避免做法**：

1. 不要部署到数据库服务器本身
2. 不要设置过短的监控间隔（< 5 秒）
3. 不要禁用生产环境通知
4. 不要使用邮箱密码（应该用授权码）
5. 不要忽略测试步骤

### 10.2 安全建议

1. **配置文件安全**：
```bash
# .env 文件包含密码，设置严格权限
chmod 600 .env

# 不要提交到 Git
git add .gitignore  # 确保 .env 在 .gitignore 中
```

2. **钉钉机器人安全**：
```bash
# 使用加签而不是关键词
PROD_DINGTALK_SECRET=SECxxx

# 定期更换密钥
```

3. **数据库账号安全**：
```bash
# 使用只读账号
GRANT SELECT ON performance_schema.* TO 'monitor'@'%';

# 定期更换密码
```

4. **网络安全**：
```bash
# 限制监控服务器的访问
# 只允许访问必要的端口
# 配置防火墙规则
```

### 10.3 维护建议

1. **定期检查**：
```bash
# 每周检查一次
# - 容器状态
# - 日志大小
# - 资源占用
# - 通知是否正常

# 可以写成脚本定期执行
./weekly-check.sh
```

2. **日志管理**：
```bash
# 日志已配置自动轮转
# 定期清理旧日志
docker system prune -a

# 导出重要日志
docker logs mysql-monitor-prod > /backup/logs/$(date +%Y%m%d).log
```

3. **监控告警本身**：
```bash
# 如果长期没有收到任何告警（包括正常的"没有慢查询"），
# 检查监控是否正常运行

# 定期测试
SELECT SLEEP(15);
```

4. **文档更新**：
```bash
# 记录每次配置变更
echo "$(date) - 修改通知间隔为 10 分钟" >> CHANGES.log
```

### 10.4 性能优化

1. **监控开销**：Performance Schema 本身有 5-10% 的开销
2. **合理配置**：
   - 监控间隔不要太频繁
   - 阈值设置合理
   - 限制容器资源

3. **数据库优化**：
```sql
-- 如果监控开销过大，可以只启用必要的 consumers
UPDATE performance_schema.setup_consumers
SET ENABLED = 'NO'
WHERE NAME = 'events_statements_history_long';
```

### 10.5 团队协作

1. **告警群管理**：
   - 创建专门的告警群
   - 添加相关人员（DBA、开发、运维）
   - 制定告警响应流程

2. **责任明确**：
   - 定义谁负责响应告警
   - 定义响应时间（如 5 分钟内）
   - 定义升级流程

3. **知识共享**：
   - 将本文档分享给团队
   - 记录常见问题和解决方案
   - 定期培训

### 10.6 监控指标

定期审查以下指标：

1. **慢查询数量**：趋势是增加还是减少
2. **慢查询类型**：哪些 SQL 最常见
3. **响应时间**：从发现到处理的时间
4. **误报率**：是否有太多不需要处理的告警

---

## 🎯 快速检查清单

### 部署前

- [ ] Docker 已安装并运行
- [ ] 能访问目标数据库
- [ ] Performance Schema 已启用
- [ ] 数据库账号有权限
- [ ] 配置文件已创建（.env）
- [ ] 数据库连接信息已配置
- [ ] 钉钉机器人已创建（如果需要）
- [ ] 邮箱授权码已获取（如果需要）
- [ ] 通知配置已完成
- [ ] 本地测试已通过

### 部署后

- [ ] 容器正常运行
- [ ] 日志无错误
- [ ] 慢查询检测正常
- [ ] 钉钉通知收到（如果配置）
- [ ] 邮件通知收到（如果配置）
- [ ] 资源占用正常
- [ ] 监控服务器可访问
- [ ] 告警群已通知相关人员
- [ ] 文档已分享给团队
- [ ] 响应流程已建立

### 日常维护

- [ ] 每周检查容器状态
- [ ] 每月审查慢查询统计
- [ ] 定期测试通知渠道
- [ ] 定期备份配置文件
- [ ] 关注监控工具更新
- [ ] 记录配置变更
- [ ] 处理告警并跟踪
- [ ] 优化慢查询

---

## 📞 获取帮助

**查看文档**：
```bash
# 主文档
cat README.md

# 详细文档
ls -l docs/

# 在线查看
make help
```

**验证工具**：
```bash
./scripts/verify.sh
```

**日志调试**：
```bash
make logs-prod
docker logs mysql-monitor-prod
```

**社区支持**：
- GitHub Issues
- 团队内部技术群
- DBA 团队

---

## 🎉 总结

完成以上流程后，你已经：

✅ 成功部署了 MySQL 慢查询实时监控工具
✅ 配置了钉钉和邮件通知
✅ 掌握了日常运维和故障处理
✅ 了解了最佳实践和进阶配置

**下一步**：

1. 持续观察监控效果
2. 根据实际情况调整配置
3. 优化发现的慢查询
4. 分享经验给团队

**记住**：监控是手段，优化才是目的！
