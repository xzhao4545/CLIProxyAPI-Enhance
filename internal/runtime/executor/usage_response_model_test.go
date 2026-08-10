package executor

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

type captureResponseModelUsagePlugin struct {
	provider string
	model    string
	records  chan usage.Record
}

func (p *captureResponseModelUsagePlugin) HandleUsage(_ context.Context, record usage.Record) {
	if p == nil || record.Provider != p.provider || (p.model != "" && record.Model != p.model) {
		return
	}
	select {
	case p.records <- record:
	default:
	}
}

func TestOpenAICompatExecutorRecordsUpstreamResponseModel(t *testing.T) {
	const provider = "response-model-test"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"actual-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	plugin := &captureResponseModelUsagePlugin{provider: provider, records: make(chan usage.Record, 1)}
	usage.RegisterPlugin(plugin)
	executor := NewOpenAICompatExecutor(provider, &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL, "api_key": "test"}}

	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "requested-model",
		Payload: []byte(`{"model":"requested-model","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	record := waitForResponseModelUsageRecord(t, plugin.records)
	if record.Model != "requested-model" {
		t.Fatalf("model = %q, want requested-model", record.Model)
	}
	if record.ResponseModel != "actual-model" {
		t.Fatalf("response model = %q, want actual-model", record.ResponseModel)
	}
}

func TestClaudeExecutorRecordsResponseModelAfterGzipDecompression(t *testing.T) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	_, _ = gzipWriter.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-actual\"},\"usage\":{\"input_tokens\":1}}\n\n"))
	_, _ = gzipWriter.Write([]byte("data: {\"type\":\"message_stop\"}\n"))
	if errClose := gzipWriter.Close(); errClose != nil {
		t.Fatalf("close gzip writer: %v", errClose)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	plugin := &captureResponseModelUsagePlugin{provider: "claude", model: "requested-model", records: make(chan usage.Record, 1)}
	usage.RegisterPlugin(plugin)
	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": server.URL}}
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "requested-model",
		Payload: []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
	}

	record := waitForResponseModelUsageRecord(t, plugin.records)
	if record.ResponseModel != "claude-actual" {
		t.Fatalf("response model = %q, want claude-actual", record.ResponseModel)
	}
}

func waitForResponseModelUsageRecord(t *testing.T, records <-chan usage.Record) usage.Record {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for usage record")
		return usage.Record{}
	}
}
