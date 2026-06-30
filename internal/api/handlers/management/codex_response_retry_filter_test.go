package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/fork/codexretryfilter"
)

func TestGetCodexResponseRetryFilterReturnsNormalizedConfig(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	c, rec := managementTestContext(http.MethodGet, "/v0/management/codex-response-retry-filter", "")

	h.GetCodexResponseRetryFilter(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Config codexretryfilter.RuntimeConfig `json:"codex-response-retry-filter"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Config.Enabled || body.Config.GuardRetryAttempts != 3 || !body.Config.InterceptStreaming || !body.Config.InterceptNonStreaming {
		t.Fatalf("config = %#v, want normalized defaults", body.Config)
	}
}

func TestPatchCodexResponseRetryFilterRejectsNoInterceptModes(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	c, rec := managementTestContext(http.MethodPatch, "/v0/management/codex-response-retry-filter", `{
		"enabled": true,
		"intercept-streaming": false,
		"intercept-non-streaming": false
	}`)

	h.PatchCodexResponseRetryFilter(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchCodexResponseRetryFilterRejectsExplicitEmptyModels(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	c, rec := managementTestContext(http.MethodPatch, "/v0/management/codex-response-retry-filter", `{
		"models": []
	}`)

	h.PatchCodexResponseRetryFilter(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchCodexResponseRetryFilterRejectsInvalidReasoningLengths(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	c, rec := managementTestContext(http.MethodPatch, "/v0/management/codex-response-retry-filter", `{
		"reasoning-token-lengths": [516, -1]
	}`)

	h.PatchCodexResponseRetryFilter(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCodexResponseRetryFilterStatsAndHits(t *testing.T) {
	ctx := context.Background()
	store, err := codexretryfilter.OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	length := int64(516)
	record := codexretryfilter.AttemptRecord{
		RequestID:       "req-1",
		ProviderKey:     "codex",
		AuthID:          "auth-1",
		Model:           "gpt-5-codex",
		Eligible:        true,
		Matched:         true,
		ReasoningTokens: &length,
		MatchedLength:   &length,
		Action:          codexretryfilter.ActionInternalRetry,
	}
	if err := store.InsertAttempt(ctx, record); err != nil {
		t.Fatalf("InsertAttempt() error = %v", err)
	}
	if err := store.InsertHit(ctx, record); err != nil {
		t.Fatalf("InsertHit() error = %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.SetCodexRetryFilterQueryService(store)

	cStats, recStats := managementTestContext(http.MethodGet, "/v0/management/codex-response-retry-filter/stats?model=gpt-5-codex&matched_length=516", "")
	h.GetCodexResponseRetryFilterStats(cStats)
	if recStats.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want 200: %s", recStats.Code, recStats.Body.String())
	}
	var stats codexretryfilter.Stats
	if err := json.Unmarshal(recStats.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Attempts != 1 || stats.Hits != 1 || stats.InternalRetries != 1 {
		t.Fatalf("stats = %#v, want 1/1/internal", stats)
	}

	cHits, recHits := managementTestContext(http.MethodGet, "/v0/management/codex-response-retry-filter/hits?matched_length=516", "")
	h.GetCodexResponseRetryFilterHits(cHits)
	if recHits.Code != http.StatusOK {
		t.Fatalf("hits status = %d, want 200: %s", recHits.Code, recHits.Body.String())
	}
	var hitsBody struct {
		Hits               []codexretryfilter.HitRecord `json:"hits"`
		NextBeforeOccurred *string                      `json:"next_before_occurred_at"`
		NextBeforeID       *int64                       `json:"next_before_id"`
		HasMore            bool                         `json:"has_more"`
	}
	if err := json.Unmarshal(recHits.Body.Bytes(), &hitsBody); err != nil {
		t.Fatalf("decode hits: %v", err)
	}
	if len(hitsBody.Hits) != 1 || hitsBody.Hits[0].MatchedLength != 516 {
		t.Fatalf("hits = %#v, want one 516 hit", hitsBody.Hits)
	}
	if hitsBody.HasMore {
		t.Fatalf("has_more = true, want false")
	}
}

func TestCodexResponseRetryFilterStatsIgnoreCursorQuery(t *testing.T) {
	ctx := context.Background()
	store, err := codexretryfilter.OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	length := int64(516)
	for i := 0; i < 3; i++ {
		record := codexretryfilter.AttemptRecord{
			RequestID:       "req-cursor-stats-" + strconv.Itoa(i),
			OccurredAt:      time.Unix(int64(100+i), 0).UTC(),
			ProviderKey:     "codex",
			AuthID:          "auth-1",
			Model:           "gpt-5-codex",
			Eligible:        true,
			Matched:         true,
			ReasoningTokens: &length,
			MatchedLength:   &length,
			Action:          codexretryfilter.ActionInternalRetry,
		}
		if err := store.InsertAttempt(ctx, record); err != nil {
			t.Fatalf("InsertAttempt(%d) error = %v", i, err)
		}
		if err := store.InsertHit(ctx, record); err != nil {
			t.Fatalf("InsertHit(%d) error = %v", i, err)
		}
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.SetCodexRetryFilterQueryService(store)

	c, rec := managementTestContext(http.MethodGet, "/v0/management/codex-response-retry-filter/stats?before_occurred_at=1970-01-01T00:01:42Z&before_id=4611686018427387903", "")
	h.GetCodexResponseRetryFilterStats(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var stats codexretryfilter.Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Attempts != 3 || stats.Hits != 3 {
		t.Fatalf("stats = %#v, want full 3/3 totals", stats)
	}
}

func TestCodexResponseRetryFilterPrune(t *testing.T) {
	ctx := context.Background()
	store, err := codexretryfilter.OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	length := int64(516)
	oldRecord := codexretryfilter.AttemptRecord{
		RequestID:       "req-old",
		OccurredAt:      time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
		ProviderKey:     "codex",
		AuthID:          "auth-1",
		Model:           "gpt-5-codex",
		Eligible:        true,
		Matched:         true,
		ReasoningTokens: &length,
		MatchedLength:   &length,
		Action:          codexretryfilter.ActionInternalRetry,
	}
	if err := store.InsertAttempt(ctx, oldRecord); err != nil {
		t.Fatalf("InsertAttempt() error = %v", err)
	}
	if err := store.InsertHit(ctx, oldRecord); err != nil {
		t.Fatalf("InsertHit() error = %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.SetCodexRetryFilterQueryService(store)

	c, rec := managementTestContext(http.MethodDelete, "/v0/management/codex-response-retry-filter/prune?before=2026-07-01T10:00:00Z", "")
	h.PruneCodexResponseRetryFilter(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("prune status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var result codexretryfilter.PruneResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode prune: %v", err)
	}
	if result.DeletedAttempts != 1 || result.DeletedHits != 1 {
		t.Fatalf("prune result = %#v", result)
	}
}

func managementTestContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if body == "" {
		c.Request = httptest.NewRequest(method, target, nil)
	} else {
		c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
	}
	return c, rec
}
