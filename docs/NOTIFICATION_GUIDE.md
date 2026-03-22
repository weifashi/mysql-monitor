# MySQL 监控通知配置指南

## 📢 支持的通知方式

- **钉钉机器人**：企业内部群聊通知
- **邮件通知**：发送邮件到指定邮箱

## 🔔 钉钉通知配置

### 1. 创建钉钉机器人

1. 打开钉钉群聊 → 点击右上角 `...` → `群设置` → `智能群助手` → `添加机器人` → `自定义`
2. 填写机器人名称，如 `MySQL 监控告警`
3. 安全设置选择：
   - **方式 1**：自定义关键词（添加关键词：`慢查询` 或 `数据库`）
   - **方式 2**：加签（推荐，更安全）
4. 点击完成，复制 Webhook 地址

### 2. 配置到 .env.monitor

```bash
# 钉钉 Webhook（必填）
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_ACCESS_TOKEN

# 钉钉加签密钥（如果选择了加签，必填）
PROD_DINGTALK_SECRET=SECxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 3. 测试钉钉通知

```bash
# 启动监控
make up-prod

# 查看日志，确认配置已加载
make logs-prod

# 在数据库执行一个慢查询，触发告警
# 例如：SELECT SLEEP(15);
```

### 钉钉通知效果

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
SQL: SELECT * FROM ttpos_order WHERE create_time > 1234567890 ...
终止: KILL 67890;
```

## 📧 邮件通知配置

### 1. 准备邮箱信息

根据你的邮箱提供商获取 SMTP 配置：

#### 常用邮箱 SMTP 配置

| 邮箱提供商 | SMTP 地址 | 端口 | 说明 |
|-----------|----------|------|------|
| Gmail | smtp.gmail.com | 587 | 需要开启"允许不够安全的应用" |
| QQ邮箱 | smtp.qq.com | 587 或 465 | 需要开启 SMTP 服务，使用授权码 |
| 163邮箱 | smtp.163.com | 465 | 需要开启 SMTP 服务，使用授权码 |
| 阿里云邮箱 | smtp.aliyun.com | 465 | 企业邮箱 |
| 腾讯企业邮 | smtp.exmail.qq.com | 465 | 企业邮箱 |
| Outlook | smtp.office365.com | 587 | 需要应用密码 |

#### QQ 邮箱示例（推荐）

1. 登录 QQ 邮箱 → 设置 → 账户
2. 找到 `POP3/IMAP/SMTP/Exchange/CardDAV/CalDAV服务`
3. 开启 `SMTP 服务`
4. 生成授权码（点击"生成授权码"，按提示操作）
5. 复制授权码，作为 `EMAIL_PASSWORD` 使用

### 2. 配置到 .env.monitor

```bash
# 启用邮件通知
PROD_EMAIL_ENABLED=true

# 发件人邮箱
PROD_EMAIL_FROM=monitor@example.com

# 收件人邮箱（多个用逗号分隔）
PROD_EMAIL_TO=admin@example.com,dev@example.com

# SMTP 服务器
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587

# SMTP 认证信息
PROD_EMAIL_USERNAME=monitor@example.com
PROD_EMAIL_PASSWORD=your-authorization-code  # 授权码，不是邮箱密码
```

#### QQ 邮箱完整示例

```bash
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=123456789@qq.com
PROD_EMAIL_TO=admin@company.com,ops@company.com
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=123456789@qq.com
PROD_EMAIL_PASSWORD=abcdefghijklmnop  # QQ 邮箱授权码
```

### 3. 测试邮件通知

```bash
# 重启监控应用新配置
make restart-prod

# 查看日志确认配置
make logs-prod

# 触发慢查询测试
```

## ⚙️ 通知高级配置

### 通知间隔（避免告警风暴）

```bash
# 通知间隔（秒），默认 300 秒（5 分钟）
# 在此间隔内，相同告警只发送一次
PROD_NOTIFY_INTERVAL=300
```

建议值：
- **开发环境**：600 秒（10 分钟）
- **测试环境**：300 秒（5 分钟）
- **生产环境**：180 秒（3 分钟）

### 同时启用多种通知

可以同时启用钉钉和邮件，双重保障：

```bash
# 钉钉通知
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxx
PROD_DINGTALK_SECRET=SECxxx

# 邮件通知
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=monitor@company.com
PROD_EMAIL_TO=admin@company.com,dba@company.com
PROD_EMAIL_SMTP_HOST=smtp.exmail.qq.com
PROD_EMAIL_SMTP_PORT=465
PROD_EMAIL_USERNAME=monitor@company.com
PROD_EMAIL_PASSWORD=your-password
```

### 不同环境使用不同通知

