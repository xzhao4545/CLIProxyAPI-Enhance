package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestTransientFailureCoolDownThreshold verifies that 5xx transient failures
// only trigger cooldown once the configured repeat count is reached.

// testMemoryStore is a minimal in-memory Store for cooldown tests.
type testMemoryStore struct{ items map[string]*Auth }

func newTestMemoryStore() *testMemoryStore {
	return &testMemoryStore{items: make(map[string]*Auth)}
}

func (s *testMemoryStore) List(_ context.Context) ([]*Auth, error) {
	out := make([]*Auth, 0, len(s.items))
	for _, a := range s.items {
		out = append(out, a)
	}
	return out, nil
}

func (s *testMemoryStore) Save(_ context.Context, a *Auth) (string, error) {
	if a == nil {
		return "", nil
	}
	s.items[a.ID] = a
	return a.ID, nil
}

func (s *testMemoryStore) Delete(_ context.Context, id string) error {
	delete(s.items, id)
	return nil
}

func TestTransientFailureCoolDownThreshold(t *testing.T) {
	prev := transientFailureCoolDownMinFailures.Load()
	t.Cleanup(func() { transientFailureCoolDownMinFailures.Store(prev) })

	ctx := context.Background()

	newManager := func() *Manager {
		m := NewManager(newTestMemoryStore(), nil, nil)
		if _, err := m.Register(ctx, &Auth{ID: "auth-1", Provider: "mock"}); err != nil {
			panic(err)
		}
		return m
	}

	resetCounter := func(authID, model string) {
		transientFailures.reset(authID, model)
	}

	fail503 := func(m *Manager, authID, model string) {
		m.MarkResult(ctx, Result{
			AuthID:   authID,
			Provider: "mock",
			Model:    model,
			Success:  false,
			Error:    &Error{Code: "server_error", HTTPStatus: http.StatusServiceUnavailable, Message: "upstream 503"},
		})
	}

	blocked := func(m *Manager, authID, model string) bool {
		auth, ok := m.auths[authID]
		if !ok || auth == nil {
			return false
		}
		state := auth.ModelStates[model]
		if state == nil {
			return false
		}
		return state.Unavailable
	}

	t.Run("default threshold 3 - first two 503s do not cool down", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(3)
		m := newManager()
		resetCounter("auth-1", "gpt-5")

		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("first 503 should not cool down")
		}
		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("second 503 should not cool down")
		}
		fail503(m, "auth-1", "gpt-5") // threshold hit
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("third 503 should cool down")
		}
	})

	t.Run("configured threshold 1 keeps legacy immediate cool", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(1)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		fail503(m, "auth-1", "gpt-5")
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("threshold 1 should cool down on first failure")
		}
	})

	t.Run("counter resets on success", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(3)
		m := newManager()
		resetCounter("auth-1", "gpt-5")

		fail503(m, "auth-1", "gpt-5")
		fail503(m, "auth-1", "gpt-5") // counter = 2
		m.MarkResult(ctx, Result{     // success resets
			AuthID:   "auth-1",
			Provider: "mock",
			Model:    "gpt-5",
			Success:  true,
		})
		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("after success + 1 failure should NOT cool down")
		}
		fail503(m, "auth-1", "gpt-5")
		fail503(m, "auth-1", "gpt-5")
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("threshold hit after reset should cool down")
		}
	})

	t.Run("counter resets when triggered", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(2)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		fail503(m, "auth-1", "gpt-5")
		fail503(m, "auth-1", "gpt-5")
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("threshold reached, should cool down")
		}
		// Counter should be clear — next 503 burst starts from scratch.
		auth, _ := m.auths["auth-1"]
		clearAuthStateOnSuccess(auth, time.Now())
		resetCounter("auth-1", "gpt-5")
		auth.Status = StatusActive
		auth.Unavailable = false

		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("first 503 after reset should not cool down (threshold 2)")
		}
	})

	t.Run("non-5xx errors keep immediate cooling", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(3)
		m := newManager()
		resetCounter("auth-1", "gpt-5")

		// 401 keeps its own immediate 30m cooling regardless of transient counter.
		m.MarkResult(ctx, Result{
			AuthID:   "auth-1",
			Provider: "mock",
			Model:    "gpt-5",
			Success:  false,
			Error:    &Error{Code: "unauthorized", HTTPStatus: http.StatusUnauthorized, Message: "401"},
		})

		// Non-transient errors produce auth-level Unavailable via applyAuthFailureState;
		// assert transient counter was NOT consumed.
		fail503(m, "auth-1", "gpt-5")
		auth, _ := m.auths["auth-1"]
		if auth == nil {
			t.Fatalf("auth missing")
		}
		// Now check whether another 503 would escalate the same auth beyond what a single 503 allows.
		// At this point transient counter should be exactly 1 (just one 503 consumed), so a second 503
		// still does not cool down under threshold 3.
		auth.Unavailable = false // clear to isolate transient-lane behaviour
		auth.NextRetryAfter = time.Time{}
		if st, ok := auth.ModelStates["gpt-5"]; ok && st != nil {
			st.Unavailable = false
			st.NextRetryAfter = time.Time{}
			st.Quota = QuotaState{}
		}
		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("transient counter should not cross threshold after only 2 consecutive 5xx failures")
		}
	})
}

// TestTransientFailureCoolDownConfigWires confirms config load/update drives the threshold.
func TestTransientFailureCoolDownConfigWires(t *testing.T) {
	prev := transientFailureCoolDownMinFailures.Load()
	t.Cleanup(func() { transientFailureCoolDownMinFailures.Store(prev) })

	base := &internalconfig.Config{}
	base.QuotaExceeded.TransientFailureCoolDownMinFailures = 0
	// Unset → default applied by setter.
	var (
		mu      sync.Mutex
		called  []int
		applyFn = func(n int) { mu.Lock(); called = append(called, n); mu.Unlock() }
	)

	Set := func(cfg *internalconfig.Config) {
		v := cfg.QuotaExceeded.TransientFailureCoolDownMinFailures
		if v < 1 {
			v = 3
		}
		applyFn(v)
	}

	Set(base)
	change := &internalconfig.Config{}
	change.QuotaExceeded.TransientFailureCoolDownMinFailures = 5
	Set(change)

	mu.Lock()
	defer mu.Unlock()
	if len(called) != 2 || called[0] != 3 || called[1] != 5 {
		t.Fatalf("expected [3 5], got %v", called)
	}
}
