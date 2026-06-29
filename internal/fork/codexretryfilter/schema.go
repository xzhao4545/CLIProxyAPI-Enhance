package codexretryfilter

const schemaSQL = `
CREATE TABLE IF NOT EXISTS codex_response_retry_filter_attempts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT,
	occurred_at INTEGER NOT NULL,
	provider_key TEXT NOT NULL,
	auth_id TEXT,
	auth_label TEXT,
	model TEXT NOT NULL,
	client_model TEXT,
	response_model TEXT,
	stream INTEGER NOT NULL DEFAULT 0,
	eligible INTEGER NOT NULL DEFAULT 0,
	matched INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER,
	action TEXT NOT NULL DEFAULT '',
	guard_retry_remaining INTEGER NOT NULL DEFAULT 0,
	attempt INTEGER NOT NULL DEFAULT 1,
	final_success INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS codex_response_retry_filter_hits (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT,
	occurred_at INTEGER NOT NULL,
	provider_key TEXT NOT NULL,
	auth_id TEXT,
	auth_label TEXT,
	model TEXT NOT NULL,
	client_model TEXT,
	response_model TEXT,
	stream INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL,
	matched_length INTEGER NOT NULL,
	action TEXT NOT NULL DEFAULT '',
	guard_retry_remaining INTEGER NOT NULL DEFAULT 0,
	attempt INTEGER NOT NULL DEFAULT 1,
	retried INTEGER NOT NULL DEFAULT 1,
	final_success INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_crrf_attempts_occurred_at ON codex_response_retry_filter_attempts(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_model ON codex_response_retry_filter_attempts(model);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_auth_id ON codex_response_retry_filter_attempts(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_matched ON codex_response_retry_filter_attempts(matched);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_action ON codex_response_retry_filter_attempts(action);

CREATE INDEX IF NOT EXISTS idx_crrf_hits_occurred_at ON codex_response_retry_filter_hits(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_model ON codex_response_retry_filter_hits(model);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_auth_id ON codex_response_retry_filter_hits(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_matched_length ON codex_response_retry_filter_hits(matched_length);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_action ON codex_response_retry_filter_hits(action);
`