```bash
# 开发环境：仅控制台输出，不发送通知
DEV_DINGTALK_WEBHOOK=
DEV_EMAIL_ENABLED=false

# 测试环境：仅钉钉通知
TEST_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=test_token
TEST_EMAIL_ENABLED=false

# 生产环境：钉钉 + 邮件双重通知
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=prod_token
PROD_EMAIL_ENABLED=true
PROD_EMAIL_TO=oncall@company.com
```

## 🧪 测试通知

### 方式 1：手动触发慢查询

连接到数据库，执行：

```sql
-- 执行 15 秒的慢查询（超过默认 10 秒阈值）
SELECT SLEEP(15);
```

### 方式 2：降低阈值测试

临时降低阈值到 1 秒：

```bash
# 在 .env.monitor 中修改
PROD_MONITOR_THRESHOLD=1

# 重启监控
make restart-prod
```

然后执行任意耗时超过 1 秒的查询即可触发告警。

### 方式 3：查看测试日志

```bash
# 查看实时日志
make logs-prod

# 确认通知配置已加载
# 日志中会显示：
# 通知方式: 钉钉、邮件 | 通知间隔: 300s
```

## 🔍 故障排查

### 钉钉通知不工作

1. **检查 Webhook 是否正确**
   ```bash
   # 测试 Webhook（替换为你的 URL）
   curl -X POST "YOUR_WEBHOOK_URL" \
     -H "Content-Type: application/json" \
     -d '{"msgtype":"text","text":{"content":"测试消息"}}'
   ```

2. **检查安全设置**
   - 如果使用了自定义关键词，确保通知内容包含该关键词
   - 如果使用了加签，确保 `DINGTALK_SECRET` 配置正确

3. **查看容器日志**
   ```bash
   docker logs mysql-monitor-prod | grep "钉钉"
   ```

### 邮件通知不工作

1. **检查 SMTP 配置**
   ```bash
   # 进入容器测试
   docker exec -it mysql-monitor-prod /bin/sh

   # 安装 telnet
   apk add busybox-extras

   # 测试 SMTP 连接
   telnet smtp.qq.com 587
   ```

2. **常见错误**
   - `535 Error: authentication failed`: 邮箱密码/授权码错误
   - `530 Must issue a STARTTLS`: 端口配置错误，587 端口需要 STARTTLS
   - `554 DT:SPM`: QQ 邮箱判定为垃圾邮件，更换发件内容

3. **查看详细日志**
   ```bash
   make logs-prod | grep "邮件"
   ```

### 通知发送过于频繁

调整 `NOTIFY_INTERVAL` 参数：

```bash
# 增加通知间隔到 10 分钟
PROD_NOTIFY_INTERVAL=600

# 重启应用配置
make restart-prod
```

### 通知延迟

检查监控间隔：

```bash
# 减小监控间隔到 5 秒（更快发现慢查询）
PROD_MONITOR_INTERVAL=5

# 但同时要注意性能影响
```

## 📊 通知内容定制

当前通知内容包括：

- 数据库地址和端口
- 告警时间
- 慢查询数量
- 每个慢查询的详细信息：
  - 线程 ID 和连接 ID
  - 用户和来源 IP
  - 数据库名
  - 执行时间和锁等待时间
  - 扫描行数和返回行数
  - 当前状态
  - SQL 语句（截取前 150 字符）
  - KILL 命令

如需定制，修改 `main/performance_schema_monitor.go` 中的 `sendNotification` 函数。

## 🎯 最佳实践

1. **生产环境必须配置通知**：确保及时发现问题
2. **使用加签保护钉钉机器人**：避免被恶意调用
3. **配置多个收件人**：确保有人收到告警
4. **设置合理的通知间隔**：避免告警风暴
5. **定期测试通知**：确保通知渠道畅通
6. **监控告警本身**：如果长期没有收到告警，检查监控是否正常运行

## 📝 配置检查清单

- [ ] 钉钉机器人已创建，Webhook 已复制
- [ ] 钉钉安全设置已配置（关键词或加签）
- [ ] 邮箱 SMTP 服务已开启
- [ ] 邮箱授权码已生成
- [ ] `.env.monitor` 配置文件已正确填写
- [ ] 容器已重启，新配置已生效
- [ ] 通知已测试，能正常接收
- [ ] 通知间隔设置合理

## 🆘 需要帮助？

查看日志：
```bash
make logs-prod
```

查看环境变量：
```bash
docker inspect mysql-monitor-prod | grep -A 50 "Env"
```

测试钉钉 Webhook：
```bash
curl -X POST "YOUR_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{"msgtype":"text","text":{"content":"🚨 测试通知"}}'
```
