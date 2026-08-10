package executor

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCodexExecutorRecordsTerminalResponseModelAcrossReadBoundaries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-stream", true: "stream"}[stream], func(t *testing.T) {
			largeEvent := `data: {"type":"response.output_text.delta","delta":"` + strings.Repeat("x", 9<<10) + `"}` + "\n\n"
			terminalStart := `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":0,"status":"completed","mo`
			terminalEnd := `del":"gpt-5.4-actual","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", responseModelRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(io.MultiReader(
						strings.NewReader(largeEvent),
						strings.NewReader(terminalStart),
						strings.NewReader(terminalEnd),
					)),
					Request: req,
				}, nil
			}))

			plugin := &captureResponseModelUsagePlugin{provider: "codex", model: "gpt-5.4", records: make(chan usage.Record, 1)}
			usage.RegisterPlugin(plugin)
			executor := NewCodexExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": "http://codex.test", "api_key": "test"}}
			req := cliproxyexecutor.Request{
				Model:   "gpt-5.4",
				Payload: []byte(`{"model":"gpt-5.4","input":"hi","stream":true}`),
			}
			opts := cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("openai-response"),
				Stream:       stream,
			}

			if stream {
				result, err := executor.ExecuteStream(ctx, auth, req, opts)
				if err != nil {
					t.Fatalf("ExecuteStream error: %v", err)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatalf("stream chunk error: %v", chunk.Err)
					}
				}
			} else {
				if _, err := executor.Execute(ctx, auth, req, opts); err != nil {
					t.Fatalf("Execute error: %v", err)
				}
			}

			record := waitForResponseModelUsageRecord(t, plugin.records)
			if record.ResponseModel != "gpt-5.4-actual" {
				t.Fatalf("response model = %q, want gpt-5.4-actual", record.ResponseModel)
			}
		})
	}
}

func TestCodexExecutorKeepsResponseModelFromCreatedEvent(t *testing.T) {
	created := `data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4-actual","status":"in_progress"}}` + "\n\n"
	completed := `data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", responseModelRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(io.MultiReader(strings.NewReader(created), strings.NewReader(completed))),
			Request:    req,
		}, nil
	}))

	plugin := &captureResponseModelUsagePlugin{provider: "codex", model: "gpt-5.4", records: make(chan usage.Record, 1)}
	usage.RegisterPlugin(plugin)
	executor := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": "http://codex.test", "api_key": "test"}}
	_, err := executor.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: []byte(`{"model":"gpt-5.4","input":"hi"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	record := waitForResponseModelUsageRecord(t, plugin.records)
	if record.ResponseModel != "gpt-5.4-actual" {
		t.Fatalf("response model = %q, want gpt-5.4-actual", record.ResponseModel)
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

type responseModelRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f responseModelRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
