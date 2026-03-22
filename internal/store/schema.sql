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
