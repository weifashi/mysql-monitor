# 监控工具更新日志 - 通知功能

## 🎉 新功能：钉钉和邮件通知

版本：v2.0
日期：2026-03-21

### ✨ 功能概述

监控工具现在支持自动发送告警通知，当检测到慢查询时，可以通过以下方式通知相关人员：

- **钉钉机器人**：发送到钉钉群聊
- **邮件通知**：发送到指定邮箱
- **告警去重**：通知间隔控制，避免告警风暴

### 📋 更新内容

#### 1. 代码更新

- `main/performance_schema_monitor.go`
  - 新增钉钉通知功能
  - 新增邮件通知功能
  - 新增通知间隔控制（避免重复告警）
  - 支持从环境变量读取通知配置
  - 优化通知内容格式

#### 2. 配置文件更新

- `.env.monitor.example`
  - 新增钉钉配置项（`DINGTALK_WEBHOOK`, `DINGTALK_SECRET`）
  - 新增邮件配置项（`EMAIL_*` 系列）
  - 新增通知间隔配置（`NOTIFY_INTERVAL`）

- `docker-compose.yml`
  - 添加所有通知相关环境变量
  - 支持三个环境（dev/test/prod）独立配置通知

#### 3. 文档更新

- 新增 `NOTIFICATION_GUIDE.md` - 详细通知配置指南
- 新增 `NOTIFICATION_QUICK_START.md` - 5分钟快速配置
- 更新 `README_DOCKER.md` - 添加通知配置说明

### 🚀 快速使用

#### 步骤 1：配置钉钉（推荐）

```bash
# 1. 创建钉钉机器人，获取 Webhook
# 2. 编辑配置文件
vi .env.monitor

# 3. 添加 Webhook
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN
```

#### 步骤 2：重启监控

```bash
cd mysql-monitor
make restart-prod
```

#### 步骤 3：测试

```sql
-- 执行慢查询触发告警
SELECT SLEEP(15);
```

### 📊 通知示例

**钉钉通知效果**：

```
🚨 数据库慢查询告警

数据库: your-mysql-host:58888
时间: 2026-03-21 16:30:45
慢查询数量: 2

【慢查询 #1】
线程ID: 12345 | 连接ID: 67890
用户: dev@10.148.15.195 | 数据库: shop8267304538112000
执行时间: 15.3s | 锁等待: 0.2s
扫描行数: 500000 | 返回行数: 100
状态: Sending data
SQL: SELECT * FROM ttpos_order WHERE ...
终止: KILL 67890;
```

### 🔧 配置参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `DINGTALK_WEBHOOK` | 钉钉 Webhook URL | （空）|
| `DINGTALK_SECRET` | 钉钉签名密钥 | （空）|
| `EMAIL_ENABLED` | 启用邮件通知 | false |
| `EMAIL_FROM` | 发件人邮箱 | （空）|
| `EMAIL_TO` | 收件人邮箱（逗号分隔）| （空）|
| `EMAIL_SMTP_HOST` | SMTP 服务器 | （空）|
| `EMAIL_SMTP_PORT` | SMTP 端口 | 587 |
| `EMAIL_USERNAME` | SMTP 用户名 | （空）|
| `EMAIL_PASSWORD` | SMTP 密码/授权码 | （空）|
| `NOTIFY_INTERVAL` | 通知间隔（秒） | 300 |

### 📖 详细文档

- **快速开始**：[NOTIFICATION_QUICK_START.md](NOTIFICATION_QUICK_START.md)
- **完整指南**：[NOTIFICATION_GUIDE.md](NOTIFICATION_GUIDE.md)
- **Docker 部署**：[README_DOCKER.md](README_DOCKER.md)

### 🔍 常见问题

**Q: 如何只配置钉钉或邮件？**

A: 只需配置对应的环境变量即可，未配置的通知方式不会启用。

**Q: 通知间隔是什么？**

A: 避免短时间内重复发送相同告警。默认 5 分钟内只发送一次。

**Q: 如何测试通知？**

A: 执行 `SELECT SLEEP(15);` 慢查询，或临时降低阈值到 1 秒。

**Q: 支持其他通知方式吗？**

A: 当前支持钉钉和邮件。如需其他方式（如企业微信、Slack），可参考代码自行扩展。

### 🎯 推荐配置

**开发环境**：
- 不配置通知，仅控制台输出
- 或配置到测试钉钉群

**测试环境**：
- 配置钉钉通知到测试群
- 通知间隔：5-10 分钟

**生产环境**：
- **必须配置**钉钉和邮件双重通知
- 通知间隔：3-5 分钟
- 配置多个收件人确保有人收到

### 🚨 注意事项

1. 生产环境**强烈建议**配置通知，否则无法及时发现问题
2. 钉钉机器人建议配置"加签"，更安全
3. 邮箱密码使用**授权码**，不是邮箱密码
4. 通知间隔不要设置太短，避免告警风暴
5. 定期测试通知渠道是否畅通

### 🔄 升级指南

如果你已经在使用旧版本监控工具：

```bash
cd mysql-monitor

# 1. 备份旧配置
cp .env.monitor .env.monitor.bak

# 2. 更新配置模板
cp .env.monitor.example .env.monitor.new

# 3. 合并配置（手动复制数据库配置，添加通知配置）
vi .env.monitor

# 4. 重新构建镜像
make build

# 5. 重启服务
make restart-prod
```

### 📈 未来计划

- [ ] 支持企业微信通知
- [ ] 支持 Slack 通知
- [ ] 支持 Webhook 自定义通知
- [ ] 支持通知内容模板定制
- [ ] 支持告警等级分级
- [ ] 支持告警静默时间段
- [ ] 通知发送历史记录

### 🤝 反馈

如有问题或建议，请在项目中提 Issue 或联系开发团队。
