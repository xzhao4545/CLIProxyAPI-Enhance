package codexretryfilter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	defaultHitsLimit = 100
	maxHitsLimit     = 500
)

func normalizeQueryFilter(filter QueryFilter) QueryFilter {
	filter.Model = strings.TrimSpace(filter.Model)
	filter.AuthID = strings.TrimSpace(filter.AuthID)
	filter.Action = strings.TrimSpace(filter.Action)
	if filter.Limit <= 0 {
		filter.Limit = defaultHitsLimit
	}
	if filter.Limit > maxHitsLimit {
		filter.Limit = maxHitsLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func buildWhere(filter QueryFilter) (string, []any) {
	return buildWhereForTable(filter, false)
}

func buildAttemptWhere(filter QueryFilter) (string, []any) {
	return buildWhereForTable(filter, true)
}

func buildWhereForTable(filter QueryFilter, attempts bool) (string, []any) {
	var parts []string
	args := make([]any, 0, 6)
	add := func(expr string, values ...any) {
		parts = append(parts, expr)
		args = append(args, values...)
	}
	if filter.DateFrom != nil {
		add("occurred_at >= ?", filter.DateFrom.UTC().UnixMilli())
	}
	if filter.DateTo != nil {
		add("occurred_at < ?", filter.DateTo.UTC().UnixMilli())
	}
	if filter.Model != "" {
		add("model = ?", filter.Model)
	}
	if filter.AuthID != "" {
		add("auth_id = ?", filter.AuthID)
	}
	if filter.MatchedLength > 0 {
		if attempts {
			add("reasoning_tokens = ?", filter.MatchedLength)
		} else {
			add("matched_length = ?", filter.MatchedLength)
		}
	}
	if filter.Action != "" {
		add("action = ?", filter.Action)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func scanHit(rows *sql.Rows) (HitRecord, error) {
	var hit HitRecord
	var occurredMS int64
	var streamFlag, retriedFlag, finalSuccessFlag int
	if err := rows.Scan(
		&hit.ID,
		&hit.RequestID,
		&occurredMS,
		&hit.ProviderKey,
		&hit.AuthID,
		&hit.AuthLabel,
		&hit.Model,
		&hit.ClientModel,
		&hit.ResponseModel,
		&streamFlag,
		&hit.ReasoningTokens,
		&hit.MatchedLength,
		&hit.Action,
		&hit.GuardRetryRemaining,
		&hit.Attempt,
		&retriedFlag,
		&finalSuccessFlag,
		&hit.MetadataJSON,
	); err != nil {
		return HitRecord{}, fmt.Errorf("scan codex retry filter hit: %w", err)
	}
	hit.OccurredAt = time.UnixMilli(occurredMS).UTC()
	hit.Stream = intToBool(streamFlag)
	hit.Retried = intToBool(retriedFlag)
	hit.FinalSuccess = intToBool(finalSuccessFlag)
	return hit, nil
}

func (s *Store) queryBreakdown(ctx context.Context, filter QueryFilter, column string) ([]Breakdown, error) {
	if column != "model" && column != "auth_id" {
		return nil, fmt.Errorf("unsupported codex retry filter breakdown column %q", column)
	}
	hitsWhere, hitsArgs := buildWhere(filter)
	attemptsWhere, attemptsArgs := buildAttemptWhere(filter)
	attempts, err := s.countByColumn(ctx, "codex_response_retry_filter_attempts", column, attemptsWhere, attemptsArgs)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key,
	COUNT(*) AS hits,
	COALESCE(SUM(CASE WHEN final_success = 1 THEN 1 ELSE 0 END), 0) AS final_successes
FROM codex_response_retry_filter_hits`+hitsWhere+`
GROUP BY COALESCE(`+column+`, '')
ORDER BY hits DESC, key ASC`, hitsArgs...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter %s breakdown: %w", column, err)
	}
	defer rows.Close()
	var out []Breakdown
	for rows.Next() {
		var row Breakdown
		var successes int64
		if errScan := rows.Scan(&row.Key, &row.Hits, &successes); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter %s breakdown: %w", column, errScan)
		}
		row.Attempts = attempts[row.Key]
		row.HitRate = ratio(row.Hits, row.Attempts)
		row.RetrySuccessRate = ratio(successes, row.Hits)
		out = append(out, row)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate codex retry filter %s breakdown: %w", column, errRows)
	}
	return out, nil
}

func (s *Store) countByColumn(ctx context.Context, table, column, where string, args []any) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key, COUNT(*)
FROM `+table+where+`
GROUP BY COALESCE(`+column+`, '')`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter %s counts: %w", column, err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if errScan := rows.Scan(&key, &count); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter %s counts: %w", column, errScan)
		}
		out[key] = count
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate codex retry filter %s counts: %w", column, errRows)
	}
	return out, nil
}

func (s *Store) queryReasoningBreakdown(ctx context.Context, filter QueryFilter) ([]ReasoningBreakdown, error) {
	where, args := buildWhere(filter)
	rows, err := s.db.QueryContext(ctx, `
SELECT matched_length, COUNT(*)
FROM codex_response_retry_filter_hits`+where+`
GROUP BY matched_length
ORDER BY COUNT(*) DESC, matched_length ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter reasoning breakdown: %w", err)
	}
	defer rows.Close()
	var out []ReasoningBreakdown
	for rows.Next() {
		var row ReasoningBreakdown
		if errScan := rows.Scan(&row.MatchedLength, &row.Hits); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter reasoning breakdown: %w", errScan)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) queryActionBreakdown(ctx context.Context, filter QueryFilter) ([]ActionBreakdown, error) {
	where, args := buildWhere(filter)
	rows, err := s.db.QueryContext(ctx, `
SELECT action, COUNT(*)
FROM codex_response_retry_filter_hits`+where+`
GROUP BY action
ORDER BY COUNT(*) DESC, action ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter action breakdown: %w", err)
	}
	defer rows.Close()
	var out []ActionBreakdown
	for rows.Next() {
		var row ActionBreakdown
		if errScan := rows.Scan(&row.Action, &row.Hits); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter action breakdown: %w", errScan)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
