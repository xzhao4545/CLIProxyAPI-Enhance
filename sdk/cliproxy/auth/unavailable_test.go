package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListUnavailable_ActiveOnlyReasonAndRetrySeconds(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	next := time.Now().Add(90 * time.Second)
	auth := &Auth{
		ID:       "auth-unavailable-1",
		Provider: "codex",
		Label:    "codex-main",
		ModelStates: map[string]*ModelState{
			"gpt-5": {
				Status:         StatusError,
				StatusMessage:  "quota exhausted",
				Unavailable:    true,
				NextRetryAfter: next,
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 2},
				LastError:      &Error{Code: "rate_limit", Message: "quota exhausted", HTTPStatus: 429},
			},
			"gpt-clean": {
				Status: StatusActive,
			},
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	items := manager.ListUnavailable(UnavailableFilter{ActiveOnly: true})
	if len(items) != 1 {
		t.Fatalf("ListUnavailable() len = %d, want 1; items=%+v", len(items), items)
	}
	item := items[0]
	if item.Scope != UnavailableScopeModel || item.Model != "gpt-5" {
		t.Fatalf("item scope/model = %s/%s, want model/gpt-5", item.Scope, item.Model)
	}
	if item.Reason != "quota" {
		t.Fatalf("reason = %q, want quota", item.Reason)
	}
	if !item.Blocking {
		t.Fatal("blocking = false, want true")
	}
	if item.RetryAfterSeconds < 80 || item.RetryAfterSeconds > 95 {
		t.Fatalf("retry_after_seconds = %d, want ~90", item.RetryAfterSeconds)
	}
	if item.NextRetryAfter == nil || item.NextRetryAfter.IsZero() {
		t.Fatal("next_retry_after is nil/zero")
	}
}

func TestListUnavailable_FiltersAndNonBlocking(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	next := time.Now().Add(time.Hour)
	authA := &Auth{
		ID:       "auth-a",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			"gpt-5": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
				StatusMessage:  "quota",
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next},
			},
		},
	}
	authB := &Auth{
		ID:             "auth-b",
		Provider:       "claude",
		Unavailable:    true,
		Status:         StatusError,
		StatusMessage:  "soft mark",
		NextRetryAfter: time.Time{}, // non-blocking
	}
	if _, err := manager.Register(context.Background(), authA); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if _, err := manager.Register(context.Background(), authB); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	if got := manager.ListUnavailable(UnavailableFilter{ActiveOnly: true, Provider: "claude"}); len(got) != 0 {
		t.Fatalf("active claude items = %d, want 0", len(got))
	}
	if got := manager.ListUnavailable(UnavailableFilter{ActiveOnly: true, Provider: "codex"}); len(got) != 1 {
		t.Fatalf("active codex items = %d, want 1", len(got))
	}

	nonBlocking := manager.ListUnavailable(UnavailableFilter{ActiveOnly: false, IncludeNonBlocking: true, Provider: "claude"})
	if len(nonBlocking) != 1 {
		t.Fatalf("nonblocking claude items = %d, want 1", len(nonBlocking))
	}
	if nonBlocking[0].Blocking || nonBlocking[0].RetryAfterSeconds != 0 {
		t.Fatalf("nonblocking entry = %+v, want Blocking=false RetryAfterSeconds=0", nonBlocking[0])
	}
}

func TestListUnavailable_ModelFilterCanonicalCaseInsensitive(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	next := time.Now().Add(time.Hour)
	auth := &Auth{
		ID:       "auth-model-filter",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			"GPT-5": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next},
			},
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	items := manager.ListUnavailable(UnavailableFilter{ActiveOnly: true, Model: "gpt-5"})
	if len(items) != 1 || items[0].Model != "GPT-5" {
		t.Fatalf("items = %+v, want GPT-5 match via case-insensitive filter", items)
	}
}

