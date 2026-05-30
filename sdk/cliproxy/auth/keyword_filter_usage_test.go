package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type keywordFilterUsageExecutor struct {
	publishBeforeKeyword bool
	firstPayload         []byte
}

func (e *keywordFilterUsageExecutor) Identifier() string { return "kw" }

func (e *keywordFilterUsageExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{HTTPStatus: http.StatusNotImplemented, Message: "Execute not implemented"}
}

func (e *keywordFilterUsageExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	chunks := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(chunks)
		firstPayload := e.firstPayload
		if len(firstPayload) == 0 {
			firstPayload = []byte(`data: {"choices":[{"delta":{"content":"hello"}}]}`)
		}
		chunks <- cliproxyexecutor.StreamChunk{Payload: firstPayload}
		if e.publishBeforeKeyword {
			coreusage.PublishRecord(ctx, coreusage.Record{
				Provider: "kw",
				Model:    req.Model,
				AuthID:   auth.ID,
			})
		}
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`data: {"choices":[{"delta":{"content":"quota exhausted for this account"}}]}`)}
		if !e.publishBeforeKeyword {
			coreusage.PublishRecord(ctx, coreusage.Record{
				Provider: "kw",
				Model:    req.Model,
				AuthID:   auth.ID,
			})
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: http.Header{}, Chunks: chunks}, nil
}

func (e *keywordFilterUsageExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *keywordFilterUsageExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{HTTPStatus: http.StatusNotImplemented, Message: "CountTokens not implemented"}
}

func (e *keywordFilterUsageExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "HttpRequest not implemented"}
}

type captureUsagePlugin struct {
	authID  string
	records chan coreusage.Record
}

func (p *captureUsagePlugin) HandleUsage(_ context.Context, record coreusage.Record) {
	if p == nil || record.AuthID != p.authID {
		return
	}
	p.records <- record
}

