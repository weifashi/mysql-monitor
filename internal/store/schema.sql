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
    type        TEXT    NOT NULL CHECK(type IN ('dingtalk','feishu','email')),
    config_json TEXT    NOT NULL DEFAULT '{}',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (database_id) REFERENCES databases(id) ON DELETE CASCADE
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
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    dashboard_url  TEXT    NOT NULL,
    username       TEXT    NOT NULL DEFAULT '',
    password       TEXT    NOT NULL DEFAULT '',
    consumer_group TEXT    NOT NULL,
    topic          TEXT    NOT NULL,
    threshold      INTEGER NOT NULL DEFAULT 1000,
    interval_sec   INTEGER NOT NULL DEFAULT 30,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS rocketmq_alert_logs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    config_id      INTEGER NOT NULL,
    config_name    TEXT    NOT NULL DEFAULT '',
    consumer_group TEXT    NOT NULL,
    topic          TEXT    NOT NULL,
    diff_total     INTEGER NOT NULL,
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
    database_id INTEGER NOT NULL,
    process_id  INTEGER NOT NULL,
    notified_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (database_id, process_id)
);
