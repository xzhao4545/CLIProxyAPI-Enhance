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
	publishAfterKeyword <-chan struct{}
}

func (e *keywordFilterUsageExecutor) Identifier() string { return "kw" }

func (e *keywordFilterUsageExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{HTTPStatus: http.StatusNotImplemented, Message: "Execute not implemented"}
}

func (e *keywordFilterUsageExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	chunks := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(chunks)
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`data: {"choices":[{"delta":{"content":"hello"}}]}`)}
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`data: {"choices":[{"delta":{"content":"quota exhausted for this account"}}]}`)}
		<-e.publishAfterKeyword
		coreusage.PublishRecord(ctx, coreusage.Record{
			Provider: "kw",
			Model:    req.Model,
			AuthID:   auth.ID,
		})
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

func TestKeywordFilterMarksUsageRecordAsFailure(t *testing.T) {
	authID := "kw-auth-" + t.Name()
	plugin := &captureUsagePlugin{authID: authID, records: make(chan coreusage.Record, 1)}
	coreusage.RegisterPlugin(plugin)

	publishAfterKeyword := make(chan struct{})
	m := NewManager(nil, nil, nil)
	m.SetConfig(&internalconfig.Config{KeywordFilters: []internalconfig.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}}})
	m.RegisterExecutor(&keywordFilterUsageExecutor{publishAfterKeyword: publishAfterKeyword})
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
	if got := second.Err.Error(); !strings.Contains(got, "quota exhausted") || !strings.Contains(got, "quota exhausted for this account") {
		t.Fatalf("keyword error = %q, want keyword and matched text", got)
	}

	close(publishAfterKeyword)

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