func TestResetQuotaModel_ClearsOnlyTargetModel(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	next := time.Now().Add(time.Hour)
	auth := &Auth{
		ID:       "auth-model-reset",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			"gpt-5": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
				StatusMessage:  "quota",
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 1},
			},
			"gpt-4.1": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
				StatusMessage:  "quota",
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 1},
			},
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	updated, err := manager.ResetQuotaModel(context.Background(), auth.ID, "gpt-5")
	if err != nil {
		t.Fatalf("ResetQuotaModel: %v", err)
	}
	if updated == nil {
		t.Fatal("updated is nil")
	}
	cleared := updated.ModelStates["gpt-5"]
	if cleared == nil || cleared.Unavailable || !cleared.NextRetryAfter.IsZero() || cleared.Quota.Exceeded {
		t.Fatalf("gpt-5 state = %+v, want cleared", cleared)
	}
	kept := updated.ModelStates["gpt-4.1"]
	if kept == nil || !kept.Unavailable || kept.NextRetryAfter.IsZero() {
		t.Fatalf("gpt-4.1 state = %+v, want still cooling", kept)
	}
}

func TestResetQuotaModel_PreservesSiblingAndAggregatedQuota(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	next := time.Now().Add(time.Hour)
	// Sibling is StatusActive + Unavailable (hasModelError would miss it).
	// Auth-level Unavailable means *all* models blocked; after clearing one model,
	// aggregation correctly clears auth.Unavailable. Auth quota must still reflect the sibling.
	auth := &Auth{
		ID:             "auth-agg-preserve",
		Provider:       "codex",
		Unavailable:    true,
		NextRetryAfter: next,
		Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 2},
		ModelStates: map[string]*ModelState{
			"gpt-5": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 1},
			},
			"gpt-4.1": {
				Status:         StatusActive,
				Unavailable:    true,
				NextRetryAfter: next,
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next, BackoffLevel: 3},
			},
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	updated, err := manager.ResetQuotaModel(context.Background(), auth.ID, "gpt-5")
	if err != nil {
		t.Fatalf("ResetQuotaModel: %v", err)
	}
	if updated == nil {
		t.Fatal("updated is nil")
	}
	// Not all models blocked → auth.Unavailable should be false (aggregation).
	if updated.Unavailable {
		t.Fatal("auth.Unavailable = true after partial model clear, want false")
	}
	// Sibling still cooling and auth quota must remain aggregated from it.
	kept := updated.ModelStates["gpt-4.1"]
	if kept == nil || !kept.Unavailable || kept.NextRetryAfter.IsZero() {
		t.Fatalf("sibling = %+v, want still cooling", kept)
	}
	if !updated.Quota.Exceeded || updated.Quota.BackoffLevel != 3 {
		t.Fatalf("auth.Quota = %+v, want exceeded with sibling backoff 3", updated.Quota)
	}
}
func TestResetQuotaModel_NotFound(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-missing-model",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			"gpt-5": {Status: StatusActive},
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := manager.ResetQuotaModel(context.Background(), auth.ID, "does-not-exist")
	if !errors.Is(err, ErrModelStateNotFound) {
		t.Fatalf("err = %v, want ErrModelStateNotFound", err)
	}
}

func TestClearCooldownStateForAuth_IncludesBackoffLevel(t *testing.T) {
	now := time.Now()
	auth := &Auth{
		ID:    "auth-backoff-only",
		Quota: QuotaState{BackoffLevel: 3},
		ModelStates: map[string]*ModelState{
			"gpt-5": {Quota: QuotaState{BackoffLevel: 2}},
		},
	}
	if !auth.HasCooldownState() {
		t.Fatal("HasCooldownState = false for backoff-only, want true")
	}
	if !clearCooldownStateForAuth(auth, now) {
		t.Fatal("clearCooldownStateForAuth changed = false, want true")
	}
	if auth.Quota.BackoffLevel != 0 {
		t.Fatalf("auth backoff = %d, want 0", auth.Quota.BackoffLevel)
	}
	if auth.ModelStates["gpt-5"].Quota.BackoffLevel != 0 {
		t.Fatalf("model backoff = %d, want 0", auth.ModelStates["gpt-5"].Quota.BackoffLevel)
	}
	if auth.HasCooldownState() {
		t.Fatal("HasCooldownState still true after clear")
	}
}
