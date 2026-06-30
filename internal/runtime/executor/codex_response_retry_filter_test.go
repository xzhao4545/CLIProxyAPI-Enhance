package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodexExecutorResponseRetryFilterNonStreamRetriesInternally(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = w.Write([]byte(codexCompletedSSE("blocked", 516)))
			return
		}
		_, _ = w.Write([]byte(codexCompletedSSE("ok", 42)))
	}))
	defer server.Close()

	cfg := codexRetryFilterTestConfig(3)
	executor := NewCodexExecutor(cfg)
	resp, err := executor.Execute(context.Background(), codexRetryFilterTestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","input":"hello"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if got := gjson.GetBytes(resp.Payload, "output.0.content.0.text").String(); got != "ok" {
		t.Fatalf("output text = %q, want ok; payload=%s", got, string(resp.Payload))
	}
}

func TestCodexExecutorResponseRetryFilterNonStreamBudgetExhausted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(codexCompletedSSE("blocked", 516)))
	}))
	defer server.Close()

	cfg := codexRetryFilterTestConfig(1)
	executor := NewCodexExecutor(cfg)
	_, err := executor.Execute(context.Background(), codexRetryFilterTestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","input":"hello"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
	if err == nil {
		t.Fatal("Execute() error = nil, want retry filter error")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestCodexExecutorResponseRetryFilterStreamBuffersAndRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		if call == 1 {
			_, _ = w.Write([]byte(codexCompletedSSE("blocked", 516)))
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		_, _ = w.Write([]byte(codexCompletedSSE("ok", 42)))
	}))
	defer server.Close()

	cfg := codexRetryFilterTestConfig(3)
	executor := NewCodexExecutor(cfg)
	result, err := executor.ExecuteStream(context.Background(), codexRetryFilterTestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","input":"hello"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response"), Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var got bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		got.Write(chunk.Payload)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if bytes.Contains(got.Bytes(), []byte("blocked")) {
		t.Fatalf("stream leaked filtered attempt: %s", got.String())
	}
	if !bytes.Contains(got.Bytes(), []byte("ok")) {
		t.Fatalf("stream missing retry output: %s", got.String())
	}
}

func TestCodexExecutorResponseRetryFilterIgnoresNonResponseProtocol(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(codexCompletedSSE("blocked", 516)))
	}))
	defer server.Close()

	cfg := codexRetryFilterTestConfig(3)
	executor := NewCodexExecutor(cfg)
	_, err := executor.Execute(context.Background(), codexRetryFilterTestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","messages":[{"role":"user","content":"hello"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil && err != io.EOF {
		t.Fatalf("Execute() error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestCodexExecutorResponseRetryFilterHTTPErrorDoesNotConsumeRuleRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "upstream quota", http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := codexRetryFilterTestConfig(3)
	executor := NewCodexExecutor(cfg)
	_, err := executor.Execute(context.Background(), codexRetryFilterTestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","input":"hello"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
	if err == nil {
		t.Fatal("Execute() error = nil, want upstream 429")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 because rule retry budget must not handle HTTP errors", calls.Load())
	}
}

func codexRetryFilterTestConfig(guardRetries int) *config.Config {
	streaming := true
	nonStreaming := true
	return &config.Config{CodexResponseRetryFilter: config.CodexResponseRetryFilterConfig{
		Enabled:               true,
		Models:                []string{"gpt-*"},
		ReasoningTokenLengths: []int64{516},
		InterceptStreaming:    &streaming,
		InterceptNonStreaming: &nonStreaming,
		GuardRetryAttempts:    &guardRetries,
	}}
}

func codexRetryFilterTestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{ID: "auth-1", Attributes: map[string]string{
		"base_url": baseURL,
		"api_key":  "test",
	}}
}

func codexCompletedSSE(text string, reasoningTokens int64) string {
	return fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":0,\"status\":\"completed\",\"model\":\"gpt-5-codex\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2,\"output_tokens_details\":{\"reasoning_tokens\":%d}}}}\n\n", text, reasoningTokens)
}
