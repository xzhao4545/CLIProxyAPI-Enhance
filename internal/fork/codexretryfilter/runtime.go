package codexretryfilter

import (
	"context"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

var defaultStore atomic.Value
var defaultStoreMu sync.Mutex

func SetDefaultStore(store *Store) {
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()
	defaultStore.Store(store)
}

func ClearDefaultStore(store *Store) {
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()
	current, _ := defaultStore.Load().(*Store)
	if store == nil || current == store {
		defaultStore.Store((*Store)(nil))
	}
}

func DefaultStore() *Store {
	store, _ := defaultStore.Load().(*Store)
	return store
}

func RecordAttemptBestEffort(ctx context.Context, record AttemptRecord) {
	store := DefaultStore()
	if store == nil {
		return
	}
	if err := store.InsertAttempt(ctx, record); err != nil {
		log.WithError(err).Warn("record codex response retry filter attempt failed")
	}
	if record.Matched {
		if err := store.InsertHit(ctx, record); err != nil {
			log.WithError(err).Warn("record codex response retry filter hit failed")
		}
	}
}

func MarkFinalSuccessBestEffort(ctx context.Context, requestID string) {
	store := DefaultStore()
	if store == nil {
		return
	}
	if err := store.MarkFinalSuccess(ctx, requestID); err != nil {
		log.WithError(err).Warn("mark codex response retry filter final success failed")
	}
}
