package codexretryfilter

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

const defaultSQLitePath = "usage.sqlite3"

type Store struct {
	db *sql.DB
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(path) == "" {
		path = defaultSQLitePath
	}
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create codex retry filter sqlite directory: %w", err)
			}
		}
	}
	db, errOpen := sql.Open("sqlite", path)
	if errOpen != nil {
		return nil, fmt.Errorf("open codex retry filter sqlite: %w", errOpen)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) configure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("codex retry filter store is not initialized")
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure codex retry filter sqlite %q: %w", pragma, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("migrate codex retry filter schema: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) InsertAttempt(ctx context.Context, record AttemptRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record = normalizeAttempt(record)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_attempts (
	request_id, occurred_at, provider_key, auth_id, auth_label, model, client_model,
	response_model, stream, eligible, matched, reasoning_tokens, action,
	guard_retry_remaining, attempt, final_success, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID,
		record.OccurredAt.UTC().UnixMilli(),
		record.ProviderKey,
		record.AuthID,
		record.AuthLabel,
		record.Model,
		record.ClientModel,
		record.ResponseModel,
		boolToInt(record.Stream),
		boolToInt(record.Eligible),
		boolToInt(record.Matched),
		nullableInt64(record.ReasoningTokens),
		record.Action,
		record.GuardRetryRemaining,
		record.Attempt,
		boolToInt(record.FinalSuccess),
		record.MetadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert codex retry filter attempt: %w", err)
	}
	return nil
}

func (s *Store) InsertHit(ctx context.Context, record AttemptRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("codex retry filter store is not initialized")
	}
	if record.ReasoningTokens == nil || record.MatchedLength == nil {
		return fmt.Errorf("codex retry filter hit requires reasoning tokens and matched length")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record = normalizeAttempt(record)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_hits (
	request_id, occurred_at, provider_key, auth_id, auth_label, model, client_model,
	response_model, stream, reasoning_tokens, matched_length, action,
	guard_retry_remaining, attempt, retried, final_success, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID,
		record.OccurredAt.UTC().UnixMilli(),
		record.ProviderKey,
		record.AuthID,
		record.AuthLabel,
		record.Model,
		record.ClientModel,
		record.ResponseModel,
		boolToInt(record.Stream),
		*record.ReasoningTokens,
		*record.MatchedLength,
		record.Action,
		record.GuardRetryRemaining,
		record.Attempt,
		boolToInt(record.Action == ActionInternalRetry || record.Action == ActionConductorRetry),
		boolToInt(record.FinalSuccess),
		record.MetadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert codex retry filter hit: %w", err)
	}
	return nil
}

func (s *Store) MarkFinalSuccess(ctx context.Context, requestID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("codex retry filter store is not initialized")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE codex_response_retry_filter_attempts SET final_success = 1 WHERE request_id = ?`, requestID); err != nil {
		return fmt.Errorf("mark codex retry filter attempt final success: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE codex_response_retry_filter_hits SET final_success = 1 WHERE request_id = ?`, requestID); err != nil {
		return fmt.Errorf("mark codex retry filter final success: %w", err)
	}
	return nil
}

func (s *Store) QueryHits(ctx context.Context, filter QueryFilter) ([]HitRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	where, args := buildWhere(filter)
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, request_id, occurred_at, provider_key, auth_id, auth_label, model,
	client_model, response_model, stream, reasoning_tokens, matched_length, action,
	guard_retry_remaining, attempt, retried, final_success, metadata_json
FROM codex_response_retry_filter_hits`+where+`
ORDER BY occurred_at DESC, id DESC
LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter hits: %w", err)
	}
	defer rows.Close()
	var out []HitRecord
	for rows.Next() {
		hit, errScan := scanHit(rows)
		if errScan != nil {
			return nil, errScan
		}
		out = append(out, hit)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate codex retry filter hits: %w", errRows)
	}
	return out, nil
}

func (s *Store) QueryStats(ctx context.Context, filter QueryFilter) (Stats, error) {
	if s == nil || s.db == nil {
		return Stats{}, fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	attemptsWhere, attemptsArgs := buildAttemptWhere(filter)
	hitsWhere, hitsArgs := buildWhere(filter)
	var stats Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM codex_response_retry_filter_attempts`+attemptsWhere, attemptsArgs...).Scan(&stats.Attempts); err != nil {
		return Stats{}, fmt.Errorf("count codex retry filter attempts: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN final_success = 1 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN 1 ELSE 0 END), 0)
FROM codex_response_retry_filter_hits`+hitsWhere,
		append([]any{ActionInternalRetry, ActionConductorRetry, ActionObserveOnly}, hitsArgs...)...).Scan(
		&stats.Hits,
		&stats.FinalSuccessesAfterHit,
		&stats.InternalRetries,
		&stats.ConductorRetries,
		&stats.ObserveOnlyHits,
	); err != nil {
		return Stats{}, fmt.Errorf("count codex retry filter hits: %w", err)
	}
	stats.HitRate = ratio(stats.Hits, stats.Attempts)
	stats.RetrySuccessRate = ratio(stats.FinalSuccessesAfterHit, stats.Hits)
	var err error
	stats.ByModel, err = s.queryBreakdown(ctx, filter, "model")
	if err != nil {
		return Stats{}, err
	}
	stats.ByAuth, err = s.queryBreakdown(ctx, filter, "auth_id")
	if err != nil {
		return Stats{}, err
	}
	stats.ByReasoningTokens, err = s.queryReasoningBreakdown(ctx, filter)
	if err != nil {
		return Stats{}, err
	}
	stats.ByAction, err = s.queryActionBreakdown(ctx, filter)
	if err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func normalizeAttempt(record AttemptRecord) AttemptRecord {
	record.RequestID = strings.TrimSpace(record.RequestID)
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now().UTC()
	}
	record.ProviderKey = strings.TrimSpace(record.ProviderKey)
	if record.ProviderKey == "" {
		record.ProviderKey = "codex"
	}
	record.AuthID = strings.TrimSpace(record.AuthID)
	record.AuthLabel = strings.TrimSpace(record.AuthLabel)
	record.Model = strings.TrimSpace(record.Model)
	record.ClientModel = strings.TrimSpace(record.ClientModel)
	record.ResponseModel = strings.TrimSpace(record.ResponseModel)
	record.Action = strings.TrimSpace(record.Action)
	if record.Action == "" {
		record.Action = ActionPass
	}
	if record.Attempt <= 0 {
		record.Attempt = 1
	}
	return record
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intToBool(v int) bool {
	return v != 0
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func ratio(num, denom int64) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom)
}
