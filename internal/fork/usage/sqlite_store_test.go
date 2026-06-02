package usage

import (
	"context"
	"errors"
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
		Provider: "gemini",
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

	failures, err := store.QueryFailures(QueryFilter{Provider: "claude"})
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
	if len(metrics.ProviderSuccessRates) != 1 || metrics.ProviderSuccessRates[0].SuccessRate != 0.5 {
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
	if sharedMetric.ProviderLabel != "Shared Codex" || sharedMetric.Requests != 2 || sharedMetric.Successful != 1 || sharedMetric.Failed != 1 || sharedMetric.Tokens != 30 || sharedMetric.AuthID != "" {
		t.Fatalf("shared metric = %+v", sharedMetric)
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
	page, err = store.QueryEvents(QueryFilter{Provider: "codex#1"})
	if err != nil {
		t.Fatalf("QueryEvents(provider key) error = %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("legacy provider key filter total = %d, want 1", page.Total)
	}

	failures, err := store.QueryFailures(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryFailures() error = %v", err)
	}
	if len(failures) != 1 || failures[0].ProviderKey != "Shared Codex" || failures[0].Requests != 1 {
		t.Fatalf("failures = %+v", failures)
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
