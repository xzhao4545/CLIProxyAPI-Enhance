package codexretryfilter

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHitsLimit = 100
	maxHitsLimit     = 500
	statsCacheTTL    = 15 * time.Second
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
	if filter.BeforeID < 0 {
		filter.BeforeID = 0
	}
	return filter
}

func normalizeStatsFilter(filter QueryFilter) QueryFilter {
	filter = normalizeQueryFilter(filter)
	filter.BeforeTime = nil
	filter.BeforeID = 0
	filter.Offset = 0
	return filter
}

func buildWhere(filter QueryFilter) (string, []any) {
	return buildWhereForTable(filter, false, true)
}

func buildAttemptWhere(filter QueryFilter) (string, []any) {
	return buildWhereForTable(filter, true, true)
}

func buildWhereForTable(filter QueryFilter, attempts bool, includeCursor bool) (string, []any) {
	var parts []string
	args := make([]any, 0, 9)
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
	if includeCursor && filter.BeforeTime != nil {
		beforeMS := filter.BeforeTime.UTC().UnixMilli()
		beforeID := filter.BeforeID
		if beforeID <= 0 {
			beforeID = 1<<63 - 1
		}
		add("(occurred_at < ? OR (occurred_at = ? AND id < ?))", beforeMS, beforeMS, beforeID)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func statsCacheKey(filter QueryFilter) string {
	var b strings.Builder
	if filter.DateFrom != nil {
		b.WriteString("from=")
		b.WriteString(strconv.FormatInt(filter.DateFrom.UTC().UnixMilli(), 10))
	}
	b.WriteString("|to=")
	if filter.DateTo != nil {
		b.WriteString(strconv.FormatInt(filter.DateTo.UTC().UnixMilli(), 10))
	}
	b.WriteString("|model=")
	b.WriteString(filter.Model)
	b.WriteString("|auth=")
	b.WriteString(filter.AuthID)
	b.WriteString("|matched=")
	b.WriteString(strconv.FormatInt(filter.MatchedLength, 10))
	b.WriteString("|action=")
	b.WriteString(filter.Action)
	return b.String()
}

type retryRollupPlan struct {
	fullHours *QueryFilter
	partials  []QueryFilter
}

func buildRetryRollupPlan(filter QueryFilter) (retryRollupPlan, bool) {
	if filter.DateFrom == nil || filter.DateTo == nil {
		return retryRollupPlan{}, false
	}
	var plan retryRollupPlan
	from := filter.DateFrom
	to := filter.DateTo
	fullFrom := from
	fullTo := to
	if from != nil {
		ceil := ceilHour(*from)
		fullFrom = &ceil
		if from.Before(ceil) {
			headTo := ceil
			if to.Before(headTo) {
				headTo = *to
			}
			if from.Before(headTo) {
				head := filter
				head.DateFrom = from
				head.DateTo = &headTo
				plan.partials = append(plan.partials, head)
			}
		}
	}
	if to != nil {
		floor := to.UTC().Truncate(time.Hour)
		fullTo = &floor
		if floor.Before(*to) {
			tailFrom := floor
			if tailFrom.Before(*from) {
				tailFrom = *from
			}
			if tailFrom.Before(*to) && !retryPartialCovered(plan.partials, tailFrom, *to) {
				tail := filter
				tail.DateFrom = &tailFrom
				tail.DateTo = to
				plan.partials = append(plan.partials, tail)
			}
		}
	}
	if fullFrom.Before(*fullTo) {
		full := filter
		full.DateFrom = fullFrom
		full.DateTo = fullTo
		plan.fullHours = &full
	}
	if plan.fullHours == nil && len(plan.partials) == 0 {
		return retryRollupPlan{}, false
	}
	return plan, true
}

func retryPartialCovered(partials []QueryFilter, from, to time.Time) bool {
	for _, partial := range partials {
		if partial.DateFrom == nil || partial.DateTo == nil {
			continue
		}
		if partial.DateFrom.Equal(from) && partial.DateTo.Equal(to) {
			return true
		}
	}
	return false
}

func ceilHour(value time.Time) time.Time {
	value = value.UTC()
	floor := value.Truncate(time.Hour)
	if value.Equal(floor) {
		return floor
	}
	return floor.Add(time.Hour)
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
	includeLabel := column == "auth_id"
	hitsWhere, hitsArgs := buildWhere(filter)
	attemptsWhere, attemptsArgs := buildAttemptWhere(filter)
	attempts, err := s.countByColumn(ctx, "codex_response_retry_filter_attempts", column, attemptsWhere, attemptsArgs, includeLabel)
	if err != nil {
		return nil, err
	}
	selectLabel := "''"
	if includeLabel {
		selectLabel = "MAX(COALESCE(NULLIF(auth_label, ''), ''))"
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key,
	`+selectLabel+` AS label,
	COUNT(*) AS hits,
	COALESCE(SUM(CASE WHEN final_success = 1 THEN 1 ELSE 0 END), 0) AS final_successes
FROM codex_response_retry_filter_hits`+hitsWhere+`
GROUP BY COALESCE(`+column+`, '')
ORDER BY hits DESC, key ASC`, hitsArgs...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter %s breakdown: %w", column, err)
	}
	defer rows.Close()
	type hitBreakdown struct {
		label     string
		hits      int64
		successes int64
	}
	hits := make(map[string]hitBreakdown)
	for rows.Next() {
		var key string
		var label string
		var hitCount int64
		var successes int64
		if errScan := rows.Scan(&key, &label, &hitCount, &successes); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter %s breakdown: %w", column, errScan)
		}
		hits[key] = hitBreakdown{label: label, hits: hitCount, successes: successes}
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate codex retry filter %s breakdown: %w", column, errRows)
	}

	keys := make([]string, 0, len(attempts))
	for key := range attempts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := hits[keys[i]]
		right := hits[keys[j]]
		if left.hits != right.hits {
			return left.hits > right.hits
		}
		if attempts[keys[i]].Attempts != attempts[keys[j]].Attempts {
			return attempts[keys[i]].Attempts > attempts[keys[j]].Attempts
		}
		return keys[i] < keys[j]
	})

	out := make([]Breakdown, 0, len(keys))
	for _, key := range keys {
		hit := hits[key]
		row := Breakdown{
			Key:              key,
			Label:            attempts[key].Label,
			Attempts:         attempts[key].Attempts,
			Hits:             hit.hits,
			HitRate:          ratio(hit.hits, attempts[key].Attempts),
			RetrySuccessRate: ratio(hit.successes, hit.hits),
		}
		if includeLabel && row.Label == "" {
			row.Label = hit.label
		}
		out = append(out, row)
	}
	return out, nil
}

type countRow struct {
	Attempts int64
	Label    string
}

func (s *Store) countByColumn(ctx context.Context, table, column, where string, args []any, includeLabel bool) (map[string]countRow, error) {
	selectLabel := "''"
	if includeLabel {
		selectLabel = "MAX(COALESCE(NULLIF(auth_label, ''), ''))"
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key, `+selectLabel+` AS label, COUNT(*)
FROM `+table+where+`
GROUP BY COALESCE(`+column+`, '')`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter %s counts: %w", column, err)
	}
	defer rows.Close()
	out := map[string]countRow{}
	for rows.Next() {
		var key string
		var label string
		var count int64
		if errScan := rows.Scan(&key, &label, &count); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter %s counts: %w", column, errScan)
		}
		out[key] = countRow{Attempts: count, Label: label}
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

func (s *Store) queryStatsMixed(ctx context.Context, original QueryFilter, plan retryRollupPlan) (Stats, error) {
	var combined Stats
	if plan.fullHours != nil {
		stats, errRollup := s.queryStatsRollup(ctx, *plan.fullHours)
		if errRollup != nil {
			return Stats{}, errRollup
		}
		mergeStatsInto(&combined, stats)
	}
	for _, partial := range plan.partials {
		stats, errRaw := s.queryStatsRaw(ctx, partial)
		if errRaw != nil {
			return Stats{}, errRaw
		}
		mergeStatsInto(&combined, stats)
	}
	finalizeStats(&combined)
	if original.Model != "" || original.AuthID != "" || original.MatchedLength > 0 || original.Action != "" {
		return combined, nil
	}
	return combined, nil
}

func (s *Store) queryStatsRaw(ctx context.Context, filter QueryFilter) (Stats, error) {
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
	finalizeStats(&stats)
	return stats, nil
}

func (s *Store) queryStatsRollup(ctx context.Context, filter QueryFilter) (Stats, error) {
	var stats Stats
	attemptsWhere, attemptsArgs := buildAttemptsRollupWhere(filter)
	hitsWhere, hitsArgs := buildHitsRollupWhere(filter)
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(attempts), 0) FROM codex_response_retry_filter_attempts_rollup_hourly`+attemptsWhere, attemptsArgs...).Scan(&stats.Attempts); err != nil {
		return Stats{}, fmt.Errorf("count codex retry filter attempts rollup: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT
	COALESCE(SUM(hits), 0),
	COALESCE(SUM(final_successes), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN hits ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN hits ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN action = ? THEN hits ELSE 0 END), 0)
FROM codex_response_retry_filter_hits_rollup_hourly`+hitsWhere,
		append([]any{ActionInternalRetry, ActionConductorRetry, ActionObserveOnly}, hitsArgs...)...).Scan(
		&stats.Hits,
		&stats.FinalSuccessesAfterHit,
		&stats.InternalRetries,
		&stats.ConductorRetries,
		&stats.ObserveOnlyHits,
	); err != nil {
		return Stats{}, fmt.Errorf("count codex retry filter hits rollup: %w", err)
	}
	var err error
	stats.ByModel, err = s.queryBreakdownRollup(ctx, filter, "model")
	if err != nil {
		return Stats{}, err
	}
	stats.ByAuth, err = s.queryBreakdownRollup(ctx, filter, "auth_id")
	if err != nil {
		return Stats{}, err
	}
	stats.ByReasoningTokens, err = s.queryReasoningBreakdownRollup(ctx, filter)
	if err != nil {
		return Stats{}, err
	}
	stats.ByAction, err = s.queryActionBreakdownRollup(ctx, filter)
	if err != nil {
		return Stats{}, err
	}
	finalizeStats(&stats)
	return stats, nil
}

func buildAttemptsRollupWhere(filter QueryFilter) (string, []any) {
	return buildRollupWhere(filter, true)
}

func buildHitsRollupWhere(filter QueryFilter) (string, []any) {
	return buildRollupWhere(filter, false)
}

func buildRollupWhere(filter QueryFilter, attempts bool) (string, []any) {
	var parts []string
	args := make([]any, 0, 8)
	add := func(expr string, values ...any) {
		parts = append(parts, expr)
		args = append(args, values...)
	}
	if filter.DateFrom != nil {
		add("bucket_start >= ?", filter.DateFrom.UTC().UnixMilli())
	}
	if filter.DateTo != nil {
		add("bucket_start < ?", filter.DateTo.UTC().UnixMilli())
	}
	if filter.Model != "" {
		add("model = ?", filter.Model)
	}
	if filter.AuthID != "" {
		add("auth_id = ?", filter.AuthID)
	}
	if filter.Action != "" {
		add("action = ?", filter.Action)
	}
	if filter.MatchedLength > 0 {
		if attempts {
			add("reasoning_tokens = ?", filter.MatchedLength)
		} else {
			add("matched_length = ?", filter.MatchedLength)
		}
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func (s *Store) queryBreakdownRollup(ctx context.Context, filter QueryFilter, column string) ([]Breakdown, error) {
	if column != "model" && column != "auth_id" {
		return nil, fmt.Errorf("unsupported codex retry filter rollup breakdown column %q", column)
	}
	includeLabel := column == "auth_id"
	attemptsWhere, attemptsArgs := buildAttemptsRollupWhere(filter)
	hitsWhere, hitsArgs := buildHitsRollupWhere(filter)
	attempts, err := s.countByColumnRollup(ctx, "codex_response_retry_filter_attempts_rollup_hourly", column, "attempts", attemptsWhere, attemptsArgs, includeLabel)
	if err != nil {
		return nil, err
	}
	hits, err := s.countHitsBreakdownRollup(ctx, column, hitsWhere, hitsArgs, includeLabel)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(attempts))
	for key := range attempts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := hits[keys[i]]
		right := hits[keys[j]]
		if left.Hits != right.Hits {
			return left.Hits > right.Hits
		}
		if attempts[keys[i]].Attempts != attempts[keys[j]].Attempts {
			return attempts[keys[i]].Attempts > attempts[keys[j]].Attempts
		}
		return keys[i] < keys[j]
	})
	out := make([]Breakdown, 0, len(keys))
	for _, key := range keys {
		attempt := attempts[key]
		hit := hits[key]
		label := attempt.Label
		if includeLabel && label == "" {
			label = hit.Label
		}
		out = append(out, Breakdown{
			Key:              key,
			Label:            label,
			Attempts:         attempt.Attempts,
			Hits:             hit.Hits,
			HitRate:          ratio(hit.Hits, attempt.Attempts),
			RetrySuccessRate: ratio(hit.FinalSuccesses, hit.Hits),
		})
	}
	return out, nil
}

