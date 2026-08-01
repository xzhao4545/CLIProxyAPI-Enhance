package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// ErrModelStateNotFound is returned when ResetQuotaModel cannot find the target model state.
var ErrModelStateNotFound = errors.New("model state not found")

func hasQuotaCooldownSignals(unavailable bool, nextRetry time.Time, quota QuotaState) bool {
	return unavailable ||
		!nextRetry.IsZero() ||
		quota.Exceeded ||
		!quota.NextRecoverAt.IsZero() ||
		quota.BackoffLevel != 0 ||
		strings.TrimSpace(quota.Reason) != ""
}

// HasCooldownState reports whether auth-level or any model still carries cooldown/quota signals.
func (a *Auth) HasCooldownState() bool {
	if a == nil {
		return false
	}
	if hasQuotaCooldownSignals(a.Unavailable, a.NextRetryAfter, a.Quota) {
		return true
	}
	for _, state := range a.ModelStates {
		if state == nil {
			continue
		}
		if hasQuotaCooldownSignals(state.Unavailable, state.NextRetryAfter, state.Quota) {
			return true
		}
	}
	return false
}

// ResetQuotaModel clears quota/cooldown state for one model under an auth and resumes registry routing.
func (m *Manager) ResetQuotaModel(ctx context.Context, authID, model string) (*Auth, error) {
	if m == nil {
		return nil, nil
	}
	authID = strings.TrimSpace(authID)
	model = strings.TrimSpace(model)
	if authID == "" {
		return nil, fmt.Errorf("auth id is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	now := time.Now()
	var snapshot *Auth

	m.mu.Lock()
	auth, ok := m.auths[authID]
	if !ok || auth == nil {
		m.mu.Unlock()
		return nil, nil
	}

	state := auth.ModelStates[model]
	resumeModels := []string{model}
	if state == nil {
		base := canonicalModelKey(model)
		if base != "" && base != model {
			if alt := auth.ModelStates[base]; alt != nil {
				state = alt
				resumeModels = []string{model, base}
			}
		}
	}
	if state == nil {
		for key, candidate := range auth.ModelStates {
			if candidate == nil {
				continue
			}
			// Fallback: match canonical model name in either direction
			candidateName := canonicalModelKey(key)
			if candidateName != "" && candidateName == canonicalModelKey(model) {
				state = candidate
				resumeModels = []string{model, key, canonicalModelKey(model), canonicalModelKey(key)}
				break
			}
		}
	}
	if state == nil {
		m.mu.Unlock()
		return nil, ErrModelStateNotFound
	}

	resetModelState(state, now)
	if len(auth.ModelStates) > 0 {
		updateAggregatedAvailability(auth, now)
	}
	if !auth.Disabled && auth.Status != StatusDisabled && !hasModelError(auth, now) {
		auth.LastError = nil
		auth.StatusMessage = ""
		auth.Status = StatusActive
	}
	auth.UpdatedAt = now
	if errPersist := m.persist(ctx, auth); errPersist != nil {
		m.mu.Unlock()
		return nil, errPersist
	}
	snapshot = auth.Clone()
	m.mu.Unlock()

	for _, key := range resumeModels {
		if strings.TrimSpace(key) == "" {
			continue
		}
		registry.GetGlobalRegistry().ClearModelQuotaExceeded(authID, key)
		registry.GetGlobalRegistry().ResumeClientModel(authID, key)
	}
	if m.scheduler != nil && snapshot != nil {
		m.scheduler.upsertAuth(snapshot)
	}
	return snapshot, nil
}
