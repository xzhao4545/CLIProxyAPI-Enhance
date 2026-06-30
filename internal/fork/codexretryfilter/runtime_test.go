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
