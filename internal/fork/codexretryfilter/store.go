package codexretryfilter

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultSQLitePath = "usage.sqlite3"
const retryFilterRollupVersion = "1"

type Store struct {
	db           *sql.DB
	statsCacheMu sync.Mutex
	statsCache   map[string]statsCacheEntry
}

type statsCacheEntry struct {
	expiresAt time.Time
	stats     Stats
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
	store := &Store{db: db, statsCache: map[string]statsCacheEntry{}}
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
	if err := s.backfillRollups(ctx); err != nil {
		return err
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
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin codex retry filter attempt insert: %w", errBegin)
	}
	defer func() { _ = tx.Rollback() }()
	_, err := tx.ExecContext(ctx, `
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
	if err := upsertAttemptRollup(ctx, tx, record); err != nil {
		return err
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit codex retry filter attempt insert: %w", errCommit)
	}
	s.invalidateStatsCache()
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
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin codex retry filter hit insert: %w", errBegin)
	}
	defer func() { _ = tx.Rollback() }()
	_, err := tx.ExecContext(ctx, `
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
	if err := upsertHitRollup(ctx, tx, record); err != nil {
		return err
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit codex retry filter hit insert: %w", errCommit)
	}
	s.invalidateStatsCache()
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
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin codex retry filter final success update: %w", errBegin)
	}
	defer func() { _ = tx.Rollback() }()
	pendingHitUpdates, err := pendingHitFinalSuccessRollups(ctx, tx, requestID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE codex_response_retry_filter_attempts SET final_success = 1 WHERE request_id = ?`, requestID); err != nil {
		return fmt.Errorf("mark codex retry filter attempt final success: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE codex_response_retry_filter_hits SET final_success = 1 WHERE request_id = ?`, requestID); err != nil {
		return fmt.Errorf("mark codex retry filter final success: %w", err)
	}
	for _, update := range pendingHitUpdates {
		if _, err := tx.ExecContext(ctx, `
UPDATE codex_response_retry_filter_hits_rollup_hourly
SET final_successes = final_successes + ?
WHERE bucket_start = ? AND model = ? AND auth_id = ? AND action = ? AND matched_length = ?`,
			update.count,
			update.bucketStart,
			update.model,
			update.authID,
			update.action,
			update.matchedLength,
		); err != nil {
			return fmt.Errorf("mark codex retry filter rollup final success: %w", err)
		}
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit codex retry filter final success update: %w", errCommit)
	}
	s.invalidateStatsCache()
	return nil
}

func (s *Store) QueryHits(ctx context.Context, filter QueryFilter) (HitsResult, error) {
	if s == nil || s.db == nil {
		return HitsResult{}, fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	where, args := buildWhere(filter)
	limit := filter.Limit
	useOffset := filter.Offset > 0 && filter.BeforeTime == nil
	query := `
SELECT id, request_id, occurred_at, provider_key, auth_id, auth_label, model,
	client_model, response_model, stream, reasoning_tokens, matched_length, action,
	guard_retry_remaining, attempt, retried, final_success, metadata_json
FROM codex_response_retry_filter_hits` + where + `
ORDER BY occurred_at DESC, id DESC
LIMIT ?`
	if useOffset {
		args = append(args, limit, filter.Offset)
		query += ` OFFSET ?`
	} else {
		args = append(args, limit+1)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return HitsResult{}, fmt.Errorf("query codex retry filter hits: %w", err)
	}
	defer rows.Close()
	var out []HitRecord
	for rows.Next() {
		hit, errScan := scanHit(rows)
		if errScan != nil {
			return HitsResult{}, errScan
		}
		out = append(out, hit)
	}
	if errRows := rows.Err(); errRows != nil {
		return HitsResult{}, fmt.Errorf("iterate codex retry filter hits: %w", errRows)
	}
	result := HitsResult{Hits: out}
	if useOffset {
		if len(out) > limit {
			result.Hits = out[:limit]
			result.HasMore = true
		}
		return result, nil
	}
	if len(out) > limit {
		last := out[limit-1]
		nextTime := last.OccurredAt
		nextID := last.ID
		result.Hits = out[:limit]
		result.NextBeforeOccurred = &nextTime
		result.NextBeforeID = &nextID
		result.HasMore = true
	}
	return result, nil
}

func (s *Store) QueryStats(ctx context.Context, filter QueryFilter) (Stats, error) {
	if s == nil || s.db == nil {
		return Stats{}, fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeStatsFilter(filter)
	cacheKey := statsCacheKey(filter)
	if stats, ok := s.getCachedStats(cacheKey); ok {
		return stats, nil
	}
	if plan, ok := buildRetryRollupPlan(filter); ok {
		stats, err := s.queryStatsMixed(ctx, filter, plan)
		if err != nil {
			return Stats{}, err
		}
		s.putCachedStats(cacheKey, stats)
		return stats, nil
	}
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
	s.putCachedStats(cacheKey, stats)
	return stats, nil
}

func (s *Store) invalidateStatsCache() {
	if s == nil {
		return
	}
	s.statsCacheMu.Lock()
	s.statsCache = map[string]statsCacheEntry{}
	s.statsCacheMu.Unlock()
}

func (s *Store) getCachedStats(key string) (Stats, bool) {
	if s == nil {
		return Stats{}, false
	}
	s.statsCacheMu.Lock()
	defer s.statsCacheMu.Unlock()
	entry, ok := s.statsCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(s.statsCache, key)
		}
		return Stats{}, false
	}
	return cloneStats(entry.stats), true
}

