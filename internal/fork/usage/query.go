package usage

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEventsLimit   = 100
	maxEventsLimit       = 1000
	defaultMetricsWindow = time.Hour
)

func (s *SQLiteStore) QueryEvents(filter QueryFilter) (EventsPage, error) {
	return s.QueryEventsContext(context.Background(), filter)
}

func (s *SQLiteStore) QueryEventsContext(ctx context.Context, filter QueryFilter) (EventsPage, error) {
	if s == nil || s.db == nil {
		return EventsPage{}, fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	where, args := buildWhere(filter, true)

	var total int64
	countQuery := "SELECT COUNT(*) FROM usage_events" + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return EventsPage{}, fmt.Errorf("count usage events: %w", err)
	}

	sortColumn := "started_at"
	switch filter.Sort {
	case "completed_at":
		sortColumn = "completed_at"
	case "duration_ms":
		sortColumn = "duration_ms"
	case "provider":
		sortColumn = "provider_key"
	case "model":
		sortColumn = "model"
	case "status":
		sortColumn = "status"
	}
	order := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		order = "ASC"
	}

	fields := eventSelectFields(filter.IncludeErrorRaw)
	query := "SELECT " + fields + " FROM usage_events" + where +
		" ORDER BY " + sortColumn + " " + order + ", id " + order + " LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, errQuery := s.db.QueryContext(ctx, query, args...)
	if errQuery != nil {
		return EventsPage{}, fmt.Errorf("query usage events: %w", errQuery)
	}
	defer rows.Close()

	events := make([]Event, 0, filter.Limit)
	for rows.Next() {
		event, errScan := scanEvent(rows, filter.IncludeErrorRaw)
		if errScan != nil {
			return EventsPage{}, errScan
		}
		events = append(events, event)
	}
	if errRows := rows.Err(); errRows != nil {
		return EventsPage{}, fmt.Errorf("iterate usage events: %w", errRows)
	}

	return EventsPage{Events: events, Limit: filter.Limit, Offset: filter.Offset, Total: total}, nil
}

func (s *SQLiteStore) QuerySummary(filter SummaryFilter) ([]SummaryRow, error) {
	return s.QuerySummaryContext(context.Background(), filter)
}

func (s *SQLiteStore) QuerySummaryContext(ctx context.Context, filter SummaryFilter) ([]SummaryRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter.QueryFilter = normalizeQueryFilter(filter.QueryFilter)
	groupBy := strings.TrimSpace(filter.GroupBy)
	if groupBy == "" {
		groupBy = "provider"
	}

	selectPrefix, groupExpr, orderExpr, errGroup := summaryGroupSQL(groupBy)
	if errGroup != nil {
		return nil, errGroup
	}
	where, args := buildWhere(filter.QueryFilter, false)
	query := selectPrefix + summaryAggregateSQL() + " FROM usage_events" + where + groupExpr + orderExpr
	rows, errQuery := s.db.QueryContext(ctx, query, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query usage summary: %w", errQuery)
	}
	defer rows.Close()

	var out []SummaryRow
	for rows.Next() {
		row, errScan := scanSummaryRow(rows, groupBy)
		if errScan != nil {
			return nil, errScan
		}
		out = append(out, row)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate usage summary: %w", errRows)
	}
	return out, nil
}

func (s *SQLiteStore) QueryFailures(filter QueryFilter) ([]FailureRow, error) {
	return s.QueryFailuresContext(context.Background(), filter)
}

