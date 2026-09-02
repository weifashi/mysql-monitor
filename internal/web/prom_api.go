package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"ops-sentinel/internal/monitor"
	"ops-sentinel/internal/store"
)

// ---------------------------------------------------------------------------
// Prometheus 采集目标
// ---------------------------------------------------------------------------

func (s *Server) apiPromTargetList(w http.ResponseWriter, r *http.Request) {
	targets, err := s.store.ListPromTargets()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 附带运行状态与规则数，列表页一次拿全
	items := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		checks, _ := s.store.ListPromChecks(&t.ID)
		items = append(items, map[string]any{
			"target":      t,
			"running":     s.promMgr.IsRunning(t.ID),
			"check_count": len(checks),
		})
	}
	jsonOK(w, items)
}

type promTargetRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	HeadersJSON string `json:"headers_json"`
	TimeoutSec  int    `json:"timeout_sec"`
	IntervalSec int    `json:"interval_sec"`
	LabelsJSON  string `json:"labels_json"`
	Enabled     *bool  `json:"enabled"`
}

func (req *promTargetRequest) applyTo(t *store.PromTarget) {
	t.Name = strings.TrimSpace(req.Name)
	t.URL = strings.TrimSpace(req.URL)
	t.Kind = defaultIfEmpty(req.Kind, "custom")
	t.HeadersJSON = defaultIfEmpty(req.HeadersJSON, "{}")
	t.LabelsJSON = defaultIfEmpty(req.LabelsJSON, "{}")
	t.TimeoutSec = defaultIfZero(req.TimeoutSec, 10)
	t.IntervalSec = defaultIfZero(req.IntervalSec, 30)
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}
}