func (s *Store) putCachedStats(key string, stats Stats) {
	if s == nil {
		return
	}
	s.statsCacheMu.Lock()
	s.statsCache[key] = statsCacheEntry{
		expiresAt: time.Now().Add(statsCacheTTL),
		stats:     cloneStats(stats),
	}
	s.statsCacheMu.Unlock()
}

func cloneStats(stats Stats) Stats {
	stats.ByModel = append([]Breakdown(nil), stats.ByModel...)
	stats.ByAuth = append([]Breakdown(nil), stats.ByAuth...)
	stats.ByReasoningTokens = append([]ReasoningBreakdown(nil), stats.ByReasoningTokens...)
	stats.ByAction = append([]ActionBreakdown(nil), stats.ByAction...)
	return stats
}

func (s *Store) PruneOlderThan(ctx context.Context, before time.Time) (PruneResult, error) {
	if s == nil || s.db == nil {
		return PruneResult{}, fmt.Errorf("codex retry filter store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	before = before.UTC()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return PruneResult{}, fmt.Errorf("begin codex retry filter prune: %w", errBegin)
	}
	defer func() { _ = tx.Rollback() }()

	result := PruneResult{Before: before}
	resAttempts, err := tx.ExecContext(ctx, `DELETE FROM codex_response_retry_filter_attempts WHERE occurred_at < ?`, before.UnixMilli())
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune codex retry filter attempts: %w", err)
	}
	resHits, err := tx.ExecContext(ctx, `DELETE FROM codex_response_retry_filter_hits WHERE occurred_at < ?`, before.UnixMilli())
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune codex retry filter hits: %w", err)
	}
	if result.DeletedAttempts, err = resAttempts.RowsAffected(); err != nil {
		return PruneResult{}, fmt.Errorf("count pruned codex retry filter attempts: %w", err)
	}
	if result.DeletedHits, err = resHits.RowsAffected(); err != nil {
		return PruneResult{}, fmt.Errorf("count pruned codex retry filter hits: %w", err)
	}
	if err := rebuildRollupsTx(ctx, tx); err != nil {
		return PruneResult{}, err
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return PruneResult{}, fmt.Errorf("commit codex retry filter prune: %w", errCommit)
	}
	s.invalidateStatsCache()
	return result, nil
}

func (s *Store) backfillRollups(ctx context.Context) error {
	var storedVersion string
	errVersion := s.db.QueryRowContext(ctx, "SELECT value FROM codex_response_retry_filter_meta WHERE key = 'rollup_version'").Scan(&storedVersion)
	if errVersion != nil && errVersion != sql.ErrNoRows {
		return fmt.Errorf("read codex retry filter rollup version: %w", errVersion)
	}
	if storedVersion == retryFilterRollupVersion {
		return nil
	}
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return fmt.Errorf("begin codex retry filter rollup rebuild: %w", errBegin)
	}
	defer func() { _ = tx.Rollback() }()
	if err := rebuildRollupsTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_meta(key, value) VALUES ('rollup_version', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, retryFilterRollupVersion); err != nil {
		return fmt.Errorf("write codex retry filter rollup version: %w", err)
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return fmt.Errorf("commit codex retry filter rollup rebuild: %w", errCommit)
	}
	return nil
}