func (s *SQLiteStore) QueryFailuresContext(ctx context.Context, filter QueryFilter) ([]FailureRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	filter.Status = StatusFailure
	where, args := buildWhere(filter, false)
	query := `
WITH filtered_usage AS (
	SELECT *, ` + providerStatsKeySQL() + ` AS provider_group_key, ` + providerStatsLabelSQL() + ` AS provider_group_label FROM usage_events` + where + `
)
SELECT
	COALESCE(error_stage, ''),
	COALESCE(error_code, ''),
	provider_group_key,
	provider_group_label,
	model,
	COUNT(*) AS requests,
	COALESCE((SELECT e2.error_message FROM filtered_usage e2
		WHERE e2.status = 'failure'
		  AND COALESCE(e2.error_stage, '') = COALESCE(filtered_usage.error_stage, '')
		  AND COALESCE(e2.error_code, '') = COALESCE(filtered_usage.error_code, '')
		  AND e2.provider_group_key = filtered_usage.provider_group_key
		  AND e2.model = filtered_usage.model
		ORDER BY e2.started_at DESC, e2.id DESC LIMIT 1), '') AS last_message,
	MAX(started_at) AS last_seen_at
FROM filtered_usage
GROUP BY COALESCE(error_stage, ''), COALESCE(error_code, ''), provider_group_key, provider_group_label, model
ORDER BY requests DESC, last_seen_at DESC`
	rows, errQuery := s.db.QueryContext(ctx, query, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query usage failures: %w", errQuery)
	}
	defer rows.Close()

	var out []FailureRow
	for rows.Next() {
		var row FailureRow
		var lastSeen int64
		if err := rows.Scan(&row.ErrorStage, &row.ErrorCode, &row.ProviderKey, &row.ProviderLabel, &row.Model, &row.Requests, &row.LastMessage, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan usage failure row: %w", err)
		}
		if lastSeen > 0 {
			row.LastSeenAt = time.UnixMilli(lastSeen).UTC().Format(time.RFC3339Nano)
		}
		out = append(out, row)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate usage failures: %w", errRows)
	}
	return out, nil
}

func (s *SQLiteStore) QueryFilters(filter QueryFilter) (FilterOptions, error) {
	return s.QueryFiltersContext(context.Background(), filter)
}