type rollupCountRow struct {
	Attempts int64
	Label    string
}

type rollupHitBreakdown struct {
	Hits           int64
	FinalSuccesses int64
	Label          string
}

func (s *Store) countByColumnRollup(ctx context.Context, table, column, metricColumn, where string, args []any, includeLabel bool) (map[string]rollupCountRow, error) {
	selectLabel := "''"
	if includeLabel {
		selectLabel = "MAX(COALESCE(NULLIF(auth_label, ''), ''))"
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key, `+selectLabel+` AS label, COALESCE(SUM(`+metricColumn+`), 0)
FROM `+table+where+`
GROUP BY COALESCE(`+column+`, '')`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter rollup %s counts: %w", column, err)
	}
	defer rows.Close()
	out := map[string]rollupCountRow{}
	for rows.Next() {
		var key, label string
		var count int64
		if errScan := rows.Scan(&key, &label, &count); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter rollup %s counts: %w", column, errScan)
		}
		out[key] = rollupCountRow{Attempts: count, Label: label}
	}
	return out, rows.Err()
}

func (s *Store) countHitsBreakdownRollup(ctx context.Context, column, where string, args []any, includeLabel bool) (map[string]rollupHitBreakdown, error) {
	selectLabel := "''"
	if includeLabel {
		selectLabel = "MAX(COALESCE(NULLIF(auth_label, ''), ''))"
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(`+column+`, '') AS key,
	`+selectLabel+` AS label,
	COALESCE(SUM(hits), 0) AS hits,
	COALESCE(SUM(final_successes), 0) AS final_successes
FROM codex_response_retry_filter_hits_rollup_hourly`+where+`
GROUP BY COALESCE(`+column+`, '')`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter rollup %s breakdown: %w", column, err)
	}
	defer rows.Close()
	out := map[string]rollupHitBreakdown{}
	for rows.Next() {
		var key, label string
		var hits int64
		var finalSuccesses int64
		if errScan := rows.Scan(&key, &label, &hits, &finalSuccesses); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter rollup %s breakdown: %w", column, errScan)
		}
		out[key] = rollupHitBreakdown{Hits: hits, FinalSuccesses: finalSuccesses, Label: label}
	}
	return out, rows.Err()
}

