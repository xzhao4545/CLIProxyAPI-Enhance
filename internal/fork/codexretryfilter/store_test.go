package codexretryfilter

import (
	"context"
	"testing"
	"time"
)

func TestStoreInsertQueryStatsAndHits(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() {
		if errClose := store.Close(); errClose != nil {
			t.Fatalf("Close() error = %v", errClose)
		}
	}()

	tokens := int64(516)
	base := AttemptRecord{
		RequestID:           "req-1",
		OccurredAt:          time.Unix(100, 0).UTC(),
		ProviderKey:         "codex",
		AuthID:              "auth-1",
		AuthLabel:           "Primary",
		Model:               "gpt-5-codex",
		ClientModel:         "gpt-5-codex(high)",
		ResponseModel:       "gpt-5-codex",
		Stream:              true,
		Eligible:            true,
		Matched:             true,
		ReasoningTokens:     &tokens,
		MatchedLength:       &tokens,
		Action:              ActionInternalRetry,
		GuardRetryRemaining: 2,
		Attempt:             1,
	}
	if err := store.InsertAttempt(ctx, base); err != nil {
		t.Fatalf("InsertAttempt(match) error = %v", err)
	}
	if err := store.InsertHit(ctx, base); err != nil {
		t.Fatalf("InsertHit() error = %v", err)
	}

	pass := base
	pass.RequestID = "req-1"
	pass.Matched = false
	pass.MatchedLength = nil
	pass.Action = ActionPass
	pass.Attempt = 2
	pass.GuardRetryRemaining = 1
	if err := store.InsertAttempt(ctx, pass); err != nil {
		t.Fatalf("InsertAttempt(pass) error = %v", err)
	}
	if err := store.MarkFinalSuccess(ctx, "req-1"); err != nil {
		t.Fatalf("MarkFinalSuccess() error = %v", err)
	}

	stats, err := store.QueryStats(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if stats.Attempts != 2 || stats.Hits != 1 {
		t.Fatalf("stats attempts/hits = %d/%d, want 2/1", stats.Attempts, stats.Hits)
	}
	if stats.HitRate != 0.5 {
		t.Fatalf("hit rate = %v, want 0.5", stats.HitRate)
	}
	if stats.FinalSuccessesAfterHit != 1 || stats.RetrySuccessRate != 1 {
		t.Fatalf("success stats = %d/%v, want 1/1", stats.FinalSuccessesAfterHit, stats.RetrySuccessRate)
	}
	if stats.InternalRetries != 1 || stats.ConductorRetries != 0 || stats.ObserveOnlyHits != 0 {
		t.Fatalf("action counts = %d/%d/%d, want 1/0/0", stats.InternalRetries, stats.ConductorRetries, stats.ObserveOnlyHits)
	}
	if len(stats.ByModel) != 1 || stats.ByModel[0].Key != "gpt-5-codex" || stats.ByModel[0].Attempts != 2 || stats.ByModel[0].Hits != 1 {
		t.Fatalf("by model = %#v, want gpt-5-codex 2/1", stats.ByModel)
	}
	if len(stats.ByReasoningTokens) != 1 || stats.ByReasoningTokens[0].MatchedLength != 516 || stats.ByReasoningTokens[0].Hits != 1 {
		t.Fatalf("by reasoning = %#v, want 516/1", stats.ByReasoningTokens)
	}
	if len(stats.ByAction) != 1 || stats.ByAction[0].Action != ActionInternalRetry || stats.ByAction[0].Hits != 1 {
		t.Fatalf("by action = %#v, want internal_retry/1", stats.ByAction)
	}

	hits, err := store.QueryHits(ctx, QueryFilter{Model: "gpt-5-codex", MatchedLength: 516, Limit: 10})
	if err != nil {
		t.Fatalf("QueryHits() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits len = %d, want 1", len(hits))
	}
	if hit := hits[0]; !hit.Stream || !hit.Retried || !hit.FinalSuccess || hit.GuardRetryRemaining != 2 {
		t.Fatalf("hit flags = stream:%v retried:%v final:%v remaining:%d", hit.Stream, hit.Retried, hit.FinalSuccess, hit.GuardRetryRemaining)
	}
}

func TestStoreStatsActionFilters(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	length := int64(1034)
	for _, action := range []string{ActionObserveOnly, ActionConductorRetry} {
		record := AttemptRecord{
			RequestID:       "req-" + action,
			ProviderKey:     "codex",
			AuthID:          "auth-2",
			Model:           "gpt-5-codex",
			Eligible:        true,
			Matched:         true,
			ReasoningTokens: &length,
			MatchedLength:   &length,
			Action:          action,
		}
		if err := store.InsertAttempt(ctx, record); err != nil {
			t.Fatalf("InsertAttempt(%s) error = %v", action, err)
		}
		if err := store.InsertHit(ctx, record); err != nil {
			t.Fatalf("InsertHit(%s) error = %v", action, err)
		}
	}

	stats, err := store.QueryStats(ctx, QueryFilter{Action: ActionObserveOnly})
	if err != nil {
		t.Fatalf("QueryStats(action) error = %v", err)
	}
	if stats.Attempts != 1 || stats.Hits != 1 || stats.ObserveOnlyHits != 1 {
		t.Fatalf("observe stats = attempts:%d hits:%d observe:%d", stats.Attempts, stats.Hits, stats.ObserveOnlyHits)
	}
}
