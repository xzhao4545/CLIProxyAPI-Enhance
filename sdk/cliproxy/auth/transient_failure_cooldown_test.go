package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestTransientFailureCoolDownThreshold verifies that retryable transient failures
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

	fail := func(m *Manager, authID, model string, resultErr *Error, retryAfter *time.Duration) {
		m.MarkResult(ctx, Result{
			AuthID:     authID,
			Provider:   "mock",
			Model:      model,
			Success:    false,
			Error:      resultErr,
			RetryAfter: retryAfter,
		})
	}
	fail503 := func(m *Manager, authID, model string) {
		fail(m, authID, model, &Error{Code: "server_error", HTTPStatus: http.StatusServiceUnavailable, Message: "upstream 503"}, nil)
	}

	blocked := func(m *Manager, authID, model string) bool {
		auth, ok := m.auths[authID]
		if !ok || auth == nil {
			return false
		}
		isBlocked, _, _ := isAuthBlockedForModel(auth, model, time.Now())
		return isBlocked
	}

	t.Run("default threshold 5 leaves four downstream retries unblocked", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(0)
		m := newManager()
		resetCounter("auth-1", "gpt-5")

		for attempt := 1; attempt < defaultTransientFailureCoolDownMinFailures; attempt++ {
			fail503(m, "auth-1", "gpt-5")
			if blocked(m, "auth-1", "gpt-5") {
				t.Fatalf("attempt %d should not cool down", attempt)
			}
		}
		fail503(m, "auth-1", "gpt-5")
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("fifth 503 should cool down")
		}
	})

	t.Run("408 uses the same threshold", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(2)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		err408 := &Error{Code: "request_timeout", HTTPStatus: http.StatusRequestTimeout, Message: "upstream timeout", Retryable: true}

		fail(m, "auth-1", "gpt-5", err408, nil)
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatal("first 408 should not cool down")
		}
		fail(m, "auth-1", "gpt-5", err408, nil)
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatal("second 408 should cool down")
		}
	})

	t.Run("429 without retry after is transient", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(2)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		err429 := &Error{Code: "rate_limit_exceeded", HTTPStatus: http.StatusTooManyRequests, Message: "rate limited", Retryable: true}

		fail(m, "auth-1", "gpt-5", err429, nil)
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatal("first 429 without Retry-After should not cool down")
		}
		fail(m, "auth-1", "gpt-5", err429, nil)
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatal("second 429 without Retry-After should cool down")
		}
	})

	t.Run("429 with retry after cools immediately", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(5)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		retryAfter := time.Minute

		fail(m, "auth-1", "gpt-5", &Error{Code: "quota_exhausted", HTTPStatus: http.StatusTooManyRequests, Message: "quota exhausted"}, &retryAfter)
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatal("429 with Retry-After should cool down immediately")
		}
		auth := m.auths["auth-1"]
		if auth.ModelStates["gpt-5"].Quota.Reason != "quota" {
			t.Fatalf("quota reason = %q, want quota", auth.ModelStates["gpt-5"].Quota.Reason)
		}
	})

	t.Run("status-less retryable errors use the threshold", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(2)
		m := newManager()
		resetCounter("auth-1", "gpt-5")
		emptyStream := &Error{Code: "empty_stream", Message: "upstream stream has no source", Retryable: true}

		fail(m, "auth-1", "gpt-5", emptyStream, nil)
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatal("first retryable status-less failure should not cool down")
		}
		fail(m, "auth-1", "gpt-5", emptyStream, nil)
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatal("second retryable status-less failure should cool down")
		}
	})

	t.Run("auth-level transient failures use the threshold", func(t *testing.T) {
		SetTransientFailureCoolDownMinFailures(2)
		m := newManager()
		resetCounter("auth-1", "")
		err503 := &Error{Code: "server_error", HTTPStatus: http.StatusServiceUnavailable, Message: "upstream 503"}

		fail(m, "auth-1", "", err503, nil)
		if m.auths["auth-1"].Unavailable {
			t.Fatal("first auth-level 503 should not cool down")
		}
		fail(m, "auth-1", "", err503, nil)
		if !m.auths["auth-1"].Unavailable {
			t.Fatal("second auth-level 503 should cool down")
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
		resetModelState(auth.ModelStates["gpt-5"], time.Now())
		resetCounter("auth-1", "gpt-5")
		auth.Status = StatusActive
		auth.Unavailable = false

		fail503(m, "auth-1", "gpt-5")
		if blocked(m, "auth-1", "gpt-5") {
			t.Fatalf("first 503 after reset should not cool down (threshold 2)")
		}
	})

	t.Run("hard errors keep immediate cooling", func(t *testing.T) {
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
		if !blocked(m, "auth-1", "gpt-5") {
			t.Fatal("401 should cool down immediately")
		}

		// A concurrent transient result must not clear the active hard-error cooldown
		// or consume the next transient-failure burst.
		fail503(m, "auth-1", "gpt-5")
		auth, _ := m.auths["auth-1"]
		if auth == nil {
			t.Fatalf("auth missing")
		}
		// Clear the hard-error state, then verify the next 503 starts at one.
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
			v = defaultTransientFailureCoolDownMinFailures
		}
		applyFn(v)
	}

	Set(base)
	change := &internalconfig.Config{}
	change.QuotaExceeded.TransientFailureCoolDownMinFailures = 7
	Set(change)

	mu.Lock()
	defer mu.Unlock()
	if len(called) != 2 || called[0] != defaultTransientFailureCoolDownMinFailures || called[1] != 7 {
		t.Fatalf("expected [%d 7], got %v", defaultTransientFailureCoolDownMinFailures, called)
	}
}