func upsertAttemptRollup(ctx context.Context, tx *sql.Tx, record AttemptRecord) error {
	bucketStart := record.OccurredAt.UTC().Truncate(time.Hour).UnixMilli()
	_, err := tx.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_attempts_rollup_hourly (
	bucket_start, model, auth_id, auth_label, action, reasoning_tokens, attempts
) VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(bucket_start, model, auth_id, action, reasoning_tokens) DO UPDATE SET
	auth_label = CASE
		WHEN excluded.auth_label != '' THEN excluded.auth_label
		ELSE codex_response_retry_filter_attempts_rollup_hourly.auth_label
	END,
	attempts = attempts + 1`,
		bucketStart,
		record.Model,
		record.AuthID,
		record.AuthLabel,
		record.Action,
		int64Value(record.ReasoningTokens),
	)
	if err != nil {
		return fmt.Errorf("upsert codex retry filter attempts rollup: %w", err)
	}
	return nil
}

func upsertHitRollup(ctx context.Context, tx *sql.Tx, record AttemptRecord) error {
	bucketStart := record.OccurredAt.UTC().Truncate(time.Hour).UnixMilli()
	finalSuccess := 0
	if record.FinalSuccess {
		finalSuccess = 1
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_hits_rollup_hourly (
	bucket_start, model, auth_id, auth_label, action, matched_length, hits, final_successes
) VALUES (?, ?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(bucket_start, model, auth_id, action, matched_length) DO UPDATE SET
	auth_label = CASE
		WHEN excluded.auth_label != '' THEN excluded.auth_label
		ELSE codex_response_retry_filter_hits_rollup_hourly.auth_label
	END,
	hits = hits + 1,
	final_successes = final_successes + excluded.final_successes`,
		bucketStart,
		record.Model,
		record.AuthID,
		record.AuthLabel,
		record.Action,
		int64Value(record.MatchedLength),
		finalSuccess,
	)
	if err != nil {
		return fmt.Errorf("upsert codex retry filter hits rollup: %w", err)
	}
	return nil
}

func int64Value(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

type hitFinalSuccessRollupUpdate struct {
	bucketStart   int64
	model         string
	authID        string
	action        string
	matchedLength int64
	count         int64
}

func pendingHitFinalSuccessRollups(ctx context.Context, tx *sql.Tx, requestID string) ([]hitFinalSuccessRollupUpdate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT
	(occurred_at / 3600000) * 3600000 AS bucket_start,
	COALESCE(model, ''),
	COALESCE(auth_id, ''),
	COALESCE(action, ''),
	COALESCE(matched_length, 0),
	COUNT(*)
FROM codex_response_retry_filter_hits
WHERE request_id = ? AND final_success = 0
GROUP BY 1, 2, 3, 4, 5`, requestID)
	if err != nil {
		return nil, fmt.Errorf("query pending codex retry filter rollup final success updates: %w", err)
	}
	defer rows.Close()
	var updates []hitFinalSuccessRollupUpdate
	for rows.Next() {
		var update hitFinalSuccessRollupUpdate
		if errScan := rows.Scan(&update.bucketStart, &update.model, &update.authID, &update.action, &update.matchedLength, &update.count); errScan != nil {
			return nil, fmt.Errorf("scan pending codex retry filter rollup final success update: %w", errScan)
		}
		updates = append(updates, update)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate pending codex retry filter rollup final success updates: %w", errRows)
	}
	return updates, nil
}

func rebuildRollupsTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM codex_response_retry_filter_attempts_rollup_hourly"); err != nil {
		return fmt.Errorf("clear codex retry filter attempts rollup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM codex_response_retry_filter_hits_rollup_hourly"); err != nil {
		return fmt.Errorf("clear codex retry filter hits rollup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_attempts_rollup_hourly (
	bucket_start, model, auth_id, auth_label, action, reasoning_tokens, attempts
)
SELECT
	(occurred_at / 3600000) * 3600000,
	COALESCE(model, ''),
	COALESCE(auth_id, ''),
	MAX(COALESCE(NULLIF(auth_label, ''), '')),
	COALESCE(action, ''),
	COALESCE(reasoning_tokens, 0),
	COUNT(*)
FROM codex_response_retry_filter_attempts
GROUP BY 1, 2, 3, 5, 6`); err != nil {
		return fmt.Errorf("backfill codex retry filter attempts rollup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO codex_response_retry_filter_hits_rollup_hourly (
	bucket_start, model, auth_id, auth_label, action, matched_length, hits, final_successes
)
SELECT
	(occurred_at / 3600000) * 3600000,
	COALESCE(model, ''),
	COALESCE(auth_id, ''),
	MAX(COALESCE(NULLIF(auth_label, ''), '')),
	COALESCE(action, ''),
	COALESCE(matched_length, 0),
	COUNT(*),
	COALESCE(SUM(CASE WHEN final_success = 1 THEN 1 ELSE 0 END), 0)
FROM codex_response_retry_filter_hits
GROUP BY 1, 2, 3, 5, 6`); err != nil {
		return fmt.Errorf("backfill codex retry filter hits rollup: %w", err)
	}
	return nil
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
