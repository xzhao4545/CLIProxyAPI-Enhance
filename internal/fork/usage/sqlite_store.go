package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const usageRollupVersion = "3"

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if path == "" {
		path = defaultSQLitePath
	}
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create usage sqlite directory: %w", err)
			}
		}
	}

	db, errOpen := sql.Open("sqlite", path)
	if errOpen != nil {
		return nil, fmt.Errorf("open usage sqlite: %w", errOpen)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) configure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("usage sqlite store is not initialized")
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure usage sqlite %q: %w", pragma, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, usageTablesSchema); err != nil {
		return fmt.Errorf("migrate usage sqlite schema: %w", err)
	}
	if err := s.ensureUsageEventColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureUsageRollupColumns(ctx); err != nil {
		return err
	}
	if err := s.backfillUsageProviderStats(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, usageIndexesSchema); err != nil {
		return fmt.Errorf("migrate usage sqlite indexes: %w", err)
	}
	if err := s.backfillUsageRollup(ctx); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) InsertEvent(ctx context.Context, event Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.CompletedAt.IsZero() {
		event.CompletedAt = event.StartedAt
	}
	event = normalizeCacheTokens(event)
	statsProviderKey, statsProviderLabel := eventStatsProviderIdentity(event)
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin usage insert: %w", errBegin)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	_, err := tx.ExecContext(ctx, `
INSERT INTO usage_events (
	request_id, started_at, completed_at, duration_ms, provider_key, provider_label,
	stats_provider_key, stats_provider_label, auth_id, auth_label, auth_index,
	auth_type, auth_category, model, client_model, response_model, route, stream,
	status, http_status, upstream_status, prompt_tokens, completion_tokens,
	total_tokens, reasoning_tokens, reasoning_effort, cached_tokens, cache_read_tokens,
	cache_creation_tokens, ttft_ms,
	client_key_hash, error_stage, error_code, error_message, provider_error_raw,
	metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestID,
		event.StartedAt.UTC().UnixMilli(),
		event.CompletedAt.UTC().UnixMilli(),
		event.DurationMS,
		event.ProviderKey,
		event.ProviderLabel,
		statsProviderKey,
		statsProviderLabel,
		event.AuthID,
		event.AuthLabel,
		event.AuthIndex,
		event.AuthType,
		event.AuthCategory,
		event.Model,
		event.ClientModel,
		event.ResponseModel,
		event.Route,
		streamFlag(event.Stream),
		event.Status,
		event.HTTPStatus,
		event.UpstreamStatus,
		event.PromptTokens,
		event.CompletionTokens,
		event.TotalTokens,
		event.ReasoningTokens,
		event.ReasoningEffort,
		event.CachedTokens,
		event.CacheReadTokens,
		event.CacheCreationTokens,
		event.TTFTMS,
		event.ClientKeyHash,
		event.ErrorStage,
		event.ErrorCode,
		event.ErrorMessage,
		event.ProviderErrorRaw,
		event.MetadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert usage event: %w", err)
	}
	if err := upsertUsageRollup(ctx, tx, event, statsProviderKey, statsProviderLabel); err != nil {
		return err
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit usage insert: %w", errCommit)
	}
	return nil
}

func (s *SQLiteStore) ensureUsageEventColumns(ctx context.Context) error {
	rows, errQuery := s.db.QueryContext(ctx, "PRAGMA table_info(usage_events)")
	if errQuery != nil {
		return fmt.Errorf("inspect usage_events columns: %w", errQuery)
	}
	defer rows.Close()
	columns := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan usage_events column: %w", err)
		}
		columns[name] = struct{}{}
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("iterate usage_events columns: %w", errRows)
	}
	migrations := map[string]string{
		"stats_provider_key":    "ALTER TABLE usage_events ADD COLUMN stats_provider_key TEXT NOT NULL DEFAULT ''",
		"stats_provider_label":  "ALTER TABLE usage_events ADD COLUMN stats_provider_label TEXT NOT NULL DEFAULT ''",
		"auth_type":             "ALTER TABLE usage_events ADD COLUMN auth_type TEXT NOT NULL DEFAULT ''",
		"auth_category":         "ALTER TABLE usage_events ADD COLUMN auth_category TEXT NOT NULL DEFAULT ''",
		"response_model":        "ALTER TABLE usage_events ADD COLUMN response_model TEXT NOT NULL DEFAULT ''",
		"stream":                "ALTER TABLE usage_events ADD COLUMN stream INTEGER NOT NULL DEFAULT 0",
		"reasoning_effort":      "ALTER TABLE usage_events ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT ''",
		"cache_read_tokens":     "ALTER TABLE usage_events ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0",
		"cache_creation_tokens": "ALTER TABLE usage_events ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0",
		"ttft_ms":               "ALTER TABLE usage_events ADD COLUMN ttft_ms INTEGER NOT NULL DEFAULT 0",
	}
	for column, statement := range migrations {
		if _, exists := columns[column]; exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add usage_events column %s: %w", column, err)
		}
	}
	return nil
}

func (s *SQLiteStore) ensureUsageRollupColumns(ctx context.Context) error {
	rows, errQuery := s.db.QueryContext(ctx, "PRAGMA table_info(usage_rollup_hourly)")
	if errQuery != nil {
		return fmt.Errorf("inspect usage_rollup_hourly columns: %w", errQuery)
	}
	defer rows.Close()
	columns := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan usage_rollup_hourly column: %w", err)
		}
		columns[name] = struct{}{}
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("iterate usage_rollup_hourly columns: %w", errRows)
	}
	migrations := map[string]string{
		"cache_read_tokens":     "ALTER TABLE usage_rollup_hourly ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0",
		"cache_creation_tokens": "ALTER TABLE usage_rollup_hourly ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0",
	}
	for column, statement := range migrations {
		if _, exists := columns[column]; exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add usage_rollup_hourly column %s: %w", column, err)
		}
	}
	return nil
}

