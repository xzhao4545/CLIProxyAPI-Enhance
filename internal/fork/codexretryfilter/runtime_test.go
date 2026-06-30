package codexretryfilter

import (
	"context"
	"testing"
)

func TestClearDefaultStoreOnlyClearsCurrentStore(t *testing.T) {
	ctx := context.Background()
	store1, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore(store1) error = %v", err)
	}
	defer func() { _ = store1.Close() }()
	store2, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore(store2) error = %v", err)
	}
	defer func() { _ = store2.Close() }()
	t.Cleanup(func() { ClearDefaultStore(nil) })

	SetDefaultStore(store1)
	if DefaultStore() != store1 {
		t.Fatal("DefaultStore() did not return store1")
	}

	ClearDefaultStore(store2)
	if DefaultStore() != store1 {
		t.Fatal("ClearDefaultStore(store2) cleared unrelated default store")
	}

	ClearDefaultStore(store1)
	if DefaultStore() != nil {
		t.Fatal("ClearDefaultStore(store1) did not clear default store")
	}

	SetDefaultStore(store1)
	ClearDefaultStore(nil)
	if DefaultStore() != nil {
		t.Fatal("ClearDefaultStore(nil) did not clear default store")
	}
}

func TestRecordAttemptBestEffortSkipsHitWhenAttemptInsertFails(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	t.Cleanup(func() { ClearDefaultStore(nil) })

	SetDefaultStore(store)
	if _, err := store.db.ExecContext(ctx, "DROP TABLE codex_response_retry_filter_attempts"); err != nil {
		t.Fatalf("drop attempts table: %v", err)
	}

	tokens := int64(516)
	RecordAttemptBestEffort(ctx, AttemptRecord{
		RequestID:       "req-fail",
		ProviderKey:     "codex",
		AuthID:          "auth-1",
		Model:           "gpt-5-codex",
		Eligible:        true,
		Matched:         true,
		ReasoningTokens: &tokens,
		MatchedLength:   &tokens,
		Action:          ActionInternalRetry,
	})

	var hits int64
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_response_retry_filter_hits").Scan(&hits); err != nil {
		t.Fatalf("count hits: %v", err)
	}
	if hits != 0 {
		t.Fatalf("hits = %d, want 0 when attempt insert fails", hits)
	}
}
