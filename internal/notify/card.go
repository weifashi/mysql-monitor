package notify

import (
	"fmt"
	"strings"
)

// AlertCard 是结构化告警通知：飞书渲染成「彩色头 + 字段行 + 查看详情」
// 的消息卡片（对齐旧云监控的推送样式），其他通道降级为纯文本。
type AlertCard struct {
	Title  string      // 规则名，不带级别前缀（前缀由 Level 生成）
	Level  string      // critical / warning / recovery / info / test
	Fields [][2]string // 有序键值对（实例 / 指标 / 当前值 / 开始 …）
	Note   string      // 附加说明（消息模板渲染结果、来源等）
	Code   string      // 代码块（诊断样本），可空
	// DetailPath 是站内相对路径（如 /#/objects/9）；
	// 发送时用 Dispatcher.PublicBaseURL 拼成完整链接，Base 未配置则省略。
	DetailPath string
}

func (c *AlertCard) titlePrefix() string {
	switch strings.ToLower(strings.TrimSpace(c.Level)) {
	case "critical", "error":
		return "[CRITICAL] "
	case "warning", "warn":
		return "[WARNING] "
	case "recovery", "recovered", "ok":
		return "[已恢复] "
	case "test":
		return "[测试] "
	case "info":
		return "[INFO] "
	default:
		return ""
	}
}

// PlainText 供非飞书通道（钉钉/邮件/DooTask）使用的降级文本。
func (c *AlertCard) PlainText(baseURL string) string {
	var b strings.Builder
	b.WriteString(c.titlePrefix() + c.Title + "\n")
	for _, kv := range c.Fields {
		fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
	}
	if strings.TrimSpace(c.Note) != "" {
		b.WriteString("\n" + strings.TrimSpace(c.Note) + "\n")
	}
	if strings.TrimSpace(c.Code) != "" {
		b.WriteString("\n" + strings.TrimSpace(c.Code) + "\n")
	}
	if url := c.detailURL(baseURL); url != "" {
		b.WriteString("\n查看详情: " + url + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *AlertCard) detailURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(c.DetailPath) == "" {
		return ""
	}
	return baseURL + c.DetailPath
}

// SendGlobalAlertCard 用全局通知渠道发送结构化卡片。
// 飞书走卡片渲染，其余通道用 PlainText 降级。
func (d *Dispatcher) SendGlobalAlertCard(card *AlertCard) error {
	configs, err := d.store.GetGlobalNotifications()
	if err != nil {
		return fmt.Errorf("load global notification configs: %w", err)
	}
	if len(configs) == 0 {
		return nil
	}
	return d.dispatchCard(configs, card)
}
