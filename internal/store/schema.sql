CREATE TABLE IF NOT EXISTS databases (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL,
    host          TEXT    NOT NULL,
    port          INTEGER NOT NULL DEFAULT 3306,
    user          TEXT    NOT NULL,
    password      TEXT    NOT NULL,
    interval_sec  INTEGER NOT NULL DEFAULT 10,
    threshold_sec INTEGER NOT NULL DEFAULT 10,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS notification_configs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    database_id INTEGER,
    scope_type  TEXT    NOT NULL DEFAULT 'all',
    type        TEXT    NOT NULL,
    config_json TEXT    NOT NULL DEFAULT '{}',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS slow_query_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    database_id   INTEGER NOT NULL,
    thread_id     INTEGER NOT NULL DEFAULT 0,
    process_id    INTEGER NOT NULL DEFAULT 0,
    user          TEXT    NOT NULL DEFAULT '',
    host          TEXT    NOT NULL DEFAULT '',
    db_name       TEXT    NOT NULL DEFAULT '',
    sql_text      TEXT    NOT NULL DEFAULT '',
    exec_sec      REAL    NOT NULL DEFAULT 0,
    lock_sec      REAL    NOT NULL DEFAULT 0,
    rows_examined INTEGER NOT NULL DEFAULT 0,
    rows_sent     INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT '',
    detected_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (database_id) REFERENCES databases(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_slow_detected_at ON slow_query_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_slow_db ON slow_query_logs(database_id, detected_at DESC);

CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    username     TEXT    NOT NULL UNIQUE,
    github_id    INTEGER,
    github_login TEXT,
    avatar_url   TEXT    NOT NULL DEFAULT '',
    role         TEXT    NOT NULL DEFAULT 'member',
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS rocketmq_configs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    dashboard_url   TEXT    NOT NULL,
    username        TEXT    NOT NULL DEFAULT '',
    password        TEXT    NOT NULL DEFAULT '',
    consumer_group  TEXT    NOT NULL,
    topic           TEXT    NOT NULL,
    threshold       INTEGER NOT NULL DEFAULT 1000,
    interval_sec    INTEGER NOT NULL DEFAULT 30,
    notify_new_msg  INTEGER NOT NULL DEFAULT 0,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS rocketmq_alert_logs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    config_id      INTEGER NOT NULL,
    config_name    TEXT    NOT NULL DEFAULT '',
    consumer_group TEXT    NOT NULL,
    topic          TEXT    NOT NULL,
    diff_total     INTEGER NOT NULL,
    message_body   TEXT    NOT NULL DEFAULT '',
    detected_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_rocketmq_alert_detected ON rocketmq_alert_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_rocketmq_alert_config ON rocketmq_alert_logs(config_id, detected_at DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user       TEXT    NOT NULL DEFAULT '',
    action     TEXT    NOT NULL,
    target     TEXT    NOT NULL DEFAULT '',
    target_id  INTEGER NOT NULL DEFAULT 0,
    detail     TEXT    NOT NULL DEFAULT '',
    ip         TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);

CREATE TABLE IF NOT EXISTS health_checks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    url             TEXT    NOT NULL,
    method          TEXT    NOT NULL DEFAULT 'GET',
    headers_json    TEXT    NOT NULL DEFAULT '{}',
    body            TEXT    NOT NULL DEFAULT '',
    expected_status INTEGER NOT NULL DEFAULT 200,
    expected_field  TEXT    NOT NULL DEFAULT '',
    expected_value  TEXT    NOT NULL DEFAULT '',
    alert_field     TEXT    NOT NULL DEFAULT '',
    alert_strategy  TEXT    NOT NULL DEFAULT 'threshold',
    alert_condition TEXT    NOT NULL DEFAULT 'gt',
    alert_value     TEXT    NOT NULL DEFAULT '',
    alert_delta_value TEXT  NOT NULL DEFAULT '',
    alert_delta_percent TEXT NOT NULL DEFAULT '',
    alert_consecutive INTEGER NOT NULL DEFAULT 1,
    alert_rules     TEXT    NOT NULL DEFAULT '[]',
    trigger_actions TEXT    NOT NULL DEFAULT '[]',
    timeout_sec     INTEGER NOT NULL DEFAULT 10,
    interval_sec    INTEGER NOT NULL DEFAULT 30,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS health_check_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id    INTEGER NOT NULL,
    check_name  TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT '',
    http_status INTEGER NOT NULL DEFAULT 0,
    response    TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT '',
    diagnostic_output TEXT NOT NULL DEFAULT '',
    latency_ms  INTEGER NOT NULL DEFAULT 0,
    detected_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_hc_log_detected ON health_check_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_hc_log_check ON health_check_logs(check_id, detected_at DESC);

CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT PRIMARY KEY,
    username    TEXT    NOT NULL DEFAULT '',
    user_id     INTEGER NOT NULL DEFAULT 0,
    github_login TEXT   NOT NULL DEFAULT '',
    role        TEXT    NOT NULL DEFAULT '',
    avatar_url  TEXT    NOT NULL DEFAULT '',
    expires_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS notified_pids (
    database_id     INTEGER NOT NULL,
    process_id      INTEGER NOT NULL,
    sql_fingerprint TEXT    NOT NULL DEFAULT '',
    notified_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (database_id, process_id)
);

CREATE TABLE IF NOT EXISTS ignored_sql_patterns (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    database_id   INTEGER NOT NULL,
    fingerprint   TEXT    NOT NULL,
    sample_sql    TEXT    NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (database_id) REFERENCES databases(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ignored_sql_db_fp ON ignored_sql_patterns(database_id, fingerprint);

CREATE TABLE IF NOT EXISTS grafana_configs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    grafana_url     TEXT    NOT NULL,
    username        TEXT    NOT NULL DEFAULT '',
    password        TEXT    NOT NULL DEFAULT '',
    datasource_uid  TEXT    NOT NULL DEFAULT '',
    auto_rules      TEXT    NOT NULL DEFAULT '[]',
    webhook_url     TEXT    NOT NULL DEFAULT '',
    webhook_secret  TEXT    NOT NULL DEFAULT '',
    webhook_uid     TEXT    NOT NULL DEFAULT '',
    folder_uid      TEXT    NOT NULL DEFAULT '',
    interval_sec    INTEGER NOT NULL DEFAULT 60,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS grafana_alert_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    config_id       INTEGER NOT NULL,
    config_name     TEXT    NOT NULL DEFAULT '',
    alert_name      TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT '',
    severity        TEXT    NOT NULL DEFAULT '',
    summary         TEXT    NOT NULL DEFAULT '',
    description     TEXT    NOT NULL DEFAULT '',
    fingerprint     TEXT    NOT NULL DEFAULT '',
    labels_json     TEXT    NOT NULL DEFAULT '{}',
    starts_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    ends_at         DATETIME,
    detected_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_grafana_alert_detected ON grafana_alert_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_grafana_alert_config ON grafana_alert_logs(config_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_grafana_alert_fingerprint ON grafana_alert_logs(fingerprint);

CREATE TABLE IF NOT EXISTS cloud_logging_configs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL,
    project_id       TEXT    NOT NULL DEFAULT '',
    resource_names   TEXT    NOT NULL DEFAULT '',
    credentials_file TEXT    NOT NULL DEFAULT '',
    default_filter   TEXT    NOT NULL DEFAULT '',
    interval_sec     INTEGER NOT NULL DEFAULT 60,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS cloud_logging_checks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    config_id        INTEGER NOT NULL,
    name             TEXT    NOT NULL,
    filter           TEXT    NOT NULL DEFAULT '',
    metric_type      TEXT    NOT NULL DEFAULT 'count',
    lookback_minutes INTEGER NOT NULL DEFAULT 5,
    threshold_count  INTEGER NOT NULL DEFAULT 0,
    interval_sec     INTEGER NOT NULL DEFAULT 60,
    notify_enabled   INTEGER NOT NULL DEFAULT 1,
    recovery_notify  INTEGER NOT NULL DEFAULT 1,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (config_id) REFERENCES cloud_logging_configs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cloud_logging_alert_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id        INTEGER NOT NULL,
    check_name      TEXT    NOT NULL DEFAULT '',
    config_id       INTEGER NOT NULL,
    config_name     TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT '',
    match_count     INTEGER NOT NULL DEFAULT 0,
    threshold_count INTEGER NOT NULL DEFAULT 0,
    filter          TEXT    NOT NULL DEFAULT '',
    sample          TEXT    NOT NULL DEFAULT '',
    error           TEXT    NOT NULL DEFAULT '',
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    detected_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (check_id) REFERENCES cloud_logging_checks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cloud_logging_checks_config ON cloud_logging_checks(config_id);
CREATE INDEX IF NOT EXISTS idx_cloud_logging_alert_detected ON cloud_logging_alert_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_cloud_logging_alert_check ON cloud_logging_alert_logs(check_id, detected_at DESC);

CREATE TABLE IF NOT EXISTS custom_sql_checks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    database_id      INTEGER NOT NULL,
    name             TEXT    NOT NULL,
    db_name          TEXT    NOT NULL DEFAULT '',
    sql_text         TEXT    NOT NULL,
    result_field     TEXT    NOT NULL DEFAULT '',
    interval_sec     INTEGER NOT NULL DEFAULT 30,
    timeout_sec      INTEGER NOT NULL DEFAULT 10,
    alert_strategy   TEXT    NOT NULL DEFAULT 'threshold',
    condition        TEXT    NOT NULL DEFAULT 'gt',
    expected_value   TEXT    NOT NULL DEFAULT '0',
    alert_delta_value TEXT   NOT NULL DEFAULT '',
    alert_delta_percent TEXT NOT NULL DEFAULT '',
    alert_consecutive INTEGER NOT NULL DEFAULT 1,
    alert_rules      TEXT    NOT NULL DEFAULT '[]',
    trigger_actions  TEXT    NOT NULL DEFAULT '[]',
    notify_enabled   INTEGER NOT NULL DEFAULT 1,
    recovery_notify  INTEGER NOT NULL DEFAULT 1,
    message_template TEXT    NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (database_id) REFERENCES databases(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS custom_sql_logs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id       INTEGER NOT NULL,
    check_name     TEXT    NOT NULL DEFAULT '',
    database_id    INTEGER NOT NULL,
    database_name  TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL DEFAULT '',
    value          TEXT    NOT NULL DEFAULT '',
    expected_value TEXT    NOT NULL DEFAULT '',
    condition      TEXT    NOT NULL DEFAULT '',
    message        TEXT    NOT NULL DEFAULT '',
    error          TEXT    NOT NULL DEFAULT '',
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    detected_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (check_id) REFERENCES custom_sql_checks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_custom_sql_logs_detected ON custom_sql_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_custom_sql_logs_check ON custom_sql_logs(check_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_custom_sql_checks_db ON custom_sql_checks(database_id);

-- ============================================================
-- Prometheus 端点监控（覆盖主机 / 容器 / 中间件 / 应用 / 业务五个维度）
-- 采集对象统一为 Prometheus 文本格式端点：
--   node_exporter      主机资源 USE
--   cAdvisor           容器状态与资源
--   应用 /metrics      RED + 连接池 + 业务指标
--   redis_exporter 等  中间件
-- ============================================================
CREATE TABLE IF NOT EXISTS prom_targets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    url          TEXT    NOT NULL,
    kind         TEXT    NOT NULL DEFAULT 'custom',
    headers_json TEXT    NOT NULL DEFAULT '{}',
    timeout_sec  INTEGER NOT NULL DEFAULT 10,
    interval_sec INTEGER NOT NULL DEFAULT 30,
    labels_json  TEXT    NOT NULL DEFAULT '{}',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS prom_checks (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id           INTEGER NOT NULL,
    name                TEXT    NOT NULL,
    dimension           TEXT    NOT NULL DEFAULT 'custom',
    metric              TEXT    NOT NULL,
    label_filter        TEXT    NOT NULL DEFAULT '',
    aggregate           TEXT    NOT NULL DEFAULT 'last',
    expr_kind           TEXT    NOT NULL DEFAULT 'raw',
    expr_denominator    TEXT    NOT NULL DEFAULT '',
    alert_strategy      TEXT    NOT NULL DEFAULT 'threshold',
    alert_condition     TEXT    NOT NULL DEFAULT 'gt',
    alert_value         TEXT    NOT NULL DEFAULT '',
    alert_delta_value   TEXT    NOT NULL DEFAULT '',
    alert_delta_percent TEXT    NOT NULL DEFAULT '',
    alert_consecutive   INTEGER NOT NULL DEFAULT 1,
    severity            TEXT    NOT NULL DEFAULT 'warning',
    notify_enabled      INTEGER NOT NULL DEFAULT 1,
    recovery_notify     INTEGER NOT NULL DEFAULT 1,
    message_template    TEXT    NOT NULL DEFAULT '',
    diag_url            TEXT    NOT NULL DEFAULT '',  -- 告警时先 GET 它，把响应附进通知
    absent_as_zero      INTEGER NOT NULL DEFAULT 0,   -- 序列缺失按 0 评估（掉线检测用）
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (target_id) REFERENCES prom_targets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS prom_alert_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id    INTEGER NOT NULL,
    check_name  TEXT    NOT NULL DEFAULT '',
    target_id   INTEGER NOT NULL,
    target_name TEXT    NOT NULL DEFAULT '',
    dimension   TEXT    NOT NULL DEFAULT '',
    severity    TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT '',
    metric      TEXT    NOT NULL DEFAULT '',
    value       TEXT    NOT NULL DEFAULT '',
    threshold   TEXT    NOT NULL DEFAULT '',
    message     TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    detected_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (check_id) REFERENCES prom_checks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_prom_alert_logs_detected ON prom_alert_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_prom_alert_logs_check ON prom_alert_logs(check_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_prom_checks_target ON prom_checks(target_id);

-- ============================================================
-- 证书与依赖到期监控（维度 G）
-- ============================================================
CREATE TABLE IF NOT EXISTS cert_checks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT    NOT NULL,
    endpoint          TEXT    NOT NULL,
    server_name       TEXT    NOT NULL DEFAULT '',
    warn_days         INTEGER NOT NULL DEFAULT 30,
    critical_days     INTEGER NOT NULL DEFAULT 7,
    interval_sec      INTEGER NOT NULL DEFAULT 3600,
    notify_enabled    INTEGER NOT NULL DEFAULT 1,
    enabled           INTEGER NOT NULL DEFAULT 1,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS cert_check_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id     INTEGER NOT NULL,
    check_name   TEXT    NOT NULL DEFAULT '',
    endpoint     TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT '',
    issuer       TEXT    NOT NULL DEFAULT '',
    subject      TEXT    NOT NULL DEFAULT '',
    not_after    DATETIME,
    days_left    INTEGER NOT NULL DEFAULT 0,
    message      TEXT    NOT NULL DEFAULT '',
    error        TEXT    NOT NULL DEFAULT '',
    detected_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (check_id) REFERENCES cert_checks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cert_check_logs_detected ON cert_check_logs(detected_at);
CREATE INDEX IF NOT EXISTS idx_cert_check_logs_check ON cert_check_logs(check_id, detected_at DESC);

-- ============================================================
-- 告警事件：把「触发 → 持续 → 恢复」收敛成一行，替代从流水里聚合。
-- 同一 (source, check_id) 同时最多一条 firing。
-- ============================================================
CREATE TABLE IF NOT EXISTS alert_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source       TEXT    NOT NULL,             -- prom / health / custom_sql / cert
    check_id     INTEGER NOT NULL,
    check_name   TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',  -- 规则名去掉对象前缀，用于跨对象聚合
    target_id    INTEGER NOT NULL DEFAULT 0,
    target_name  TEXT    NOT NULL DEFAULT '',
    dimension    TEXT    NOT NULL DEFAULT '',
    severity     TEXT    NOT NULL DEFAULT 'warning',
    status       TEXT    NOT NULL DEFAULT 'firing',   -- firing / resolved
    value        TEXT    NOT NULL DEFAULT '',
    detail       TEXT    NOT NULL DEFAULT '',  -- 聚合来源（如具体是哪个容器）
    peak_value   TEXT    NOT NULL DEFAULT '',
    threshold    TEXT    NOT NULL DEFAULT '',
    message      TEXT    NOT NULL DEFAULT '',
    first_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    last_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at  DATETIME,
    notify_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_alert_events_status ON alert_events(status, last_at);
CREATE INDEX IF NOT EXISTS idx_alert_events_check  ON alert_events(source, check_id, status);

-- ============================================================
-- 指标时序采样：趋势图数据源。每分钟全量规则各存一点，保留 72h。
-- ============================================================
CREATE TABLE IF NOT EXISTS metric_samples (
    check_id INTEGER NOT NULL,
    ts       INTEGER NOT NULL,
    value    REAL    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metric_samples_check_ts ON metric_samples (check_id, ts);