func TestKeywordFilterBootstrapMatchCoolsDownAndReturns429(t *testing.T) {
	authID := "kw-auth-" + t.Name()

	m := NewManager(nil, nil, nil)
	m.SetConfig(&internalconfig.Config{KeywordFilters: []internalconfig.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}}})
	m.RegisterExecutor(&keywordFilterUsageExecutor{
		firstPayload: []byte(`data: {"choices":[{"delta":{"content":"quota exhausted for this account"}}]}`),
	})
	auth := &Auth{ID: authID, Provider: "kw", Status: StatusActive}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authID, "kw", []*registry.ModelInfo{{ID: "kw-model"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })

	streamResult, err := m.ExecuteStream(context.Background(), []string{"kw"}, cliproxyexecutor.Request{Model: "kw-model"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	if streamResult == nil {
		t.Fatal("ExecuteStream() streamResult = nil, want bootstrap error stream")
	}
	chunk := <-streamResult.Chunks
	if chunk.Err == nil {
		t.Fatal("bootstrap chunk error = nil, want keyword filter error")
	}
	if status := statusCodeFromError(chunk.Err); status != http.StatusTooManyRequests {
		t.Fatalf("bootstrap keyword status = %d, want %d", status, http.StatusTooManyRequests)
	}
	if got := chunk.Err.Error(); !strings.Contains(got, "quota exhausted") || !strings.Contains(got, "quota exhausted for this account") {
		t.Fatalf("bootstrap keyword error = %q, want keyword and matched text", got)
	}
	if _, ok := <-streamResult.Chunks; ok {
		t.Fatal("bootstrap error stream produced extra chunks")
	}
	assertKeywordFilterCooldown(t, m, authID, "kw-model")
}

func TestKeywordFilterMarksUsageRecordAsFailure(t *testing.T) {
	authID := "kw-auth-" + t.Name()
	plugin := &captureUsagePlugin{authID: authID, records: make(chan coreusage.Record, 1)}
	coreusage.RegisterPlugin(plugin)

	m := NewManager(nil, nil, nil)
	m.SetConfig(&internalconfig.Config{KeywordFilters: []internalconfig.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}}})
	m.RegisterExecutor(&keywordFilterUsageExecutor{})
	auth := &Auth{ID: authID, Provider: "kw", Status: StatusActive}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authID, "kw", []*registry.ModelInfo{{ID: "kw-model"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })

	streamResult, err := m.ExecuteStream(context.Background(), []string{"kw"}, cliproxyexecutor.Request{Model: "kw-model"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	first := <-streamResult.Chunks
	if first.Err != nil {
		t.Fatalf("first chunk error = %v", first.Err)
	}
	second := <-streamResult.Chunks
	if second.Err == nil {
		t.Fatal("expected keyword filter error")
	}
	if status := statusCodeFromError(second.Err); status != http.StatusTooManyRequests {
		t.Fatalf("keyword filter status = %d, want %d", status, http.StatusTooManyRequests)
	}
	if got := second.Err.Error(); !strings.Contains(got, "quota exhausted") || !strings.Contains(got, "quota exhausted for this account") {
		t.Fatalf("keyword error = %q, want keyword and matched text", got)
	}
	assertKeywordFilterCooldown(t, m, authID, "kw-model")

	select {
	case record := <-plugin.records:
		if !record.Failed {
			t.Fatalf("usage record Failed = false, want true: %#v", record)
		}
		if record.Fail.Code != "keyword_filtered" {
			t.Fatalf("usage failure code = %q, want keyword_filtered", record.Fail.Code)
		}
		if !strings.Contains(record.Fail.Body, "quota exhausted") || !strings.Contains(record.Fail.Body, "quota exhausted for this account") {
			t.Fatalf("usage failure body = %q, want keyword and matched text", record.Fail.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for usage record")
	}
}

func TestKeywordFilterReclassifiesEarlyUsageRecordAsFailure(t *testing.T) {
	authID := "kw-auth-" + t.Name()
	plugin := &captureUsagePlugin{authID: authID, records: make(chan coreusage.Record, 1)}
	coreusage.RegisterPlugin(plugin)

	m := NewManager(nil, nil, nil)
	m.SetConfig(&internalconfig.Config{KeywordFilters: []internalconfig.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}}})
	m.RegisterExecutor(&keywordFilterUsageExecutor{publishBeforeKeyword: true})
	auth := &Auth{ID: authID, Provider: "kw", Status: StatusActive}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authID, "kw", []*registry.ModelInfo{{ID: "kw-model"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })

	streamResult, err := m.ExecuteStream(context.Background(), []string{"kw"}, cliproxyexecutor.Request{Model: "kw-model"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	<-streamResult.Chunks
	second := <-streamResult.Chunks
	if second.Err == nil {
		t.Fatal("expected keyword filter error")
	}
	assertKeywordFilterCooldown(t, m, authID, "kw-model")

	select {
	case record := <-plugin.records:
		if !record.Failed {
			t.Fatalf("early usage record Failed = false, want true: %#v", record)
		}
		if record.Fail.Code != "keyword_filtered" {
			t.Fatalf("usage failure code = %q, want keyword_filtered", record.Fail.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for usage record")
	}
}

func assertKeywordFilterCooldown(t *testing.T, m *Manager, authID, model string) {
	t.Helper()
	updated, ok := m.GetByID(authID)
	if !ok {
		t.Fatalf("auth %q not found", authID)
	}
	state := updated.ModelStates[model]
	if state == nil {
		t.Fatalf("model state for %q is nil", model)
	}
	if !state.Unavailable {
		t.Fatalf("model state Unavailable = false, want true")
	}
	if state.LastError == nil || state.LastError.Code != "keyword_filtered" {
		t.Fatalf("model LastError = %#v, want keyword_filtered", state.LastError)
	}
	if state.LastError.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("model LastError status = %d, want %d", state.LastError.HTTPStatus, http.StatusTooManyRequests)
	}
	if state.NextRetryAfter.IsZero() {
		t.Fatalf("model NextRetryAfter is zero, want keyword filter cooldown")
	}
	if !state.Quota.Exceeded {
		t.Fatalf("model quota Exceeded = false, want true")
	}
}