func (s *SQLiteStore) QueryFiltersContext(ctx context.Context, filter QueryFilter) (FilterOptions, error) {
	if s == nil || s.db == nil {
		return FilterOptions{}, fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	where, args := buildWhere(filter, false)
	options := FilterOptions{
		Statuses: []string{StatusSuccess, StatusFailure},
	}
	var err error
	options.Providers, err = s.distinctProviders(ctx, where, args)
	if err != nil {
		return FilterOptions{}, err
	}
	if options.ProviderLabels, err = s.distinctStrings(ctx, "provider_label", where, args); err != nil {
		return FilterOptions{}, err
	}
	if options.Models, err = s.distinctStrings(ctx, "model", where, args); err != nil {
		return FilterOptions{}, err
	}
	if options.ClientModels, err = s.distinctStrings(ctx, "client_model", where, args); err != nil {
		return FilterOptions{}, err
	}
	if options.AuthLabels, err = s.distinctStrings(ctx, "auth_label", where, args); err != nil {
		return FilterOptions{}, err
	}
	if options.ErrorStages, err = s.distinctStrings(ctx, "error_stage", where, args); err != nil {
		return FilterOptions{}, err
	}
	if options.ErrorCodes, err = s.distinctStrings(ctx, "error_code", where, args); err != nil {
		return FilterOptions{}, err
	}
	return options, nil
}

func (s *SQLiteStore) QueryMetrics(filter QueryFilter) (Metrics, error) {
	return s.QueryMetricsContext(context.Background(), filter)
}

func (s *SQLiteStore) QueryMetricsContext(ctx context.Context, filter QueryFilter) (Metrics, error) {
	if s == nil || s.db == nil {
		return Metrics{}, fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filter = normalizeQueryFilter(filter)
	now := time.Now().UTC()
	if filter.DateTo == nil {
		t := now
		filter.DateTo = &t
	}
	if filter.DateFrom == nil {
		t := filter.DateTo.Add(-defaultMetricsWindow)
		filter.DateFrom = &t
	}
	where, args := buildWhere(filter, false)

	var m Metrics
	m.WindowFrom = filter.DateFrom.UTC()
	m.WindowTo = filter.DateTo.UTC()
	minutes := m.WindowTo.Sub(m.WindowFrom).Minutes()
	if minutes <= 0 {
		minutes = 1
	}
	m.WindowMinutes = minutes

	row := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'failure' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(prompt_tokens), 0),
	COALESCE(SUM(completion_tokens), 0),
	COALESCE(SUM(reasoning_tokens), 0),
	COALESCE(SUM(cached_tokens), 0),
	COALESCE(SUM(total_tokens), 0)
FROM usage_events`+where, args...)
	if err := row.Scan(&m.TotalRequests, &m.SuccessfulRequests, &m.FailedRequests, &m.TotalPromptTokens, &m.TotalCompletionTokens, &m.TotalReasoningTokens, &m.TotalCachedTokens, &m.TotalTokens); err != nil {
		return Metrics{}, fmt.Errorf("query usage metrics totals: %w", err)
	}
	m.SuccessRate = successRate(m.SuccessfulRequests, m.TotalRequests)
	m.RPM = float64(m.TotalRequests) / minutes
	m.TPM = float64(m.TotalTokens) / minutes

	providers, errProviders := s.queryProviderMetrics(ctx, where, args)
	if errProviders != nil {
		return Metrics{}, errProviders
	}
	m.ProviderSuccessRates = providers
	m.ProviderRequestTotals = providers
	m.ProviderTokenTotals = providers
	models, errModels := s.queryModelMetrics(ctx, where, args)
	if errModels != nil {
		return Metrics{}, errModels
	}
	m.ModelRequestTotals = models
	m.ModelTokenTotals = models
	return m, nil
}

func normalizeQueryFilter(filter QueryFilter) QueryFilter {
	filter.Provider = strings.TrimSpace(filter.Provider)
	filter.ProviderLabel = strings.TrimSpace(filter.ProviderLabel)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.ClientModel = strings.TrimSpace(filter.ClientModel)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.ErrorStage = strings.TrimSpace(filter.ErrorStage)
	filter.ErrorCode = strings.TrimSpace(filter.ErrorCode)
	filter.AuthID = strings.TrimSpace(filter.AuthID)
	filter.AuthLabel = strings.TrimSpace(filter.AuthLabel)
	filter.ClientKeyHash = strings.TrimSpace(filter.ClientKeyHash)
	filter.Sort = strings.ToLower(strings.TrimSpace(filter.Sort))
	filter.Order = strings.ToLower(strings.TrimSpace(filter.Order))
	if filter.Limit <= 0 {
		filter.Limit = defaultEventsLimit
	}
	if filter.Limit > maxEventsLimit {
		filter.Limit = maxEventsLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func buildWhere(filter QueryFilter, includePagination bool) (string, []any) {
	var parts []string
	args := make([]any, 0, 12)
	add := func(expr string, values ...any) {
		parts = append(parts, expr)
		args = append(args, values...)
	}
	if filter.Provider != "" {
		add("("+providerStatsKeySQL()+" = ? OR provider_key = ?)", filter.Provider, filter.Provider)
	}
	if filter.ProviderLabel != "" {
		add("provider_label = ?", filter.ProviderLabel)
	}
	if filter.Model != "" {
		add("model = ?", filter.Model)
	}
	if filter.ClientModel != "" {
		add("client_model = ?", filter.ClientModel)
	}
	if filter.Status != "" {
		add("status = ?", filter.Status)
	}
	if filter.ErrorStage != "" {
		add("error_stage = ?", filter.ErrorStage)
	}
	if filter.ErrorCode != "" {
		add("error_code = ?", filter.ErrorCode)
	}
	if filter.AuthID != "" {
		add("auth_id = ?", filter.AuthID)
	}
	if filter.AuthLabel != "" {
		add("auth_label = ?", filter.AuthLabel)
	}
	if filter.ClientKeyHash != "" {
		add("client_key_hash = ?", filter.ClientKeyHash)
	}
	if filter.DateFrom != nil {
		add("started_at >= ?", filter.DateFrom.UTC().UnixMilli())
	}
	if filter.DateTo != nil {
		add("started_at < ?", filter.DateTo.UTC().UnixMilli())
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func eventSelectFields(includeRaw bool) string {
	raw := "'' AS provider_error_raw"
	if includeRaw {
		raw = "COALESCE(provider_error_raw, '')"
	}
	return `id, request_id, started_at, completed_at, duration_ms, provider_key, provider_label,
auth_id, auth_label, auth_index, model, client_model, route, status, http_status,
upstream_status, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens,
cached_tokens, client_key_hash, error_stage, error_code, error_message, ` + raw + `, metadata_json`
}

func scanEvent(rows *sql.Rows, includeRaw bool) (Event, error) {
	var event Event
	var startedMS, completedMS int64
	if err := rows.Scan(
		&event.ID,
		&event.RequestID,
		&startedMS,
		&completedMS,
		&event.DurationMS,
		&event.ProviderKey,
		&event.ProviderLabel,
		&event.AuthID,
		&event.AuthLabel,
		&event.AuthIndex,
		&event.Model,
		&event.ClientModel,
		&event.Route,
		&event.Status,
		&event.HTTPStatus,
		&event.UpstreamStatus,
		&event.PromptTokens,
		&event.CompletionTokens,
		&event.TotalTokens,
		&event.ReasoningTokens,
		&event.CachedTokens,
		&event.ClientKeyHash,
		&event.ErrorStage,
		&event.ErrorCode,
		&event.ErrorMessage,
		&event.ProviderErrorRaw,
		&event.MetadataJSON,
	); err != nil {
		return Event{}, fmt.Errorf("scan usage event: %w", err)
	}
	event.StartedAt = time.UnixMilli(startedMS).UTC()
	event.CompletedAt = time.UnixMilli(completedMS).UTC()
	if !includeRaw {
		event.ProviderErrorRaw = ""
	}
	return event, nil
}

func summaryGroupSQL(groupBy string) (selectPrefix, groupExpr, orderExpr string, err error) {
	switch groupBy {
	case "day":
		return "SELECT strftime('%Y-%m-%d', started_at / 1000, 'unixepoch') AS day, ", " GROUP BY day", " ORDER BY day ASC", nil
	case "provider":
		return "SELECT " + providerStatsKeySQL() + " AS provider_key, " + providerStatsLabelSQL() + " AS provider_label, ", " GROUP BY 1, 2", " ORDER BY requests DESC", nil
	case "model":
		return "SELECT model, ", " GROUP BY model", " ORDER BY requests DESC", nil
	case "provider_model":
		return "SELECT " + providerStatsKeySQL() + " AS provider_key, " + providerStatsLabelSQL() + " AS provider_label, model, ", " GROUP BY 1, 2, 3", " ORDER BY requests DESC", nil
	case "status":
		return "SELECT status, ", " GROUP BY status", " ORDER BY requests DESC", nil
	default:
		return "", "", "", fmt.Errorf("invalid group_by %q", groupBy)
	}
}

func summaryAggregateSQL() string {
	return `COUNT(*) AS requests,
SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS successful,
SUM(CASE WHEN status = 'failure' THEN 1 ELSE 0 END) AS failed,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
COALESCE(SUM(total_tokens), 0) AS total_tokens`
}

func scanSummaryRow(rows *sql.Rows, groupBy string) (SummaryRow, error) {
	var row SummaryRow
	dest := make([]any, 0, 11)
	switch groupBy {
	case "day":
		dest = append(dest, &row.Day)
	case "provider":
		dest = append(dest, &row.ProviderKey, &row.ProviderLabel)
	case "model":
		dest = append(dest, &row.Model)
	case "provider_model":
		dest = append(dest, &row.ProviderKey, &row.ProviderLabel, &row.Model)
	case "status":
		dest = append(dest, &row.Status)
	}
	dest = append(dest, &row.Requests, &row.Successful, &row.Failed, &row.PromptTokens, &row.CompletionTokens, &row.ReasoningTokens, &row.CachedTokens, &row.TotalTokens)
	if err := rows.Scan(dest...); err != nil {
		return SummaryRow{}, fmt.Errorf("scan usage summary row: %w", err)
	}
	row.SuccessRate = successRate(row.Successful, row.Requests)
	return row, nil
}

func (s *SQLiteStore) distinctStrings(ctx context.Context, column, where string, args []any) ([]string, error) {
	if !isAllowedDistinctColumn(column) {
		return nil, fmt.Errorf("invalid distinct column %q", column)
	}
	query := "SELECT DISTINCT " + column + " FROM usage_events"
	if where == "" {
		query += " WHERE "
	} else {
		query += where + " AND "
	}
	query += column + " IS NOT NULL AND " + column + " != '' ORDER BY " + column
	rows, errQuery := s.db.QueryContext(ctx, query, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query distinct usage %s: %w", column, errQuery)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan distinct usage %s: %w", column, err)
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func isAllowedDistinctColumn(column string) bool {
	switch column {
	case "provider_label", "model", "client_model", "auth_label", "error_stage", "error_code":
		return true
	default:
		return false
	}
}

func (s *SQLiteStore) distinctProviders(ctx context.Context, where string, args []any) ([]FilterOption, error) {
	query := "SELECT " + providerStatsKeySQL() + " AS provider_key, " +
		providerStatsLabelSQL() + " AS provider_label, " +
		"CASE WHEN COUNT(DISTINCT COALESCE(auth_id, '')) = 1 THEN COALESCE(MAX(auth_id), '') ELSE '' END AS auth_id FROM usage_events"
	if where == "" {
		query += " WHERE " + providerStatsKeySQL() + " != ''"
	} else {
		query += where + " AND " + providerStatsKeySQL() + " != ''"
	}
	query += " GROUP BY 1, 2 ORDER BY provider_label, provider_key"
	rows, errQuery := s.db.QueryContext(ctx, query, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query distinct usage providers: %w", errQuery)
	}
	defer rows.Close()
	var out []FilterOption
	for rows.Next() {
		var item FilterOption
		if err := rows.Scan(&item.Key, &item.Label, &item.AuthID); err != nil {
			return nil, fmt.Errorf("scan distinct usage provider: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) queryProviderMetrics(ctx context.Context, where string, args []any) ([]ProviderMetric, error) {
	rows, errQuery := s.db.QueryContext(ctx, `
SELECT `+providerStatsKeySQL()+` AS provider_key,
	`+providerStatsLabelSQL()+` AS provider_label,
	CASE WHEN COUNT(DISTINCT COALESCE(auth_id, '')) = 1 THEN COALESCE(MAX(auth_id), '') ELSE '' END AS auth_id,
	COUNT(*) AS requests,
	SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS successful,
	COALESCE(SUM(total_tokens), 0) AS tokens
FROM usage_events`+where+`
GROUP BY 1, 2
ORDER BY requests DESC`, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query usage provider metrics: %w", errQuery)
	}
	defer rows.Close()
	var out []ProviderMetric
	for rows.Next() {
		var item ProviderMetric
		if err := rows.Scan(&item.ProviderKey, &item.ProviderLabel, &item.AuthID, &item.Requests, &item.Successful, &item.Tokens); err != nil {
			return nil, fmt.Errorf("scan usage provider metrics: %w", err)
		}
		item.Failed = item.Requests - item.Successful
		item.SuccessRate = successRate(item.Successful, item.Requests)
		out = append(out, item)
	}
	return out, rows.Err()
}

func providerStatsKeySQL() string {
	return "CASE WHEN NULLIF(TRIM(provider_label), '') IS NOT NULL AND TRIM(provider_label) != TRIM(provider_key) THEN TRIM(provider_label) ELSE COALESCE(NULLIF(TRIM(auth_index), ''), TRIM(provider_key)) END"
}

func providerStatsLabelSQL() string {
	return "CASE WHEN NULLIF(TRIM(provider_label), '') IS NOT NULL THEN TRIM(provider_label) ELSE TRIM(provider_key) END"
}

func (s *SQLiteStore) queryModelMetrics(ctx context.Context, where string, args []any) ([]ModelMetric, error) {
	rows, errQuery := s.db.QueryContext(ctx, `
SELECT model, COUNT(*) AS requests, COALESCE(SUM(total_tokens), 0) AS tokens
FROM usage_events`+where+`
GROUP BY model
ORDER BY requests DESC`, args...)
	if errQuery != nil {
		return nil, fmt.Errorf("query usage model metrics: %w", errQuery)
	}
	defer rows.Close()
	var out []ModelMetric
	for rows.Next() {
		var item ModelMetric
		if err := rows.Scan(&item.Model, &item.Requests, &item.Tokens); err != nil {
			return nil, fmt.Errorf("scan usage model metrics: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func parseTimeQuery(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if unixMS, err := strconv.ParseInt(value, 10, 64); err == nil {
		t := time.UnixMilli(unixMS).UTC()
		return &t, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			t := parsed.UTC()
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid time %q", value)
}