func (s *SQLiteStore) backfillUsageProviderStats(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
UPDATE usage_events
SET
	stats_provider_key = `+providerStatsFallbackKeySQL()+`,
	stats_provider_label = `+providerStatsFallbackLabelSQL()+`
WHERE stats_provider_key = '' OR stats_provider_label = ''`); err != nil {
		return fmt.Errorf("backfill usage provider stats: %w", err)
	}
	return nil
}

func (s *SQLiteStore) backfillUsageRollup(ctx context.Context) error {
	var storedVersion string
	errVersion := s.db.QueryRowContext(ctx, "SELECT value FROM usage_meta WHERE key = 'rollup_version'").Scan(&storedVersion)
	if errVersion != nil && errVersion != sql.ErrNoRows {
		return fmt.Errorf("read usage rollup version: %w", errVersion)
	}
	if storedVersion == usageRollupVersion {
		return nil
	}
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin usage hourly rollup rebuild: %w", errBegin)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, "DELETE FROM usage_rollup_hourly"); err != nil {
		return fmt.Errorf("clear usage hourly rollup: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO usage_rollup_hourly (
	bucket_start, stats_provider_key, stats_provider_label, model, client_model, status,
	requests, successful_requests, failed_requests, prompt_tokens, completion_tokens,
	total_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens
)
SELECT
	(started_at / 3600000) * 3600000 AS bucket_start,
	stats_provider_key,
	stats_provider_label,
	model,
	COALESCE(client_model, '') AS client_model,
	status,
	COUNT(*) AS requests,
	SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS successful_requests,
	SUM(CASE WHEN status = 'failure' THEN 1 ELSE 0 END) AS failed_requests,
	COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
	COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
	COALESCE(SUM(total_tokens), 0) AS total_tokens,
	COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
	COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
	COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
	COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens
FROM usage_events
GROUP BY bucket_start, stats_provider_key, stats_provider_label, model, client_model, status`)
	if err != nil {
		return fmt.Errorf("backfill usage hourly rollup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO usage_meta(key, value) VALUES ('rollup_version', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, usageRollupVersion); err != nil {
		return fmt.Errorf("write usage rollup version: %w", err)
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit usage hourly rollup rebuild: %w", errCommit)
	}
	return nil
}

func upsertUsageRollup(ctx context.Context, tx *sql.Tx, event Event, statsProviderKey, statsProviderLabel string) error {
	successful := 0
	failed := 0
	switch event.Status {
	case StatusSuccess:
		successful = 1
	case StatusFailure:
		failed = 1
	}
	bucketStart := event.StartedAt.UTC().Truncate(time.Hour).UnixMilli()
	clientModel := strings.TrimSpace(event.ClientModel)
	_, err := tx.ExecContext(ctx, `
INSERT INTO usage_rollup_hourly (
	bucket_start, stats_provider_key, stats_provider_label, model, client_model, status,
	requests, successful_requests, failed_requests, prompt_tokens, completion_tokens,
	total_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens
) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(bucket_start, stats_provider_key, model, client_model, status) DO UPDATE SET
	stats_provider_label = excluded.stats_provider_label,
	requests = requests + excluded.requests,
	successful_requests = successful_requests + excluded.successful_requests,
	failed_requests = failed_requests + excluded.failed_requests,
	prompt_tokens = prompt_tokens + excluded.prompt_tokens,
	completion_tokens = completion_tokens + excluded.completion_tokens,
	total_tokens = total_tokens + excluded.total_tokens,
	reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
	cached_tokens = cached_tokens + excluded.cached_tokens,
	cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
	cache_creation_tokens = cache_creation_tokens + excluded.cache_creation_tokens`,
		bucketStart,
		statsProviderKey,
		statsProviderLabel,
		strings.TrimSpace(event.Model),
		clientModel,
		strings.TrimSpace(event.Status),
		successful,
		failed,
		event.PromptTokens,
		event.CompletionTokens,
		event.TotalTokens,
		event.ReasoningTokens,
		event.CachedTokens,
		event.CacheReadTokens,
		event.CacheCreationTokens,
	)
	if err != nil {
		return fmt.Errorf("upsert usage hourly rollup: %w", err)
	}
	return nil
}

func normalizeCacheTokens(event Event) Event {
	if event.CacheReadTokens == 0 && event.CacheCreationTokens == 0 && event.CachedTokens > 0 {
		event.CacheReadTokens = event.CachedTokens
	}
	if event.CachedTokens == 0 && event.CacheReadTokens > 0 {
		event.CachedTokens = event.CacheReadTokens
	}
	return event
}

func eventStatsProviderIdentity(event Event) (string, string) {
	providerKey := strings.TrimSpace(event.ProviderKey)
	providerLabel := strings.TrimSpace(event.ProviderLabel)
	authIndex := strings.TrimSpace(event.AuthIndex)
	label := providerLabel
	if label == "" {
		label = providerKey
	}
	key := providerKey
	if providerLabel != "" && providerLabel != providerKey {
		key = providerLabel
	} else if authIndex != "" {
		key = authIndex
	}
	return key, label
}

func streamFlag(stream bool) int {
	if stream {
		return 1
	}
	return 0
}
