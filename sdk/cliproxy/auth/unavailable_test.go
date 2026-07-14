package auth

import (
	"context"
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
