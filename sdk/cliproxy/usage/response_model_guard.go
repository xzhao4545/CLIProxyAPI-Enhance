package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

type responseModelGuardKey struct{}

// ResponseModelValidationError identifies a locally rejected model substitution.
// It stops retries without penalizing the selected credential.
type ResponseModelValidationError struct {
	Provider      string
	Model         string
	ResponseModel string
}

func (e *ResponseModelValidationError) Error() string {
	return fmt.Sprintf("upstream response model %q does not match selected model %q for provider %q", e.ResponseModel, e.Model, e.Provider)
}

func (e *ResponseModelValidationError) StatusCode() int       { return http.StatusBadGateway }
func (e *ResponseModelValidationError) IsRequestScoped() bool { return true }

func (e *ResponseModelValidationError) ResponseBody() []byte {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{
		"type": "upstream_response_error", "code": "response_model_mismatch", "message": e.Error(),
	}})
	return body
}

type guardedUsageRecord struct {
	ctx    context.Context
	record Record
}

// ResponseModelGuard retains raw model observations and usage until the response is validated.
// Protocol conversion and client-visible model rewriting cannot change its decision.
type ResponseModelGuard struct {
	Provider string
	Model    string
	mu       sync.Mutex
	mismatch *ResponseModelValidationError
	finished bool
	outcome  error
	pending  []guardedUsageRecord
}

func WithResponseModelGuard(ctx context.Context, provider, model string) (context.Context, *ResponseModelGuard) {
	guard := &ResponseModelGuard{Provider: provider, Model: model}
	return context.WithValue(ctx, responseModelGuardKey{}, guard), guard
}

// ResponseModelGuardFromContext isolates internal executions of other models from this guard.
func ResponseModelGuardFromContext(ctx context.Context, model string) *ResponseModelGuard {
	if ctx == nil {
		return nil
	}
	guard, _ := ctx.Value(responseModelGuardKey{}).(*ResponseModelGuard)
	if guard == nil || guardModelKey(guard.Model) != guardModelKey(model) {
		return nil
	}
	return guard
}

func guardModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(strings.TrimSpace(model)).ModelName))
}

// Observe accepts the provider-aware comparison computed from the raw upstream payload.
// Empty model observations are allowed. The first mismatch remains decisive.
func (g *ResponseModelGuard) Observe(model string, substituted bool) {
	if model == "" || !substituted {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mismatch == nil {
		g.mismatch = &ResponseModelValidationError{Provider: g.Provider, Model: g.Model, ResponseModel: model}
	}
}

// Finish resolves validation and flushes usage, including records published after channel closure.
func (g *ResponseModelGuard) Finish(err error) error {
	g.mu.Lock()
	if err == nil && g.mismatch != nil {
		err = g.mismatch
	}
	g.finished, g.outcome = true, err
	pending := g.pending
	g.pending = nil
	g.mu.Unlock()
	for _, item := range pending {
		g.publish(item.ctx, item.record, err)
	}
	return err
}

func (g *ResponseModelGuard) publishOrDefer(ctx context.Context, record Record) {
	g.mu.Lock()
	if !g.finished {
		g.pending = append(g.pending, guardedUsageRecord{ctx: ctx, record: record})
		g.mu.Unlock()
		return
	}
	outcome := g.outcome
	g.mu.Unlock()
	g.publish(ctx, record, outcome)
}

func (g *ResponseModelGuard) publish(ctx context.Context, record Record, outcome error) {
	if outcome != nil && !record.Failed {
		record.Failed = true
		record.Fail = Failure{Body: outcome.Error()}
		if status, ok := outcome.(interface{ StatusCode() int }); ok {
			record.Fail.StatusCode = status.StatusCode()
		}
		if mismatch, ok := outcome.(*ResponseModelValidationError); ok {
			record.ResponseModel = mismatch.ResponseModel
			record.Fail.Code = "response_model_mismatch"
			record.Fail.Body = string(mismatch.ResponseBody())
		}
	}
	DefaultManager().Publish(ctx, record)
}
