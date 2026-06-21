package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ops-sentinel/internal/monitor"
	"ops-sentinel/internal/store"
)

func (s *Server) apiCloudLoggingConfigList(w http.ResponseWriter, r *http.Request) {
	configs, err := s.store.ListCloudLoggingConfigs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type configWithStatus struct {
		store.CloudLoggingConfig
		RunningChecks int `json:"running_checks"`
	}
	var list []configWithStatus
	checks, _ := s.store.ListCloudLoggingChecks()
	running := make(map[int64]int)
	for _, c := range checks {
		if s.cloudLoggingMgr != nil && s.cloudLoggingMgr.IsRunning(c.ID) {
			running[c.ConfigID]++
		}
	}
	for _, c := range configs {
		list = append(list, configWithStatus{CloudLoggingConfig: c, RunningChecks: running[c.ID]})
	}
	if list == nil {
		list = []configWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiCloudLoggingConfigCreate(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.cloudLoggingConfigFromRequest(w, r)
	if !ok {
		return
	}
	id, err := s.store.CreateCloudLoggingConfig(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "create", "cloud_logging_config", id, "创建 Cloud Logging 配置 "+cfg.Name)
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) apiCloudLoggingConfigUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, ok := s.cloudLoggingConfigFromRequest(w, r)
	if !ok {
		return
	}
	cfg.ID = id
	if err := s.store.UpdateCloudLoggingConfig(cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cloudLoggingMgr != nil {
		_ = s.cloudLoggingMgr.RestartAll()
	}
	s.audit(r, "update", "cloud_logging_config", id, "更新 Cloud Logging 配置 "+cfg.Name)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingConfigDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteCloudLoggingConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cloudLoggingMgr != nil {
		_ = s.cloudLoggingMgr.RestartAll()
	}
	s.audit(r, "delete", "cloud_logging_config", id, "删除 Cloud Logging 配置")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingConfigToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleCloudLoggingConfig(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cloudLoggingMgr != nil {
		_ = s.cloudLoggingMgr.RestartAll()
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingConfigTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetCloudLoggingConfig(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "配置不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := monitor.QueryCloudLogging(ctx, cfg, "", 5, 1)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true, "message": "连接成功，返回 " + strconv.Itoa(len(res.Entries)) + " 条日志", "data": res})
}

func (s *Server) cloudLoggingConfigFromRequest(w http.ResponseWriter, r *http.Request) (*store.CloudLoggingConfig, bool) {
	var req struct {
		Name            string `json:"name"`
		ProjectID       string `json:"project_id"`
		ResourceNames   string `json:"resource_names"`
		CredentialsFile string `json:"credentials_file"`
		DefaultFilter   string `json:"default_filter"`
		IntervalSec     int    `json:"interval_sec"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return nil, false
	}
	if strings.TrimSpace(req.Name) == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return nil, false
	}
	if strings.TrimSpace(req.ProjectID) == "" && strings.TrimSpace(req.ResourceNames) == "" {
		jsonError(w, http.StatusBadRequest, "project_id or resource_names is required")
		return nil, false
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 60
	}
	return &store.CloudLoggingConfig{
		Name: strings.TrimSpace(req.Name), ProjectID: strings.TrimSpace(req.ProjectID),
		ResourceNames: strings.TrimSpace(req.ResourceNames), CredentialsFile: strings.TrimSpace(req.CredentialsFile),
		DefaultFilter: strings.TrimSpace(req.DefaultFilter), IntervalSec: req.IntervalSec, Enabled: enabled,
	}, true
}

func (s *Server) apiCloudLoggingCheckList(w http.ResponseWriter, r *http.Request) {
	checks, err := s.store.ListCloudLoggingChecks()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type checkWithStatus struct {
		store.CloudLoggingCheck
		Running bool `json:"running"`
	}
	var list []checkWithStatus
	for _, c := range checks {
		running := s.cloudLoggingMgr != nil && s.cloudLoggingMgr.IsRunning(c.ID)
		list = append(list, checkWithStatus{CloudLoggingCheck: c, Running: running})
	}
	if list == nil {
		list = []checkWithStatus{}
	}
	jsonOK(w, list)
}

func (s *Server) apiCloudLoggingCheckCreate(w http.ResponseWriter, r *http.Request) {
	check, ok := s.cloudLoggingCheckFromRequest(w, r)
	if !ok {
		return
	}
	id, err := s.store.CreateCloudLoggingCheck(check)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if check.Enabled && s.cloudLoggingMgr != nil {
		if err := s.cloudLoggingMgr.Start(id); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.audit(r, "create", "cloud_logging_check", id, "创建 Cloud Logging 监控 "+check.Name)
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) apiCloudLoggingCheckUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	check, ok := s.cloudLoggingCheckFromRequest(w, r)
	if !ok {
		return
	}
	check.ID = id
	if err := s.store.UpdateCloudLoggingCheck(check); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cloudLoggingMgr != nil {
		if check.Enabled {
			_ = s.cloudLoggingMgr.Restart(id)
		} else {
			s.cloudLoggingMgr.Stop(id)
		}
	}
	s.audit(r, "update", "cloud_logging_check", id, "更新 Cloud Logging 监控 "+check.Name)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingCheckDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.cloudLoggingMgr != nil {
		s.cloudLoggingMgr.Stop(id)
	}
	if err := s.store.DeleteCloudLoggingCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "delete", "cloud_logging_check", id, "删除 Cloud Logging 监控")
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingCheckToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ToggleCloudLoggingCheck(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	check, err := s.store.GetCloudLoggingCheck(id)
	if err == nil && s.cloudLoggingMgr != nil {
		if check.Enabled {
			_ = s.cloudLoggingMgr.Start(id)
		} else {
			s.cloudLoggingMgr.Stop(id)
		}
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) apiCloudLoggingCheckTest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	check, err := s.store.GetCloudLoggingCheck(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "监控不存在")
		return
	}
	cfg, err := s.store.GetCloudLoggingConfig(check.ConfigID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "配置不存在")
		return
	}
	limit := check.ThresholdCount + 1
	if limit < 1 {
		limit = 1
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	message := ""
	var res *monitor.CloudLoggingQueryResult
	if check.MetricType == store.CloudLoggingMetricPeakConcurrency {
		res, err = monitor.QueryCloudLoggingWithStats(ctx, cfg, check.Filter, check.LookbackMinutes, 20, 500000)
		if err == nil {
			peak := 0
			if res.Stats != nil {
				peak = res.Stats.PeakConcurrency
			}
			message = "接口最大并发 " + strconv.Itoa(peak)
		}
	} else {
		res, err = monitor.QueryCloudLogging(ctx, cfg, check.Filter, check.LookbackMinutes, limit)
	}
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if message == "" {
		message = "命中 " + strconv.Itoa(len(res.Entries)) + " 条"
	}
	jsonOK(w, map[string]any{"ok": true, "message": message, "data": res})
}

func (s *Server) cloudLoggingCheckFromRequest(w http.ResponseWriter, r *http.Request) (*store.CloudLoggingCheck, bool) {
	var req struct {
		ConfigID        int64  `json:"config_id"`
		Name            string `json:"name"`
		Filter          string `json:"filter"`
		MetricType      string `json:"metric_type"`
		LookbackMinutes int    `json:"lookback_minutes"`
		ThresholdCount  int    `json:"threshold_count"`
		IntervalSec     int    `json:"interval_sec"`
		NotifyEnabled   *bool  `json:"notify_enabled"`
		RecoveryNotify  *bool  `json:"recovery_notify"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return nil, false
	}
	if req.ConfigID <= 0 || strings.TrimSpace(req.Name) == "" {
		jsonError(w, http.StatusBadRequest, "config_id and name are required")
		return nil, false
	}
	if _, err := s.store.GetCloudLoggingConfig(req.ConfigID); err != nil {
		jsonError(w, http.StatusBadRequest, "cloud logging config not found")
		return nil, false
	}
	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 5
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 60
	}
	notifyEnabled := true
	if req.NotifyEnabled != nil {
		notifyEnabled = *req.NotifyEnabled
	}
	recoveryNotify := true
	if req.RecoveryNotify != nil {
		recoveryNotify = *req.RecoveryNotify
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return &store.CloudLoggingCheck{
		ConfigID: req.ConfigID, Name: strings.TrimSpace(req.Name), Filter: strings.TrimSpace(req.Filter),
		MetricType: strings.TrimSpace(req.MetricType), LookbackMinutes: req.LookbackMinutes, ThresholdCount: req.ThresholdCount, IntervalSec: req.IntervalSec,
		NotifyEnabled: notifyEnabled, RecoveryNotify: recoveryNotify, Enabled: enabled,
	}, true
}

func (s *Server) apiCloudLoggingQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConfigID        int64  `json:"config_id"`
		Filter          string `json:"filter"`
		LookbackMinutes int    `json:"lookback_minutes"`
		Limit           int    `json:"limit"`
		StatsLimit      int    `json:"stats_limit"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ConfigID <= 0 {
		jsonError(w, http.StatusBadRequest, "config_id is required")
		return
	}
	cfg, err := s.store.GetCloudLoggingConfig(req.ConfigID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "配置不存在")
		return
	}
	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 30
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	res, err := monitor.QueryCloudLoggingWithStats(ctx, cfg, req.Filter, req.LookbackMinutes, req.Limit, req.StatsLimit)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, res)
}

func (s *Server) apiCloudLoggingLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 50
	var checkID *int64
	if raw := r.URL.Query().Get("check_id"); raw != "" {
		id, _ := strconv.ParseInt(raw, 10, 64)
		if id > 0 {
			checkID = &id
		}
	}
	logs, total, err := s.store.ListCloudLoggingAlertLogs(checkID, page, pageSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.CloudLoggingAlertLog{}
	}
	jsonOK(w, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}