func (s *Store) queryReasoningBreakdownRollup(ctx context.Context, filter QueryFilter) ([]ReasoningBreakdown, error) {
	where, args := buildHitsRollupWhere(filter)
	rows, err := s.db.QueryContext(ctx, `
SELECT matched_length, COALESCE(SUM(hits), 0)
FROM codex_response_retry_filter_hits_rollup_hourly`+where+`
GROUP BY matched_length
ORDER BY COALESCE(SUM(hits), 0) DESC, matched_length ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter rollup reasoning breakdown: %w", err)
	}
	defer rows.Close()
	var out []ReasoningBreakdown
	for rows.Next() {
		var row ReasoningBreakdown
		if errScan := rows.Scan(&row.MatchedLength, &row.Hits); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter rollup reasoning breakdown: %w", errScan)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) queryActionBreakdownRollup(ctx context.Context, filter QueryFilter) ([]ActionBreakdown, error) {
	where, args := buildHitsRollupWhere(filter)
	rows, err := s.db.QueryContext(ctx, `
SELECT action, COALESCE(SUM(hits), 0)
FROM codex_response_retry_filter_hits_rollup_hourly`+where+`
GROUP BY action
ORDER BY COALESCE(SUM(hits), 0) DESC, action ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex retry filter rollup action breakdown: %w", err)
	}
	defer rows.Close()
	var out []ActionBreakdown
	for rows.Next() {
		var row ActionBreakdown
		if errScan := rows.Scan(&row.Action, &row.Hits); errScan != nil {
			return nil, fmt.Errorf("scan codex retry filter rollup action breakdown: %w", errScan)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func mergeStatsInto(target *Stats, incoming Stats) {
	target.Attempts += incoming.Attempts
	target.Hits += incoming.Hits
	target.FinalSuccessesAfterHit += incoming.FinalSuccessesAfterHit
	target.InternalRetries += incoming.InternalRetries
	target.ConductorRetries += incoming.ConductorRetries
	target.ObserveOnlyHits += incoming.ObserveOnlyHits
	target.ByModel = mergeBreakdowns(target.ByModel, incoming.ByModel)
	target.ByAuth = mergeBreakdowns(target.ByAuth, incoming.ByAuth)
	target.ByReasoningTokens = mergeReasoningBreakdowns(target.ByReasoningTokens, incoming.ByReasoningTokens)
	target.ByAction = mergeActionBreakdowns(target.ByAction, incoming.ByAction)
}

func finalizeStats(stats *Stats) {
	stats.HitRate = ratio(stats.Hits, stats.Attempts)
	stats.RetrySuccessRate = ratio(stats.FinalSuccessesAfterHit, stats.Hits)
	for i := range stats.ByModel {
		stats.ByModel[i].HitRate = ratio(stats.ByModel[i].Hits, stats.ByModel[i].Attempts)
		stats.ByModel[i].RetrySuccessRate = ratio(int64Float(stats.ByModel[i].RetrySuccessRate*float64(stats.ByModel[i].Hits)), stats.ByModel[i].Hits)
	}
	for i := range stats.ByAuth {
		stats.ByAuth[i].HitRate = ratio(stats.ByAuth[i].Hits, stats.ByAuth[i].Attempts)
		stats.ByAuth[i].RetrySuccessRate = ratio(int64Float(stats.ByAuth[i].RetrySuccessRate*float64(stats.ByAuth[i].Hits)), stats.ByAuth[i].Hits)
	}
}

func mergeBreakdowns(target, incoming []Breakdown) []Breakdown {
	type breakdownAccumulator struct {
		Attempts       int64
		Hits           int64
		FinalSuccesses int64
		Label          string
	}
	acc := map[string]breakdownAccumulator{}
	order := make([]string, 0, len(target)+len(incoming))
	appendRow := func(row Breakdown) {
		item, exists := acc[row.Key]
		if !exists {
			order = append(order, row.Key)
		}
		item.Attempts += row.Attempts
		item.Hits += row.Hits
		item.FinalSuccesses += int64Float(row.RetrySuccessRate * float64(row.Hits))
		if item.Label == "" {
			item.Label = row.Label
		}
		acc[row.Key] = item
	}
	for _, row := range target {
		appendRow(row)
	}
	for _, row := range incoming {
		appendRow(row)
	}
	out := make([]Breakdown, 0, len(order))
	for _, key := range order {
		item := acc[key]
		out = append(out, Breakdown{
			Key:              key,
			Label:            item.Label,
			Attempts:         item.Attempts,
			Hits:             item.Hits,
			HitRate:          ratio(item.Hits, item.Attempts),
			RetrySuccessRate: ratio(item.FinalSuccesses, item.Hits),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		if out[i].Attempts != out[j].Attempts {
			return out[i].Attempts > out[j].Attempts
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func mergeReasoningBreakdowns(target, incoming []ReasoningBreakdown) []ReasoningBreakdown {
	acc := map[int64]int64{}
	for _, row := range target {
		acc[row.MatchedLength] += row.Hits
	}
	for _, row := range incoming {
		acc[row.MatchedLength] += row.Hits
	}
	keys := make([]int64, 0, len(acc))
	for key := range acc {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if acc[keys[i]] != acc[keys[j]] {
			return acc[keys[i]] > acc[keys[j]]
		}
		return keys[i] < keys[j]
	})
	out := make([]ReasoningBreakdown, 0, len(keys))
	for _, key := range keys {
		out = append(out, ReasoningBreakdown{MatchedLength: key, Hits: acc[key]})
	}
	return out
}

func mergeActionBreakdowns(target, incoming []ActionBreakdown) []ActionBreakdown {
	acc := map[string]int64{}
	for _, row := range target {
		acc[row.Action] += row.Hits
	}
	for _, row := range incoming {
		acc[row.Action] += row.Hits
	}
	keys := make([]string, 0, len(acc))
	for key := range acc {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if acc[keys[i]] != acc[keys[j]] {
			return acc[keys[i]] > acc[keys[j]]
		}
		return keys[i] < keys[j]
	})
	out := make([]ActionBreakdown, 0, len(keys))
	for _, key := range keys {
		out = append(out, ActionBreakdown{Action: key, Hits: acc[key]})
	}
	return out
}

func int64Float(value float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value + 0.5)
}
