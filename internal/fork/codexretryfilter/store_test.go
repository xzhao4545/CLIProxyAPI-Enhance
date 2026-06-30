package codexretryfilter

import (
	"context"
	"fmt"
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
	passTokens := int64(42)
	pass.ReasoningTokens = &passTokens
	pass.Action = ActionPass
	pass.Attempt = 2
	pass.GuardRetryRemaining = 1
	if err := store.InsertAttempt(ctx, pass); err != nil {
		t.Fatalf("InsertAttempt(pass) error = %v", err)
	}
	miss := base
	miss.RequestID = "req-2"
	miss.AuthID = "auth-2"
	miss.AuthLabel = "Secondary"
	miss.Model = "gpt-4.1-codex"
	miss.Matched = false
	miss.MatchedLength = nil
	missTokens := int64(128)
	miss.ReasoningTokens = &missTokens
	miss.Action = ActionPass
	miss.Attempt = 1
	miss.GuardRetryRemaining = 0
	if err := store.InsertAttempt(ctx, miss); err != nil {
		t.Fatalf("InsertAttempt(miss) error = %v", err)
	}
	if err := store.MarkFinalSuccess(ctx, "req-1"); err != nil {
		t.Fatalf("MarkFinalSuccess() error = %v", err)
	}

	stats, err := store.QueryStats(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if stats.Attempts != 3 || stats.Hits != 1 {
		t.Fatalf("stats attempts/hits = %d/%d, want 3/1", stats.Attempts, stats.Hits)
	}
	if stats.HitRate != float64(1)/float64(3) {
		t.Fatalf("hit rate = %v, want 1/3", stats.HitRate)
	}
	if stats.FinalSuccessesAfterHit != 1 || stats.RetrySuccessRate != 1 {
		t.Fatalf("success stats = %d/%v, want 1/1", stats.FinalSuccessesAfterHit, stats.RetrySuccessRate)
	}
	if stats.InternalRetries != 1 || stats.ConductorRetries != 0 || stats.ObserveOnlyHits != 0 {
		t.Fatalf("action counts = %d/%d/%d, want 1/0/0", stats.InternalRetries, stats.ConductorRetries, stats.ObserveOnlyHits)
	}
	if len(stats.ByModel) != 2 ||
		stats.ByModel[0].Key != "gpt-5-codex" || stats.ByModel[0].Attempts != 2 || stats.ByModel[0].Hits != 1 ||
		stats.ByModel[1].Key != "gpt-4.1-codex" || stats.ByModel[1].Attempts != 1 || stats.ByModel[1].Hits != 0 {
		t.Fatalf("by model = %#v, want hit and zero-hit attempt rows", stats.ByModel)
	}
	if len(stats.ByAuth) != 2 ||
		stats.ByAuth[0].Key != "auth-1" || stats.ByAuth[0].Label != "Primary" || stats.ByAuth[0].Attempts != 2 || stats.ByAuth[0].Hits != 1 ||
		stats.ByAuth[1].Key != "auth-2" || stats.ByAuth[1].Label != "Secondary" || stats.ByAuth[1].Attempts != 1 || stats.ByAuth[1].Hits != 0 {
		t.Fatalf("by auth = %#v, want hit and zero-hit attempt rows", stats.ByAuth)
	}
	if len(stats.ByReasoningTokens) != 1 || stats.ByReasoningTokens[0].MatchedLength != 516 || stats.ByReasoningTokens[0].Hits != 1 {
		t.Fatalf("by reasoning = %#v, want 516/1", stats.ByReasoningTokens)
	}
	if len(stats.ByAction) != 1 || stats.ByAction[0].Action != ActionInternalRetry || stats.ByAction[0].Hits != 1 {
		t.Fatalf("by action = %#v, want internal_retry/1", stats.ByAction)
	}

	lengthStats, err := store.QueryStats(ctx, QueryFilter{MatchedLength: 516})
	if err != nil {
		t.Fatalf("QueryStats(matched_length) error = %v", err)
	}
	if lengthStats.Attempts != 1 || lengthStats.Hits != 1 || lengthStats.HitRate != 1 {
		t.Fatalf("matched length stats = attempts:%d hits:%d rate:%v, want 1/1/1", lengthStats.Attempts, lengthStats.Hits, lengthStats.HitRate)
	}

	hits, err := store.QueryHits(ctx, QueryFilter{Model: "gpt-5-codex", MatchedLength: 516, Limit: 10})
	if err != nil {
		t.Fatalf("QueryHits() error = %v", err)
	}
	if len(hits.Hits) != 1 {
		t.Fatalf("hits len = %d, want 1", len(hits.Hits))
	}
	if hit := hits.Hits[0]; !hit.Stream || !hit.Retried || !hit.FinalSuccess || hit.GuardRetryRemaining != 2 {
		t.Fatalf("hit flags = stream:%v retried:%v final:%v remaining:%d", hit.Stream, hit.Retried, hit.FinalSuccess, hit.GuardRetryRemaining)
	}
}

func TestStoreQueryHitsCursorPagination(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	tokens := int64(516)
	for i := 0; i < 3; i++ {
		record := AttemptRecord{
			RequestID:       fmt.Sprintf("req-%d", i),
			OccurredAt:      time.Unix(int64(100+i), 0).UTC(),
			ProviderKey:     "codex",
			AuthID:          "auth-1",
			AuthLabel:       "Primary",
			Model:           "gpt-5-codex",
			Eligible:        true,
			Matched:         true,
			ReasoningTokens: &tokens,
			MatchedLength:   &tokens,
			Action:          ActionInternalRetry,
		}
		if err := store.InsertAttempt(ctx, record); err != nil {
			t.Fatalf("InsertAttempt(%d) error = %v", i, err)
		}
		if err := store.InsertHit(ctx, record); err != nil {
			t.Fatalf("InsertHit(%d) error = %v", i, err)
		}
	}

	page1, err := store.QueryHits(ctx, QueryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("QueryHits(page1) error = %v", err)
	}
	if len(page1.Hits) != 2 || !page1.HasMore || page1.NextBeforeOccurred == nil || page1.NextBeforeID == nil {
		t.Fatalf("page1 = %#v", page1)
	}
	if page1.Hits[0].OccurredAt.Before(page1.Hits[1].OccurredAt) {
		t.Fatalf("page1 ordering invalid: %#v", page1.Hits)
	}

	page2, err := store.QueryHits(ctx, QueryFilter{
		Limit:      2,
		BeforeTime: page1.NextBeforeOccurred,
		BeforeID:   *page1.NextBeforeID,
	})
	if err != nil {
		t.Fatalf("QueryHits(page2) error = %v", err)
	}
	if len(page2.Hits) != 1 || page2.HasMore {
		t.Fatalf("page2 = %#v", page2)
	}
	if !page2.Hits[0].OccurredAt.Before(page1.Hits[1].OccurredAt) {
		t.Fatalf("page2 first hit not older than page1 tail: page1=%#v page2=%#v", page1.Hits, page2.Hits)
	}
}

func TestStoreQueryStatsUsesRollupForFullHourWindow(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	tokens := int64(516)
	occurred := time.Date(2026, 7, 1, 10, 5, 0, 0, time.UTC)
	record := AttemptRecord{
		RequestID:       "req-rollup",
		OccurredAt:      occurred,
		ProviderKey:     "codex",
		AuthID:          "auth-rollup",
		AuthLabel:       "Rollup",
		Model:           "gpt-5-codex",
		Eligible:        true,
		Matched:         true,
		ReasoningTokens: &tokens,
		MatchedLength:   &tokens,
		Action:          ActionInternalRetry,
	}
	if err := store.InsertAttempt(ctx, record); err != nil {
		t.Fatalf("InsertAttempt() error = %v", err)
	}
	if err := store.InsertHit(ctx, record); err != nil {
		t.Fatalf("InsertHit() error = %v", err)
	}
	if err := store.MarkFinalSuccess(ctx, record.RequestID); err != nil {
		t.Fatalf("MarkFinalSuccess() error = %v", err)
	}

	from := occurred.Truncate(time.Hour)
	to := from.Add(time.Hour)
	stats, err := store.QueryStats(ctx, QueryFilter{
		DateFrom: &from,
		DateTo:   &to,
	})
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if stats.Attempts != 1 || stats.Hits != 1 || stats.FinalSuccessesAfterHit != 1 {
		t.Fatalf("stats = %#v, want attempts/hits/success = 1/1/1", stats)
	}
	if stats.RetrySuccessRate != 1 {
		t.Fatalf("retry success rate = %v, want 1", stats.RetrySuccessRate)
	}
	if len(stats.ByAuth) != 1 || stats.ByAuth[0].Label != "Rollup" {
		t.Fatalf("by auth = %#v", stats.ByAuth)
	}
}

func TestStorePruneOlderThan(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	tokens := int64(516)
	oldRecord := AttemptRecord{
		RequestID:       "req-old",
		OccurredAt:      time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
		ProviderKey:     "codex",
		AuthID:          "auth-1",
		Model:           "gpt-5-codex",
		Eligible:        true,
		Matched:         true,
		ReasoningTokens: &tokens,
		MatchedLength:   &tokens,
		Action:          ActionInternalRetry,
	}
	newRecord := oldRecord
	newRecord.RequestID = "req-new"
	newRecord.OccurredAt = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	for _, record := range []AttemptRecord{oldRecord, newRecord} {
		if err := store.InsertAttempt(ctx, record); err != nil {
			t.Fatalf("InsertAttempt(%s) error = %v", record.RequestID, err)
		}
		if err := store.InsertHit(ctx, record); err != nil {
			t.Fatalf("InsertHit(%s) error = %v", record.RequestID, err)
		}
	}

	before := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	pruned, err := store.PruneOlderThan(ctx, before)
	if err != nil {
		t.Fatalf("PruneOlderThan() error = %v", err)
	}
	if pruned.DeletedAttempts != 1 || pruned.DeletedHits != 1 {
		t.Fatalf("pruned = %#v, want 1 attempt and 1 hit deleted", pruned)
	}

	stats, err := store.QueryStats(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if stats.Attempts != 1 || stats.Hits != 1 {
		t.Fatalf("stats after prune = %#v, want attempts/hits = 1/1", stats)
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

func TestStoreStatsByAuthPreservesRecordedAuthLabels(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	records := []AttemptRecord{
		{
			RequestID: "req-default-label",
			AuthID:    "codex:apikey:6b19d5b7165d",
			AuthLabel: "codex-apikey",
			Model:     "gpt-5-codex",
			Eligible:  true,
			Action:    ActionPass,
		},
		{
			RequestID: "req-id-label",
			AuthID:    "codex:apikey:6b19d5b7165e",
			AuthLabel: "codex:apikey:6b19d5b7165e",
			Model:     "gpt-5-codex",
			Eligible:  true,
			Action:    ActionPass,
		},
	}
	for _, record := range records {
		if err := store.InsertAttempt(ctx, record); err != nil {
			t.Fatalf("InsertAttempt(%s) error = %v", record.RequestID, err)
		}
	}

	stats, err := store.QueryStats(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if len(stats.ByAuth) != 2 {
		t.Fatalf("by auth len = %d, want 2: %#v", len(stats.ByAuth), stats.ByAuth)
	}
	for _, row := range stats.ByAuth {
		switch row.Key {
		case "codex:apikey:6b19d5b7165d":
			if row.Label != "codex-apikey" {
				t.Fatalf("by auth row %q label = %q, want codex-apikey", row.Key, row.Label)
			}
		case "codex:apikey:6b19d5b7165e":
			if row.Label != "codex:apikey:6b19d5b7165e" {
				t.Fatalf("by auth row %q label = %q, want codex:apikey:6b19d5b7165e", row.Key, row.Label)
			}
		default:
			t.Fatalf("unexpected by auth row key %q", row.Key)
		}
	}
}
