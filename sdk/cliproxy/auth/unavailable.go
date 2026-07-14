package auth

import (
	"strings"
	"time"
)

// UnavailableScope identifies whether an entry is auth-level or model-level.
type UnavailableScope string

const (
	UnavailableScopeAuth  UnavailableScope = "auth"
	UnavailableScopeModel UnavailableScope = "model"
)

// UnavailableFilter controls ListUnavailable matching.
type UnavailableFilter struct {
	// Provider matches auth.Provider (case-insensitive). Empty matches all.
	Provider string
	// AuthIndex matches auth.Index. Empty matches all.
	AuthIndex string
	// Model matches model key after trim. Empty matches all scopes.
	Model string
	// ActiveOnly keeps only entries still inside a future cooldown window (default for management list).
	ActiveOnly bool
	// IncludeNonBlocking includes Unavailable entries with a zero NextRetryAfter.
	// Those entries do not block selector routing today.
	IncludeNonBlocking bool
}

// UnavailableEntry is a management-facing snapshot of runtime cooldown/unavailable state.
type UnavailableEntry struct {
	Scope             UnavailableScope `json:"scope"`
	AuthIndex         string           `json:"auth_index,omitempty"`
	AuthID            string           `json:"auth_id,omitempty"`
	Provider          string           `json:"provider,omitempty"`
	Label             string           `json:"label,omitempty"`
	FileName          string           `json:"file_name,omitempty"`
	Model             string           `json:"model,omitempty"`
	Status            Status           `json:"status,omitempty"`
	StatusMessage     string           `json:"status_message,omitempty"`
	Unavailable       bool             `json:"unavailable"`
	Reason            string           `json:"reason,omitempty"`
	NextRetryAfter    *time.Time       `json:"next_retry_after,omitempty"`
	RetryAfterSeconds int64            `json:"retry_after_seconds"`
	Quota             *QuotaState      `json:"quota,omitempty"`
	LastError         *Error           `json:"last_error,omitempty"`
	UpdatedAt         *time.Time       `json:"updated_at,omitempty"`
	Blocking          bool             `json:"blocking"`
}

// ListUnavailable returns runtime unavailable/cooldown entries from registered auths.
// It is a read-only view over Manager.List() and does not change routing state.
func (m *Manager) ListUnavailable(filter UnavailableFilter) []UnavailableEntry {
	if m == nil {
		return nil
	}
	now := time.Now()
	providerFilter := strings.ToLower(strings.TrimSpace(filter.Provider))
	authIndexFilter := strings.TrimSpace(filter.AuthIndex)
	modelFilter := strings.TrimSpace(filter.Model)

	auths := m.List()
	out := make([]UnavailableEntry, 0)
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		auth.EnsureIndex()
		if authIndexFilter != "" && auth.Index != authIndexFilter {
			continue
		}
		provider := strings.TrimSpace(auth.Provider)
		if providerFilter != "" && strings.ToLower(provider) != providerFilter {
			continue
		}

		for modelKey, state := range auth.ModelStates {
			modelKey = strings.TrimSpace(modelKey)
			if modelKey == "" || state == nil {
				continue
			}
			if modelFilter != "" && modelKey != modelFilter {
				continue
			}
			if entry, ok := unavailableEntryFromModel(auth, modelKey, state, now, filter); ok {
				out = append(out, entry)
			}
		}

		if modelFilter == "" {
			if entry, ok := unavailableEntryFromAuth(auth, now, filter); ok {
				out = append(out, entry)
			}
		}
	}
	return out
}

