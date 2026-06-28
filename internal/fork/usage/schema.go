package usage

const usageTablesSchema = `
CREATE TABLE IF NOT EXISTS usage_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT,
	started_at INTEGER NOT NULL,
	completed_at INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	provider_key TEXT NOT NULL,
	provider_label TEXT NOT NULL,
	stats_provider_key TEXT NOT NULL DEFAULT '',
	stats_provider_label TEXT NOT NULL DEFAULT '',
	auth_id TEXT,
	auth_label TEXT,
	auth_index TEXT,
	auth_type TEXT NOT NULL DEFAULT '',
	auth_category TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL,
	client_model TEXT,
	response_model TEXT NOT NULL DEFAULT '',
	route TEXT,
	stream INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL,
	http_status INTEGER,
	upstream_status INTEGER,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_effort TEXT NOT NULL DEFAULT '',
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
	ttft_ms INTEGER NOT NULL DEFAULT 0,
	client_key_hash TEXT,
	error_stage TEXT,
	error_code TEXT,
	error_message TEXT,
	provider_error_raw TEXT,
	metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS usage_rollup_hourly (
	bucket_start INTEGER NOT NULL,
	stats_provider_key TEXT NOT NULL,
	stats_provider_label TEXT NOT NULL,
	model TEXT NOT NULL,
	client_model TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	requests INTEGER NOT NULL DEFAULT 0,
	successful_requests INTEGER NOT NULL DEFAULT 0,
	failed_requests INTEGER NOT NULL DEFAULT 0,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (bucket_start, stats_provider_key, model, client_model, status)
);

CREATE TABLE IF NOT EXISTS usage_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

const usageIndexesSchema = `
CREATE INDEX IF NOT EXISTS idx_usage_events_started_at ON usage_events(started_at);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider ON usage_events(provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider_label ON usage_events(provider_label);
CREATE INDEX IF NOT EXISTS idx_usage_events_stats_provider ON usage_events(stats_provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_stats_provider_label ON usage_events(stats_provider_label);
CREATE INDEX IF NOT EXISTS idx_usage_events_model ON usage_events(model);
CREATE INDEX IF NOT EXISTS idx_usage_events_response_model ON usage_events(response_model);
CREATE INDEX IF NOT EXISTS idx_usage_events_client_model ON usage_events(client_model);
CREATE INDEX IF NOT EXISTS idx_usage_events_auth_type ON usage_events(auth_type);
CREATE INDEX IF NOT EXISTS idx_usage_events_auth_category ON usage_events(auth_category);
CREATE INDEX IF NOT EXISTS idx_usage_events_stream ON usage_events(stream);
CREATE INDEX IF NOT EXISTS idx_usage_events_reasoning_effort ON usage_events(reasoning_effort);
CREATE INDEX IF NOT EXISTS idx_usage_events_status ON usage_events(status);
CREATE INDEX IF NOT EXISTS idx_usage_events_error_stage ON usage_events(error_stage);
CREATE INDEX IF NOT EXISTS idx_usage_events_auth_id ON usage_events(auth_id);
CREATE INDEX IF NOT EXISTS idx_usage_events_client_key_hash ON usage_events(client_key_hash);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_provider ON usage_events(started_at, provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_status ON usage_events(started_at, status);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider_started ON usage_events(provider_key, started_at);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_stats_provider ON usage_events(started_at, stats_provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_stats_provider_started ON usage_events(stats_provider_key, started_at);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_model ON usage_events(started_at, model);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_client_model ON usage_events(started_at, client_model);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_response_model ON usage_events(started_at, response_model);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_auth_type ON usage_events(started_at, auth_type);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_auth_category ON usage_events(started_at, auth_category);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_stream ON usage_events(started_at, stream);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_reasoning_effort ON usage_events(started_at, reasoning_effort);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_error_stage ON usage_events(started_at, error_stage);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_error_code ON usage_events(started_at, error_code);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_auth_id ON usage_events(started_at, auth_id);

CREATE INDEX IF NOT EXISTS idx_usage_rollup_hourly_bucket ON usage_rollup_hourly(bucket_start);
CREATE INDEX IF NOT EXISTS idx_usage_rollup_hourly_provider_bucket ON usage_rollup_hourly(stats_provider_key, bucket_start);
CREATE INDEX IF NOT EXISTS idx_usage_rollup_hourly_model_bucket ON usage_rollup_hourly(model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_usage_rollup_hourly_status_bucket ON usage_rollup_hourly(status, bucket_start);
`
