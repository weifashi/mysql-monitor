# 通知配置快速开始

## 🚀 5分钟配置通知

### 方案 1：钉钉通知（推荐）

#### 1. 创建钉钉机器人

1. 打开钉钉群 → 右上角 `...` → 群设置 → 智能群助手 → 添加机器人 → 自定义
2. 机器人名称：`MySQL监控告警`
3. 安全设置：选择"自定义关键词"，添加关键词：`慢查询`
4. 复制 Webhook 地址

#### 2. 配置 .env.monitor

```bash
# 编辑配置文件
cd mysql-monitor
vi .env.monitor

# 添加钉钉 Webhook（替换为你的地址）
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_ACCESS_TOKEN
```

#### 3. 重启监控

```bash
make restart-prod
```

✅ 完成！现在发现慢查询会自动发送钉钉通知。

---

### 方案 2：QQ 邮箱通知

#### 1. 开启 QQ 邮箱 SMTP

1. 登录 [QQ 邮箱](https://mail.qq.com)
2. 设置 → 账户 → POP3/IMAP/SMTP 服务 → 开启 SMTP
3. 生成授权码（按提示用手机发短信）
4. 复制授权码（16位字符）

#### 2. 配置 .env.monitor

```bash
# 编辑配置文件
cd mysql-monitor
vi .env.monitor

# 添加邮箱配置（替换为你的信息）
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=123456789@qq.com
PROD_EMAIL_TO=admin@company.com,ops@company.com
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=123456789@qq.com
PROD_EMAIL_PASSWORD=abcdefghijklmnop  # 你的授权码
```

#### 3. 重启监控

```bash
make restart-prod
```

✅ 完成！现在发现慢查询会发送邮件到指定邮箱。

---

### 方案 3：钉钉 + 邮件（双重保障）

同时配置钉钉和邮件，确保告警不会漏：

```bash
# 钉钉
PROD_DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxx

# 邮箱
PROD_EMAIL_ENABLED=true
PROD_EMAIL_FROM=123456789@qq.com
PROD_EMAIL_TO=admin@company.com
PROD_EMAIL_SMTP_HOST=smtp.qq.com
PROD_EMAIL_SMTP_PORT=587
PROD_EMAIL_USERNAME=123456789@qq.com
PROD_EMAIL_PASSWORD=your-qq-auth-code
```

---

## 🧪 测试通知

### 方式 1：执行慢查询

连接数据库执行：

```sql
SELECT SLEEP(15);  -- 执行 15 秒，触发告警
```

### 方式 2：降低阈值

```bash
# 临时降低阈值到 1 秒
PROD_MONITOR_THRESHOLD=1

# 重启
make restart-prod

# 执行任意查询都会触发告警
```

---

## 🔧 调整通知频率

```bash
# 通知间隔（秒），避免告警风暴
# 默认 300 秒（5分钟）内只发送一次相同告警

PROD_NOTIFY_INTERVAL=300   # 5分钟
PROD_NOTIFY_INTERVAL=600   # 10分钟
PROD_NOTIFY_INTERVAL=180   # 3分钟
```

---

## 📱 常用邮箱 SMTP 配置

| 邮箱 | SMTP 地址 | 端口 | 说明 |
|------|----------|------|------|
| QQ | smtp.qq.com | 587 | 需要授权码 |
| 163 | smtp.163.com | 465 | 需要授权码 |
| Gmail | smtp.gmail.com | 587 | 需要应用密码 |
| 腾讯企业邮 | smtp.exmail.qq.com | 465 | 企业邮箱 |
| Outlook | smtp.office365.com | 587 | 应用密码 |

---

## ❓ 常见问题

**Q: 为什么没收到通知？**

```bash
# 查看日志
make logs-prod

# 确认配置已加载
# 日志中应该显示：通知方式: 钉钉 或 邮件
```

**Q: 钉钉提示"签名校验失败"？**

检查关键词设置，确保通知内容包含你设置的关键词（如"慢查询"）。

**Q: 邮件发送失败？**

1. 确认使用授权码，不是邮箱密码
2. 检查 SMTP 端口是否正确（QQ 邮箱用 587）
3. 查看详细错误：`make logs-prod | grep 邮件`

**Q: 通知太频繁？**

增加 `NOTIFY_INTERVAL`：

```bash
PROD_NOTIFY_INTERVAL=600  # 改为10分钟
```

---

## 📖 更多配置

详细配置指南：[NOTIFICATION_GUIDE.md](NOTIFICATION_GUIDE.md)