func (s *Server) apiPromTargetCreate(w http.ResponseWriter, r *http.Request) {
	var req promTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.URL) == "" {
		jsonError(w, http.StatusBadRequest, "名称与 URL 不能为空")
		return
	}

	t := store.PromTarget{Enabled: true}
	req.applyTo(&t)

	id, err := s.store.CreatePromTarget(&t)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t.Enabled {
		_ = s.promMgr.Start(id)
	}
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiPromTargetUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}

	existing, err := s.store.GetPromTarget(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "目标不存在")
		return
	}

	var req promTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.applyTo(existing)

	if err := s.store.UpdatePromTarget(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 配置变了就重启采集循环，让新的 interval / URL 立即生效
	s.promMgr.Stop(id)
	if existing.Enabled {
		_ = s.promMgr.Start(id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiPromTargetDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	s.promMgr.Stop(id)
	if err := s.store.DeletePromTarget(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiPromTargetToggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := s.store.TogglePromTarget(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	t, err := s.store.GetPromTarget(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t.Enabled {
		_ = s.promMgr.Start(id)
	} else {
		s.promMgr.Stop(id)
	}
	jsonOK(w, map[string]any{"enabled": t.Enabled})
}

// apiPromTargetTest 抓一次端点，返回可用指标名，方便配规则时挑指标。
func (s *Server) apiPromTargetTest(w http.ResponseWriter, r *http.Request) {
	var req promTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	t := store.PromTarget{Enabled: true}
	req.applyTo(&t)
	if t.URL == "" {
		jsonError(w, http.StatusBadRequest, "URL 不能为空")
		return
	}

	count, names, err := s.promMgr.TestTarget(&t)
	if err != nil {
		jsonOK(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{
		"success":      true,
		"metric_count": count,
		"metrics":      names,
	})
}

// ---------------------------------------------------------------------------
// Prometheus 告警规则
// ---------------------------------------------------------------------------

func (s *Server) apiPromCheckList(w http.ResponseWriter, r *http.Request) {
	var targetID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("target_id")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			targetID = &id
		}
	}

	checks, err := s.store.ListPromChecks(targetID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 支持按维度过滤，前端按 tab 切换主机/容器/应用等视图
	if dim := strings.TrimSpace(r.URL.Query().Get("dimension")); dim != "" {
		filtered := make([]store.PromCheck, 0, len(checks))
		for _, c := range checks {
			if c.Dimension == dim {
				filtered = append(filtered, c)
			}
		}
		checks = filtered
	}
	jsonOK(w, checks)
}

type promCheckRequest struct {
	TargetID          int64  `json:"target_id"`
	Name              string `json:"name"`
	Dimension         string `json:"dimension"`
	Metric            string `json:"metric"`
	LabelFilter       string `json:"label_filter"`
	Aggregate         string `json:"aggregate"`
	ExprKind          string `json:"expr_kind"`
	ExprDenominator   string `json:"expr_denominator"`
	AlertStrategy     string `json:"alert_strategy"`
	AlertCondition    string `json:"alert_condition"`
	AlertValue        string `json:"alert_value"`
	AlertDeltaValue   string `json:"alert_delta_value"`
	AlertDeltaPercent string `json:"alert_delta_percent"`
	AlertConsecutive  int    `json:"alert_consecutive"`
	Severity          string `json:"severity"`
	NotifyEnabled     *bool  `json:"notify_enabled"`
	RecoveryNotify    *bool  `json:"recovery_notify"`
	MessageTemplate   string `json:"message_template"`
	DiagURL           string `json:"diag_url"`
	AbsentAsZero      *bool  `json:"absent_as_zero"`
	Enabled           *bool  `json:"enabled"`
}

func (req *promCheckRequest) applyTo(c *store.PromCheck) {
	c.TargetID = req.TargetID
	c.Name = strings.TrimSpace(req.Name)
	c.Dimension = defaultIfEmpty(req.Dimension, "custom")
	c.Metric = strings.TrimSpace(req.Metric)
	c.LabelFilter = strings.TrimSpace(req.LabelFilter)
	c.Aggregate = defaultIfEmpty(req.Aggregate, "last")
	c.ExprKind = defaultIfEmpty(req.ExprKind, "raw")
	c.ExprDenominator = strings.TrimSpace(req.ExprDenominator)
	c.AlertStrategy = defaultIfEmpty(req.AlertStrategy, "threshold")
	c.AlertCondition = defaultIfEmpty(req.AlertCondition, "gt")
	c.AlertValue = strings.TrimSpace(req.AlertValue)
	c.AlertDeltaValue = strings.TrimSpace(req.AlertDeltaValue)
	c.AlertDeltaPercent = strings.TrimSpace(req.AlertDeltaPercent)
	c.AlertConsecutive = defaultIfZero(req.AlertConsecutive, 1)
	c.Severity = defaultIfEmpty(req.Severity, "warning")
	c.MessageTemplate = req.MessageTemplate
	c.DiagURL = strings.TrimSpace(req.DiagURL)
	if req.AbsentAsZero != nil {
		c.AbsentAsZero = *req.AbsentAsZero
	}
	if req.NotifyEnabled != nil {
		c.NotifyEnabled = *req.NotifyEnabled
	}
	if req.RecoveryNotify != nil {
		c.RecoveryNotify = *req.RecoveryNotify
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
}

func (s *Server) apiPromCheckCreate(w http.ResponseWriter, r *http.Request) {
	var req promCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.TargetID == 0 || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Metric) == "" {
		jsonError(w, http.StatusBadRequest, "采集目标、名称与指标不能为空")
		return
	}
	if _, err := s.store.GetPromTarget(req.TargetID); err != nil {
		jsonError(w, http.StatusBadRequest, "采集目标不存在")
		return
	}

	c := store.PromCheck{Enabled: true, NotifyEnabled: true, RecoveryNotify: true}
	req.applyTo(&c)

	id, err := s.store.CreatePromCheck(&c)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiPromCheckUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}

	existing, err := s.store.GetPromCheck(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "规则不存在")
		return
	}

	var req promCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.applyTo(existing)

	if err := s.store.UpdatePromCheck(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 阈值或策略变了，历史连续计数不再适用，清掉重新累计
	s.promMgr.DeleteState(id)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiPromCheckDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := s.store.DeletePromCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.promMgr.DeleteState(id)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiPromCheckToggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := s.store.TogglePromCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.promMgr.DeleteState(id)

	c, err := s.store.GetPromCheck(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"enabled": c.Enabled})
}

// apiPromCheckTest 立即求值一次，返回当前值与是否命中，不写状态也不发通知。
func (s *Server) apiPromCheckTest(w http.ResponseWriter, r *http.Request) {
	var req promCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	target, err := s.store.GetPromTarget(req.TargetID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "采集目标不存在")
		return
	}

	c := store.PromCheck{}
	req.applyTo(&c)

	value, matched, reason, err := s.promMgr.TestCheck(target, &c)
	if err != nil {
		jsonOK(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{
		"success": true,
		"value":   value,
		"matched": matched,
		"reason":  reason,
	})
}

func (s *Server) apiPromAlertLogs(w http.ResponseWriter, r *http.Request) {
	var checkID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("check_id")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			checkID = &id
		}
	}
	dimension := strings.TrimSpace(r.URL.Query().Get("dimension"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	logs, total, err := s.store.ListPromAlertLogs(checkID, dimension, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"logs": logs, "total": total})
}

// apiPromDimensionSummary 给概览页：各维度今日告警数 + 规则数。
func (s *Server) apiPromDimensionSummary(w http.ResponseWriter, r *http.Request) {
	byDim, err := s.store.CountPromAlertsByDimension()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	checks, err := s.store.ListPromChecks(nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ruleCount := map[string]int{}
	for _, c := range checks {
		if c.Enabled {
			ruleCount[c.Dimension]++
		}
	}

	dimensions := []string{"host", "container", "database", "middleware", "app", "business", "custom"}
	items := make([]map[string]any, 0, len(dimensions))
	for _, d := range dimensions {
		items = append(items, map[string]any{
			"dimension":    d,
			"rule_count":   ruleCount[d],
			"alerts_today": byDim[d],
		})
	}
	jsonOK(w, items)
}

// ---------------------------------------------------------------------------
// TLS 证书检查
// ---------------------------------------------------------------------------

func (s *Server) apiCertCheckList(w http.ResponseWriter, r *http.Request) {
	checks, err := s.store.ListCertChecks()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		logs, _, _ := s.store.ListCertCheckLogs(&c.ID, 1, 1)
		var last any
		if len(logs) > 0 {
			last = logs[0]
		}
		items = append(items, map[string]any{
			"check":   c,
			"running": s.certMgr.IsRunning(c.ID),
			"last":    last,
		})
	}
	jsonOK(w, items)
}

type certCheckRequest struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	ServerName    string `json:"server_name"`
	WarnDays      int    `json:"warn_days"`
	CriticalDays  int    `json:"critical_days"`
	IntervalSec   int    `json:"interval_sec"`
	NotifyEnabled *bool  `json:"notify_enabled"`
	Enabled       *bool  `json:"enabled"`
}

func (req *certCheckRequest) applyTo(c *store.CertCheck) {
	c.Name = strings.TrimSpace(req.Name)
	c.Endpoint = strings.TrimSpace(req.Endpoint)
	c.ServerName = strings.TrimSpace(req.ServerName)
	c.WarnDays = defaultIfZero(req.WarnDays, 30)
	c.CriticalDays = defaultIfZero(req.CriticalDays, 7)
	c.IntervalSec = defaultIfZero(req.IntervalSec, 3600)
	if req.NotifyEnabled != nil {
		c.NotifyEnabled = *req.NotifyEnabled
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
}

func (s *Server) apiCertCheckCreate(w http.ResponseWriter, r *http.Request) {
	var req certCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Endpoint) == "" {
		jsonError(w, http.StatusBadRequest, "名称与地址不能为空")
		return
	}

	c := store.CertCheck{Enabled: true, NotifyEnabled: true}
	req.applyTo(&c)

	id, err := s.store.CreateCertCheck(&c)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Enabled {
		_ = s.certMgr.Start(id)
	}
	jsonOK(w, map[string]any{"id": id})
}

func (s *Server) apiCertCheckUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}

	existing, err := s.store.GetCertCheck(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "检查项不存在")
		return
	}

	var req certCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.applyTo(existing)

	if err := s.store.UpdateCertCheck(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.certMgr.Stop(id)
	if existing.Enabled {
		_ = s.certMgr.Start(id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCertCheckDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	s.certMgr.Stop(id)
	if err := s.store.DeleteCertCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCertCheckToggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := s.store.ToggleCertCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	c, err := s.store.GetCertCheck(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Enabled {
		_ = s.certMgr.Start(id)
	} else {
		s.certMgr.Stop(id)
	}
	jsonOK(w, map[string]any{"enabled": c.Enabled})
}

// apiCertCheckTest 立即握手一次，不落库不通知。
func (s *Server) apiCertCheckTest(w http.ResponseWriter, r *http.Request) {
	var req certCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" {
		jsonError(w, http.StatusBadRequest, "地址不能为空")
		return
	}

	c := store.CertCheck{}
	req.applyTo(&c)

	result := monitor.InspectCertificate(&c)
	jsonOK(w, map[string]any{
		"success":   result.Status != "error",
		"status":    result.Status,
		"days_left": result.DaysLeft,
		"not_after": result.NotAfter,
		"issuer":    result.Issuer,
		"subject":   result.Subject,
		"message":   result.Message,
		"error":     result.Error,
	})
}

func (s *Server) apiCertCheckLogs(w http.ResponseWriter, r *http.Request) {
	var checkID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("check_id")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			checkID = &id
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	logs, total, err := s.store.ListCertCheckLogs(checkID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"logs": logs, "total": total})
}

// ---------------------------------------------------------------------------

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func defaultIfZero(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
