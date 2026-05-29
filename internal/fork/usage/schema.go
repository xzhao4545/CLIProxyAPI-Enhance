package usage

const usageSchema = `
CREATE TABLE IF NOT EXISTS usage_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT,
	started_at INTEGER NOT NULL,
	completed_at INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	provider_key TEXT NOT NULL,
	provider_label TEXT NOT NULL,
	auth_id TEXT,
	auth_label TEXT,
	auth_index TEXT,
	model TEXT NOT NULL,
	client_model TEXT,
	route TEXT,
	status TEXT NOT NULL,
	http_status INTEGER,
	upstream_status INTEGER,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	client_key_hash TEXT,
	error_stage TEXT,
	error_code TEXT,
	error_message TEXT,
	provider_error_raw TEXT,
	metadata_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_usage_events_started_at ON usage_events(started_at);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider ON usage_events(provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider_label ON usage_events(provider_label);
CREATE INDEX IF NOT EXISTS idx_usage_events_model ON usage_events(model);
CREATE INDEX IF NOT EXISTS idx_usage_events_status ON usage_events(status);
CREATE INDEX IF NOT EXISTS idx_usage_events_error_stage ON usage_events(error_stage);
CREATE INDEX IF NOT EXISTS idx_usage_events_auth_id ON usage_events(auth_id);
CREATE INDEX IF NOT EXISTS idx_usage_events_client_key_hash ON usage_events(client_key_hash);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_provider ON usage_events(started_at, provider_key);
CREATE INDEX IF NOT EXISTS idx_usage_events_started_status ON usage_events(started_at, status);
CREATE INDEX IF NOT EXISTS idx_usage_events_provider_started ON usage_events(provider_key, started_at);
`
