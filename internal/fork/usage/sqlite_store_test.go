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
		switch row.ProviderKey {
		case "gemini":
			if row.Requests != 1 || row.Successful != 1 || row.TotalTokens != 30 || row.SuccessRate != 1 {
				t.Fatalf("gemini summary = %+v", row)
			}
		case "claude":
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
	if len(options.Providers) != 1 || options.Providers[0].Key != "gemini" {
		t.Fatalf("providers = %+v", options.Providers)
	}
	requireContains(t, options.Models, "gemini-pro")
	requireContains(t, options.ClientModels, "client-gemini")
	requireContains(t, options.AuthLabels, "Gemini Key")
	requireContains(t, options.ErrorStages, "stream")
	requireContains(t, options.ErrorCodes, "read_failed")
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