func unavailableEntryFromAuth(auth *Auth, now time.Time, filter UnavailableFilter) (UnavailableEntry, bool) {
	if auth == nil {
		return UnavailableEntry{}, false
	}
	next := cooldownDeadline(auth.NextRetryAfter, auth.Quota.NextRecoverAt)
	blocking := auth.Unavailable && next.After(now)
	if !shouldIncludeUnavailable(auth.Unavailable, auth.Status, auth.StatusMessage, auth.Quota, auth.LastError, next, now, filter) {
		return UnavailableEntry{}, false
	}
	return UnavailableEntry{
		Scope:             UnavailableScopeAuth,
		AuthIndex:         auth.Index,
		AuthID:            auth.ID,
		Provider:          strings.TrimSpace(auth.Provider),
		Label:             strings.TrimSpace(auth.Label),
		FileName:          strings.TrimSpace(auth.FileName),
		Status:            auth.Status,
		StatusMessage:     strings.TrimSpace(auth.StatusMessage),
		Unavailable:       auth.Unavailable,
		Reason:            cooldownReason(auth.StatusMessage, auth.Quota, auth.LastError),
		NextRetryAfter:    timePtrIfSet(next),
		RetryAfterSeconds: retryAfterSeconds(next, now),
		Quota:             quotaStatePtr(auth.Quota),
		LastError:         cloneError(auth.LastError),
		UpdatedAt:         timePtrIfSet(auth.UpdatedAt),
		Blocking:          blocking,
	}, true
}

func unavailableEntryFromModel(auth *Auth, model string, state *ModelState, now time.Time, filter UnavailableFilter) (UnavailableEntry, bool) {
	if auth == nil || state == nil {
		return UnavailableEntry{}, false
	}
	next := cooldownDeadline(state.NextRetryAfter, state.Quota.NextRecoverAt)
	blocking := state.Unavailable && next.After(now)
	if !shouldIncludeUnavailable(state.Unavailable, state.Status, state.StatusMessage, state.Quota, state.LastError, next, now, filter) {
		return UnavailableEntry{}, false
	}
	return UnavailableEntry{
		Scope:             UnavailableScopeModel,
		AuthIndex:         auth.Index,
		AuthID:            auth.ID,
		Provider:          strings.TrimSpace(auth.Provider),
		Label:             strings.TrimSpace(auth.Label),
		FileName:          strings.TrimSpace(auth.FileName),
		Model:             model,
		Status:            state.Status,
		StatusMessage:     strings.TrimSpace(state.StatusMessage),
		Unavailable:       state.Unavailable,
		Reason:            cooldownReason(state.StatusMessage, state.Quota, state.LastError),
		NextRetryAfter:    timePtrIfSet(next),
		RetryAfterSeconds: retryAfterSeconds(next, now),
		Quota:             quotaStatePtr(state.Quota),
		LastError:         cloneError(state.LastError),
		UpdatedAt:         timePtrIfSet(state.UpdatedAt),
		Blocking:          blocking,
	}, true
}

func shouldIncludeUnavailable(unavailable bool, status Status, statusMessage string, quota QuotaState, lastErr *Error, next time.Time, now time.Time, filter UnavailableFilter) bool {
	blocking := unavailable && next.After(now)
	if filter.ActiveOnly {
		return blocking
	}

	hasSignal := unavailable || quota.Exceeded || lastErr != nil || strings.TrimSpace(statusMessage) != "" || status == StatusError || !next.IsZero()
	if !hasSignal {
		return false
	}
	// Non-blocking unavailable (Unavailable with zero deadline) is omitted unless requested.
	if unavailable && next.IsZero() && !filter.IncludeNonBlocking && !quota.Exceeded && lastErr == nil && strings.TrimSpace(statusMessage) == "" && status != StatusError {
		return false
	}
	return true
}

func cooldownDeadline(nextRetry, quotaRecover time.Time) time.Time {
	next := nextRetry
	if !quotaRecover.IsZero() {
		if next.IsZero() || quotaRecover.After(next) {
			next = quotaRecover
		}
	}
	return next
}

func retryAfterSeconds(next time.Time, now time.Time) int64 {
	if next.IsZero() {
		return 0
	}
	d := next.Sub(now)
	if d <= 0 {
		return 0
	}
	secs := int64(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return secs
}

func quotaStatePtr(quota QuotaState) *QuotaState {
	if !quota.Exceeded && strings.TrimSpace(quota.Reason) == "" && quota.NextRecoverAt.IsZero() && quota.BackoffLevel == 0 {
		return nil
	}
	copyQuota := quota
	return &copyQuota
}
