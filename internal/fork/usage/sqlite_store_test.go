package usage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStoreInsertQueryAndAggregate(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	mustInsertEvent(t, store, Event{
		RequestID:        "req-1",
		StartedAt:        base,
		CompletedAt:      base.Add(150 * time.Millisecond),
		DurationMS:       150,
		ProviderKey:      "gemini",
		ProviderLabel:    "Gemini Primary",
		AuthID:           "auth-1",
		AuthLabel:        "Gemini Key",
		AuthIndex:        "idx-1",
		Model:            "gemini-2.5-pro",
		ClientModel:      "team/gemini",
		Route:            "POST /v1/chat/completions",
		Status:           StatusSuccess,
		HTTPStatus:       200,
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	})
	mustInsertEvent(t, store, Event{
		RequestID:        "req-2",
		StartedAt:        base.Add(time.Minute),
		CompletedAt:      base.Add(time.Minute + 200*time.Millisecond),
		DurationMS:       200,
		ProviderKey:      "claude",
		ProviderLabel:    "Claude Backup",
		Model:            "claude-sonnet",
		ClientModel:      "claude-sonnet",
		Route:            "POST /v1/messages",
		Status:           StatusFailure,
		HTTPStatus:       429,
		UpstreamStatus:   429,
		ErrorStage:       "upstream_response",
		ErrorCode:        "rate_limit",
		ErrorMessage:     "rate limited",
		ProviderErrorRaw: `{"error":"rate limited"}`,
		PromptTokens:     3,
		CompletionTokens: 0,
		TotalTokens:      3,
	})

	page, err := store.QueryEvents(QueryFilter{
		Provider: "Gemini Primary",
		DateFrom: ptrTime(base.Add(-time.Second)),
		DateTo:   ptrTime(base.Add(time.Hour)),
	})
	if err != nil {
		t.Fatalf("QueryEvents() error = %v", err)
	}
	if page.Total != 1 || len(page.Events) != 1 {
		t.Fatalf("QueryEvents() total=%d len=%d, want 1", page.Total, len(page.Events))
	}
	if page.Events[0].ProviderLabel != "Gemini Primary" || page.Events[0].ProviderErrorRaw != "" {
		t.Fatalf("event = %+v", page.Events[0])
	}

	withRaw, err := store.QueryEvents(QueryFilter{Status: StatusFailure, IncludeErrorRaw: true})
	if err != nil {
		t.Fatalf("QueryEvents(include raw) error = %v", err)
	}
	if len(withRaw.Events) != 1 || withRaw.Events[0].ProviderErrorRaw == "" {
		t.Fatalf("expected raw provider error in explicit detail query, got %+v", withRaw.Events)
	}

	summary, err := store.QuerySummary(SummaryFilter{
		QueryFilter: QueryFilter{DateFrom: ptrTime(base.Add(-time.Second)), DateTo: ptrTime(base.Add(time.Hour))},
		GroupBy:     "provider",
	})
	if err != nil {
		t.Fatalf("QuerySummary() error = %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("summary rows = %d, want 2", len(summary))
	}
	for _, row := range summary {
		switch row.ProviderLabel {
		case "Gemini Primary":
			if row.Requests != 1 || row.Successful != 1 || row.TotalTokens != 30 || row.SuccessRate != 1 {
				t.Fatalf("gemini summary = %+v", row)
			}
		case "Claude Backup":
			if row.Requests != 1 || row.Failed != 1 || row.TotalTokens != 3 || row.SuccessRate != 0 {
				t.Fatalf("claude summary = %+v", row)
			}
		default:
			t.Fatalf("unexpected summary provider %q", row.ProviderKey)
		}
	}

	failures, err := store.QueryFailures(QueryFilter{Provider: "Claude Backup"})
	if err != nil {
		t.Fatalf("QueryFailures() error = %v", err)
	}
	if len(failures) != 1 || failures[0].ErrorCode != "rate_limit" || failures[0].Requests != 1 {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestSQLiteStoreMetricsAndFilters(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	mustInsertEvent(t, store, Event{
		StartedAt:        base,
		CompletedAt:      base,
		ProviderKey:      "gemini",
		ProviderLabel:    "Gemini",
		Model:            "gemini-pro",
		ClientModel:      "client-gemini",
		AuthLabel:        "Gemini Key",
		Status:           StatusSuccess,
		HTTPStatus:       200,
		PromptTokens:     10,
		CompletionTokens: 15,
		TotalTokens:      25,
		CachedTokens:     4,
	})
	mustInsertEvent(t, store, Event{
		StartedAt:        base.Add(30 * time.Second),
		CompletedAt:      base.Add(31 * time.Second),
		ProviderKey:      "gemini",
		ProviderLabel:    "Gemini",
		Model:            "gemini-pro",
		ClientModel:      "client-gemini",
		AuthLabel:        "Gemini Key",
		Status:           StatusFailure,
		HTTPStatus:       500,
		ErrorStage:       "stream",
		ErrorCode:        "read_failed",
		PromptTokens:     2,
		CompletionTokens: 0,
		TotalTokens:      2,
		CachedTokens:     2,
	})

	metrics, err := store.QueryMetrics(QueryFilter{
		DateFrom: ptrTime(base),
		DateTo:   ptrTime(base.Add(time.Minute)),
	})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if metrics.TotalRequests != 2 || metrics.SuccessfulRequests != 1 || metrics.FailedRequests != 1 {
		t.Fatalf("metrics counts = %+v", metrics)
	}
	if metrics.TotalTokens != 27 || metrics.RPM != 2 || metrics.TPM != 27 || metrics.SuccessRate != 0.5 {
		t.Fatalf("metrics rates/tokens = %+v", metrics)
	}
	if metrics.TotalPromptTokens != 12 || metrics.TotalCachedTokens != 6 || metrics.CacheHitRate != 0.5 {
		t.Fatalf("metrics cache hit rate = %+v", metrics)
	}
	if len(metrics.ProviderSuccessRates) != 1 || metrics.ProviderSuccessRates[0].SuccessRate != 0.5 ||
		metrics.ProviderSuccessRates[0].PromptTokens != 12 || metrics.ProviderSuccessRates[0].CachedTokens != 6 ||
		metrics.ProviderSuccessRates[0].CacheHitRate != 0.5 {
		t.Fatalf("provider metrics = %+v", metrics.ProviderSuccessRates)
	}

	options, err := store.QueryFilters(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryFilters() error = %v", err)
	}
	if len(options.Providers) != 1 || options.Providers[0].Key != "Gemini" {
		t.Fatalf("providers = %+v", options.Providers)
	}
	requireContains(t, options.Models, "gemini-pro")
	requireContains(t, options.ClientModels, "client-gemini")
	requireContains(t, options.AuthLabels, "Gemini Key")
	requireContains(t, options.ErrorStages, "stream")
	requireContains(t, options.ErrorCodes, "read_failed")
}

func TestSQLiteStoreCacheHitRateUsesReadTokensOnly(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	mustInsertEvent(t, store, Event{
		StartedAt:           base,
		CompletedAt:         base,
		ProviderKey:         "claude",
		ProviderLabel:       "Claude",
		Model:               "claude-sonnet",
		Status:              StatusSuccess,
		PromptTokens:        100,
		CompletionTokens:    20,
		TotalTokens:         120,
		CachedTokens:        30,
		CacheCreationTokens: 30,
	})
	mustInsertEvent(t, store, Event{
		StartedAt:        base.Add(time.Minute),
		CompletedAt:      base.Add(time.Minute),
		ProviderKey:      "claude",
		ProviderLabel:    "Claude",
		Model:            "claude-sonnet",
		Status:           StatusSuccess,
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		CachedTokens:     10,
		CacheReadTokens:  10,
	})

	metrics, err := store.QueryMetrics(QueryFilter{DateFrom: ptrTime(base), DateTo: ptrTime(base.Add(time.Hour))})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if metrics.TotalCachedTokens != 40 || metrics.TotalCacheReadTokens != 10 || metrics.TotalCacheCreationTokens != 30 {
		t.Fatalf("cache token totals = %+v", metrics)
	}
	if metrics.CacheHitRate != float64(10)/float64(150) {
		t.Fatalf("cache hit rate = %v, want %v", metrics.CacheHitRate, float64(10)/float64(150))
	}
	provider := requireProviderMetric(t, metrics.ProviderRequestTotals, "Claude")
	if provider.CachedTokens != 40 || provider.CacheReadTokens != 10 || provider.CacheCreationTokens != 30 || provider.CacheHitRate != float64(10)/float64(150) {
		t.Fatalf("provider cache metrics = %+v", provider)
	}
}

func TestSQLiteStoreNewColumnsRoundTripAndFilter(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	mustInsertEvent(t, store, Event{
		StartedAt:        base,
		CompletedAt:      base,
		ProviderKey:      "claude",
		ProviderLabel:    "Claude OAuth",
		Model:            "claude-sonnet",
		ClientModel:      "client-claude",
		ResponseModel:    "claude-sonnet-4-5",
		Route:            "POST /v1/messages",
		Stream:           true,
		Status:           StatusSuccess,
		AuthType:         "oauth",
		AuthCategory:     "claude/oauth",
		ReasoningEffort:  "medium",
		TTFTMS:           320,
		PromptTokens:     5,
		CompletionTokens: 7,
		TotalTokens:      12,
	})
	mustInsertEvent(t, store, Event{
		StartedAt:        base.Add(time.Minute),
		CompletedAt:      base.Add(time.Minute),
		ProviderKey:      "openai-compat",
		ProviderLabel:    "Compat Key",
		Model:            "gpt-5.4",
		ClientModel:      "gpt-5.4",
		ResponseModel:    "",
		Route:            "POST /v1/chat/completions",
		Stream:           false,
		Status:           StatusSuccess,
		AuthType:         "apikey",
		AuthCategory:     "openai-compat/apikey",
		ReasoningEffort:  "low",
		TTFTMS:           0,
		PromptTokens:     1,
		CompletionTokens: 2,
		TotalTokens:      3,
	})

	streaming, err := store.QueryEvents(QueryFilter{Stream: "streaming"})
	if err != nil {
		t.Fatalf("QueryEvents(stream) error = %v", err)
	}
	if streaming.Total != 1 || streaming.Events[0].ResponseModel != "claude-sonnet-4-5" || !streaming.Events[0].Stream {
		t.Fatalf("streaming event = %+v", streaming.Events)
	}

	sync, err := store.QueryEvents(QueryFilter{Stream: "sync"})
	if err != nil {
		t.Fatalf("QueryEvents(sync) error = %v", err)
	}
	if sync.Total != 1 || sync.Events[0].Stream {
		t.Fatalf("sync event = %+v", sync.Events)
	}

	byAuthType, err := store.QueryEvents(QueryFilter{AuthType: "oauth"})
	if err != nil {
		t.Fatalf("QueryEvents(auth_type) error = %v", err)
	}
	if byAuthType.Total != 1 || byAuthType.Events[0].AuthCategory != "claude/oauth" {
		t.Fatalf("auth_type event = %+v", byAuthType)
	}

	byCategory, err := store.QueryEvents(QueryFilter{AuthCategory: "openai-compat/apikey"})
	if err != nil {
		t.Fatalf("QueryEvents(auth_category) error = %v", err)
	}
	if byCategory.Total != 1 {
		t.Fatalf("auth_category total = %d, want 1", byCategory.Total)
	}

	byResponseModel, err := store.QueryEvents(QueryFilter{ResponseModel: "claude-sonnet-4-5"})
	if err != nil {
		t.Fatalf("QueryEvents(response_model) error = %v", err)
	}
	if byResponseModel.Total != 1 {
		t.Fatalf("response_model total = %d, want 1", byResponseModel.Total)
	}

	byReasoning, err := store.QueryEvents(QueryFilter{ReasoningEffort: "medium"})
	if err != nil {
		t.Fatalf("QueryEvents(reasoning_effort) error = %v", err)
	}
	if byReasoning.Total != 1 || byReasoning.Events[0].TTFTMS != 320 {
		t.Fatalf("reasoning event = %+v", byReasoning.Events[0])
	}

	options, err := store.QueryFilters(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryFilters() error = %v", err)
	}
	requireContains(t, options.AuthTypes, "oauth")
	requireContains(t, options.AuthTypes, "apikey")
	requireContains(t, options.AuthCategories, "claude/oauth")
	requireContains(t, options.AuthCategories, "openai-compat/apikey")
	requireContains(t, options.ResponseModels, "claude-sonnet-4-5")
	requireContains(t, options.ReasoningEfforts, "medium")
	requireContains(t, options.ReasoningEfforts, "low")
}

func TestSQLiteStoreAggregatesProvidersByLabelThenIndex(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)
	events := []Event{
		{
			StartedAt:        base,
			CompletedAt:      base,
			ProviderKey:      "codex#1",
			ProviderLabel:    "Shared Codex",
			AuthID:           "auth-1",
			AuthIndex:        "idx-1",
			Model:            "gpt-5",
			Status:           StatusSuccess,
			PromptTokens:     1,
			CompletionTokens: 9,
			TotalTokens:      10,
			CachedTokens:     1,
		},
		{
			StartedAt:        base.Add(time.Second),
			CompletedAt:      base.Add(time.Second),
			ProviderKey:      "codex#2",
			ProviderLabel:    "Shared Codex",
			AuthID:           "auth-2",
			AuthIndex:        "idx-2",
			Model:            "gpt-5",
			Status:           StatusFailure,
			ErrorStage:       "stream",
			ErrorCode:        "keyword_filtered",
			ErrorMessage:     "matched keyword",
			PromptTokens:     2,
			CompletionTokens: 18,
			TotalTokens:      20,
			CachedTokens:     1,
		},
		{
			StartedAt:        base.Add(2 * time.Second),
			CompletedAt:      base.Add(2 * time.Second),
			ProviderKey:      "codex",
			ProviderLabel:    "codex",
			AuthID:           "auth-3",
			AuthIndex:        "idx-3",
			Model:            "gpt-5",
			Status:           StatusSuccess,
			PromptTokens:     3,
			CompletionTokens: 0,
			TotalTokens:      3,
			CachedTokens:     0,
		},
		{
			StartedAt:        base.Add(3 * time.Second),
			CompletedAt:      base.Add(3 * time.Second),
			ProviderKey:      "codex",
			ProviderLabel:    "codex",
			AuthID:           "auth-4",
			AuthIndex:        "idx-4",
			Model:            "gpt-5",
			Status:           StatusSuccess,
			PromptTokens:     4,
			CompletionTokens: 0,
			TotalTokens:      4,
			CachedTokens:     2,
		},
	}
	for _, event := range events {
		mustInsertEvent(t, store, event)
	}

	summary, err := store.QuerySummary(SummaryFilter{GroupBy: "provider"})
	if err != nil {
		t.Fatalf("QuerySummary() error = %v", err)
	}
	shared := requireSummaryProvider(t, summary, "Shared Codex")
	if shared.ProviderLabel != "Shared Codex" || shared.Requests != 2 || shared.Successful != 1 || shared.Failed != 1 || shared.TotalTokens != 30 {
		t.Fatalf("shared summary = %+v", shared)
	}
	idx3 := requireSummaryProvider(t, summary, "idx-3")
	if idx3.ProviderLabel != "codex" || idx3.Requests != 1 || idx3.TotalTokens != 3 {
		t.Fatalf("idx-3 summary = %+v", idx3)
	}
	idx4 := requireSummaryProvider(t, summary, "idx-4")
	if idx4.ProviderLabel != "codex" || idx4.Requests != 1 || idx4.TotalTokens != 4 {
		t.Fatalf("idx-4 summary = %+v", idx4)
	}

	metrics, err := store.QueryMetrics(QueryFilter{DateFrom: ptrTime(base.Add(-time.Second)), DateTo: ptrTime(base.Add(time.Minute))})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	sharedMetric := requireProviderMetric(t, metrics.ProviderRequestTotals, "Shared Codex")
	if sharedMetric.ProviderLabel != "Shared Codex" || sharedMetric.Requests != 2 || sharedMetric.Successful != 1 || sharedMetric.Failed != 1 || sharedMetric.Tokens != 30 || sharedMetric.PromptTokens != 3 || sharedMetric.CachedTokens != 2 || sharedMetric.CacheHitRate != float64(2)/float64(3) || sharedMetric.AuthID != "" {
		t.Fatalf("shared metric = %+v", sharedMetric)
	}
	rollupMetrics, err := store.QueryMetrics(QueryFilter{DateFrom: ptrTime(base), DateTo: ptrTime(base.Add(time.Hour))})
	if err != nil {
		t.Fatalf("QueryMetrics(rollup range) error = %v", err)
	}
	if rollupMetrics.TotalRequests != 4 || rollupMetrics.SuccessfulRequests != 3 || rollupMetrics.FailedRequests != 1 || rollupMetrics.TotalTokens != 37 || rollupMetrics.TotalPromptTokens != 10 || rollupMetrics.TotalCachedTokens != 4 || rollupMetrics.CacheHitRate != 0.4 {
		t.Fatalf("rollup metrics = %+v", rollupMetrics)
	}
	if metric := requireProviderMetric(t, rollupMetrics.ProviderRequestTotals, "Shared Codex"); metric.Requests != 2 || metric.Tokens != 30 || metric.PromptTokens != 3 || metric.CachedTokens != 2 || metric.CacheHitRate != float64(2)/float64(3) {
		t.Fatalf("shared rollup metric = %+v", metric)
	}
	var rollupRows int
	if err := store.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM usage_rollup_hourly").Scan(&rollupRows); err != nil {
		t.Fatalf("count usage_rollup_hourly error = %v", err)
	}
	if rollupRows != 4 {
		t.Fatalf("rollup rows = %d, want 4", rollupRows)
	}

	options, err := store.QueryFilters(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryFilters() error = %v", err)
	}
	requireFilterOption(t, options.Providers, "Shared Codex", "Shared Codex", "")
	requireFilterOption(t, options.Providers, "idx-3", "codex", "auth-3")
	requireFilterOption(t, options.Providers, "idx-4", "codex", "auth-4")

	page, err := store.QueryEvents(QueryFilter{Provider: "Shared Codex"})
	if err != nil {
		t.Fatalf("QueryEvents(provider label) error = %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("provider label filter total = %d, want 2", page.Total)
	}
	page, err = store.QueryEvents(QueryFilter{RawProvider: "codex#1"})
	if err != nil {
		t.Fatalf("QueryEvents(raw provider key) error = %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("legacy provider key filter total = %d, want 1", page.Total)
	}

	failures, err := store.QueryFailures(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryFailures() error = %v", err)
	}
	if len(failures) != 1 || failures[0].ProviderKey != "codex#2" || failures[0].Requests != 1 {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestSQLiteStoreBackfillsProviderStatsAndRollup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old sqlite error = %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE usage_events (
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
INSERT INTO usage_events (
	started_at, completed_at, duration_ms, provider_key, provider_label, auth_id,
	auth_index, model, client_model, status, prompt_tokens, completion_tokens, total_tokens
) VALUES
	(1780300800000, 1780300800000, 0, 'codex#1', 'Shared Codex', 'auth-1', 'idx-1', 'gpt-5', '', 'success', 1, 9, 10),
	(1780300860000, 1780300860000, 0, 'codex#2', 'Shared Codex', 'auth-2', 'idx-2', 'gpt-5', '', 'failure', 2, 18, 20);
`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed old sqlite error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old sqlite error = %v", err)
	}

	store, err := OpenSQLiteStore(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenSQLiteStore(existing) error = %v", err)
	}
	defer closeTestStore(t, store)

	var statsProviderKey string
	if err := store.db.QueryRowContext(context.Background(), "SELECT stats_provider_key FROM usage_events ORDER BY id LIMIT 1").Scan(&statsProviderKey); err != nil {
		t.Fatalf("query backfilled stats_provider_key error = %v", err)
	}
	if statsProviderKey != "Shared Codex" {
		t.Fatalf("statsProviderKey = %q, want Shared Codex", statsProviderKey)
	}
	var rollupRequests, rollupTokens int64
	if err := store.db.QueryRowContext(context.Background(), "SELECT COALESCE(SUM(requests), 0), COALESCE(SUM(total_tokens), 0) FROM usage_rollup_hourly").Scan(&rollupRequests, &rollupTokens); err != nil {
		t.Fatalf("query rollup totals error = %v", err)
	}
	if rollupRequests != 2 || rollupTokens != 30 {
		t.Fatalf("rollup requests=%d tokens=%d, want 2/30", rollupRequests, rollupTokens)
	}
	var rollupVersion string
	if err := store.db.QueryRowContext(context.Background(), "SELECT value FROM usage_meta WHERE key = 'rollup_version'").Scan(&rollupVersion); err != nil {
		t.Fatalf("query rollup version error = %v", err)
	}
	if rollupVersion != usageRollupVersion {
		t.Fatalf("rollupVersion = %q, want %q", rollupVersion, usageRollupVersion)
	}
	summary, err := store.QuerySummary(SummaryFilter{GroupBy: "provider"})
	if err != nil {
		t.Fatalf("QuerySummary(backfilled) error = %v", err)
	}
	shared := requireSummaryProvider(t, summary, "Shared Codex")
	if shared.Requests != 2 || shared.TotalTokens != 30 {
		t.Fatalf("backfilled summary = %+v", shared)
	}
}

func TestSQLiteStoreMixedRollupRangeMatchesExpectedTotals(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)
	for _, event := range []Event{
		{
			StartedAt:        base.Add(10 * time.Minute),
			CompletedAt:      base.Add(10 * time.Minute),
			ProviderKey:      "codex#1",
			ProviderLabel:    "Shared Codex",
			AuthID:           "auth-1",
			AuthIndex:        "idx-1",
			Model:            "gpt-5",
			Status:           StatusSuccess,
			PromptTokens:     1,
			CompletionTokens: 9,
			TotalTokens:      10,
			CachedTokens:     1,
		},
		{
			StartedAt:        base.Add(time.Hour + 10*time.Minute),
			CompletedAt:      base.Add(time.Hour + 10*time.Minute),
			ProviderKey:      "codex#2",
			ProviderLabel:    "Shared Codex",
			AuthID:           "auth-2",
			AuthIndex:        "idx-2",
			Model:            "gpt-5",
			Status:           StatusFailure,
			PromptTokens:     2,
			CompletionTokens: 18,
			TotalTokens:      20,
			CachedTokens:     1,
		},
		{
			StartedAt:        base.Add(2*time.Hour + 10*time.Minute),
			CompletedAt:      base.Add(2*time.Hour + 10*time.Minute),
			ProviderKey:      "codex#3",
			ProviderLabel:    "Shared Codex",
			AuthID:           "auth-3",
			AuthIndex:        "idx-3",
			Model:            "gpt-5",
			Status:           StatusSuccess,
			PromptTokens:     3,
			CompletionTokens: 27,
			TotalTokens:      30,
			CachedTokens:     3,
		},
	} {
		mustInsertEvent(t, store, event)
	}

	from := base.Add(5 * time.Minute)
	to := base.Add(2*time.Hour + 20*time.Minute)
	metrics, err := store.QueryMetrics(QueryFilter{DateFrom: &from, DateTo: &to})
	if err != nil {
		t.Fatalf("QueryMetrics(mixed) error = %v", err)
	}
	if metrics.TotalRequests != 3 || metrics.SuccessfulRequests != 2 || metrics.FailedRequests != 1 || metrics.TotalTokens != 60 || metrics.TotalPromptTokens != 6 || metrics.TotalCachedTokens != 5 || metrics.CacheHitRate != float64(5)/float64(6) {
		t.Fatalf("mixed metrics = %+v", metrics)
	}
	sharedMetric := requireProviderMetric(t, metrics.ProviderRequestTotals, "Shared Codex")
	if sharedMetric.Requests != 3 || sharedMetric.Successful != 2 || sharedMetric.Failed != 1 || sharedMetric.Tokens != 60 || sharedMetric.PromptTokens != 6 || sharedMetric.CachedTokens != 5 || sharedMetric.CacheHitRate != float64(5)/float64(6) {
		t.Fatalf("mixed provider metric = %+v", sharedMetric)
	}
	summary, err := store.QuerySummary(SummaryFilter{QueryFilter: QueryFilter{DateFrom: &from, DateTo: &to}, GroupBy: "provider"})
	if err != nil {
		t.Fatalf("QuerySummary(mixed) error = %v", err)
	}
	sharedSummary := requireSummaryProvider(t, summary, "Shared Codex")
	if sharedSummary.Requests != 3 || sharedSummary.Successful != 2 || sharedSummary.Failed != 1 || sharedSummary.TotalTokens != 60 {
		t.Fatalf("mixed summary = %+v", sharedSummary)
	}
}

func TestSQLiteStoreProviderFilterUsesStatsNamespaceOnly(t *testing.T) {
	store := openTestStore(t)
	defer closeTestStore(t, store)

	base := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)
	mustInsertEvent(t, store, Event{
		StartedAt:        base.Add(10 * time.Minute),
		CompletedAt:      base.Add(10 * time.Minute),
		ProviderKey:      "gemini",
		ProviderLabel:    "gemini",
		AuthID:           "auth-raw",
		AuthIndex:        "idx-raw",
		Model:            "gemini-pro",
		Status:           StatusSuccess,
		PromptTokens:     1,
		CompletionTokens: 9,
		TotalTokens:      10,
	})
	mustInsertEvent(t, store, Event{
		StartedAt:        base.Add(time.Hour + 10*time.Minute),
		CompletedAt:      base.Add(time.Hour + 10*time.Minute),
		ProviderKey:      "other",
		ProviderLabel:    "gemini",
		AuthID:           "auth-labeled",
		AuthIndex:        "idx-labeled",
		Model:            "gemini-pro",
		Status:           StatusSuccess,
		PromptTokens:     2,
		CompletionTokens: 18,
		TotalTokens:      20,
	})

	page, err := store.QueryEvents(QueryFilter{Provider: "gemini"})
	if err != nil {
		t.Fatalf("QueryEvents(provider stats key) error = %v", err)
	}
	if page.Total != 1 || page.Events[0].ProviderKey != "other" {
		t.Fatalf("stats provider filter page = %+v", page)
	}
	rawPage, err := store.QueryEvents(QueryFilter{RawProvider: "gemini"})
	if err != nil {
		t.Fatalf("QueryEvents(raw provider) error = %v", err)
	}
	if rawPage.Total != 1 || rawPage.Events[0].ProviderKey != "gemini" {
		t.Fatalf("raw provider filter page = %+v", rawPage)
	}
	from := base.Add(5 * time.Minute)
	to := base.Add(time.Hour + 20*time.Minute)
	metrics, err := store.QueryMetrics(QueryFilter{Provider: "gemini", DateFrom: &from, DateTo: &to})
	if err != nil {
		t.Fatalf("QueryMetrics(provider stats key) error = %v", err)
	}
	if metrics.TotalRequests != 1 || metrics.TotalTokens != 20 {
		t.Fatalf("stats provider metrics = %+v", metrics)
	}
}

func TestSanitizeProviderErrorTruncatesUTF8(t *testing.T) {
	message, raw := sanitizeProviderError("hello\n世界\tsecret", 10)
	if !strings.HasPrefix(raw, "hello") {
		t.Fatalf("raw = %q", raw)
	}
	if !strings.Contains(message, "hello") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(raw, "\n") && !strings.Contains(raw, "\t") {
		return
	}
	t.Fatalf("raw should be single-line sanitized, got %q", raw)
}

func TestNoopRecorderReportsDisabled(t *testing.T) {
	_, err := NoopRecorder{}.QueryEvents(QueryFilter{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("QueryEvents() error = %v, want ErrDisabled", err)
	}
}

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLiteStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	return store
}

func closeTestStore(t *testing.T, store *SQLiteStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func mustInsertEvent(t *testing.T, store *SQLiteStore, event Event) {
	t.Helper()
	if err := store.InsertEvent(context.Background(), event); err != nil {
		t.Fatalf("InsertEvent() error = %v", err)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func requireContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q not found in %v", want, values)
}

func requireSummaryProvider(t *testing.T, rows []SummaryRow, key string) SummaryRow {
	t.Helper()
	for _, row := range rows {
		if row.ProviderKey == key {
			return row
		}
	}
	t.Fatalf("provider summary %q not found in %+v", key, rows)
	return SummaryRow{}
}

func requireProviderMetric(t *testing.T, rows []ProviderMetric, key string) ProviderMetric {
	t.Helper()
	for _, row := range rows {
		if row.ProviderKey == key {
			return row
		}
	}
	t.Fatalf("provider metric %q not found in %+v", key, rows)
	return ProviderMetric{}
}

func requireFilterOption(t *testing.T, rows []FilterOption, key, label, authID string) {
	t.Helper()
	for _, row := range rows {
		if row.Key == key {
			if row.Label != label || row.AuthID != authID {
				t.Fatalf("filter option %q = %+v, want label %q auth %q", key, row, label, authID)
			}
			return
		}
	}
	t.Fatalf("filter option %q not found in %+v", key, rows)
}
