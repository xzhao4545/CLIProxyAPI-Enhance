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

CREATE TABLE IF NOT EXISTS codex_response_retry_filter_attempts_rollup_hourly (
	bucket_start INTEGER NOT NULL,
	model TEXT NOT NULL,
	auth_id TEXT NOT NULL DEFAULT '',
	auth_label TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL DEFAULT '',
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	attempts INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (bucket_start, model, auth_id, action, reasoning_tokens)
);

CREATE TABLE IF NOT EXISTS codex_response_retry_filter_hits_rollup_hourly (
	bucket_start INTEGER NOT NULL,
	model TEXT NOT NULL,
	auth_id TEXT NOT NULL DEFAULT '',
	auth_label TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL DEFAULT '',
	matched_length INTEGER NOT NULL DEFAULT 0,
	hits INTEGER NOT NULL DEFAULT 0,
	final_successes INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (bucket_start, model, auth_id, action, matched_length)
);

CREATE TABLE IF NOT EXISTS codex_response_retry_filter_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_crrf_attempts_occurred_at ON codex_response_retry_filter_attempts(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_model ON codex_response_retry_filter_attempts(model);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_auth_id ON codex_response_retry_filter_attempts(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_matched ON codex_response_retry_filter_attempts(matched);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_action ON codex_response_retry_filter_attempts(action);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_occurred_id_desc ON codex_response_retry_filter_attempts(occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_model_occurred_id_desc ON codex_response_retry_filter_attempts(model, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_auth_id_occurred_id_desc ON codex_response_retry_filter_attempts(auth_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_reasoning_tokens_occurred_id_desc ON codex_response_retry_filter_attempts(reasoning_tokens, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_action_occurred_id_desc ON codex_response_retry_filter_attempts(action, occurred_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_crrf_hits_occurred_at ON codex_response_retry_filter_hits(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_model ON codex_response_retry_filter_hits(model);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_auth_id ON codex_response_retry_filter_hits(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_matched_length ON codex_response_retry_filter_hits(matched_length);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_action ON codex_response_retry_filter_hits(action);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_occurred_id_desc ON codex_response_retry_filter_hits(occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_model_occurred_id_desc ON codex_response_retry_filter_hits(model, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_auth_id_occurred_id_desc ON codex_response_retry_filter_hits(auth_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_matched_length_occurred_id_desc ON codex_response_retry_filter_hits(matched_length, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_action_occurred_id_desc ON codex_response_retry_filter_hits(action, occurred_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_crrf_attempts_rollup_bucket ON codex_response_retry_filter_attempts_rollup_hourly(bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_rollup_model_bucket ON codex_response_retry_filter_attempts_rollup_hourly(model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_rollup_auth_bucket ON codex_response_retry_filter_attempts_rollup_hourly(auth_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_rollup_action_bucket ON codex_response_retry_filter_attempts_rollup_hourly(action, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_rollup_reasoning_bucket ON codex_response_retry_filter_attempts_rollup_hourly(reasoning_tokens, bucket_start);

CREATE INDEX IF NOT EXISTS idx_crrf_hits_rollup_bucket ON codex_response_retry_filter_hits_rollup_hourly(bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_rollup_model_bucket ON codex_response_retry_filter_hits_rollup_hourly(model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_rollup_auth_bucket ON codex_response_retry_filter_hits_rollup_hourly(auth_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_rollup_action_bucket ON codex_response_retry_filter_hits_rollup_hourly(action, bucket_start);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_rollup_matched_bucket ON codex_response_retry_filter_hits_rollup_hourly(matched_length, bucket_start);
`
