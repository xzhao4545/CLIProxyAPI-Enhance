package helps

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestParseOpenAIUsageIgnoresNullUsage(t *testing.T) {
	data := []byte(`{"usage":null}`)
	detail := ParseOpenAIUsage(data)
	if detail != (usage.Detail{}) {
		t.Fatalf("detail = %+v, want zero detail", detail)
	}
}

func TestParseOpenAIStreamUsageIgnoresNullUsage(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}],"usage":null}`)
	if detail, ok := ParseOpenAIStreamUsage(line); ok {
		t.Fatalf("ParseOpenAIStreamUsage() = (%+v, true), want false for null usage", detail)
	}
}

func TestParseOpenAIStreamUsageResponsesFields(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","choices":[],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`)
	detail, ok := ParseOpenAIStreamUsage(line)
	if !ok {
		t.Fatal("ParseOpenAIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 8 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 8)
	}
	if detail.OutputTokens != 5 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 5)
	}
	if detail.TotalTokens != 13 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 13)
	}
	if detail.CachedTokens != 3 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 3)
	}
	if detail.ReasoningTokens != 2 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 2)
	}
}

func TestParseGeminiCLIUsage_TopLevelUsageMetadata(t *testing.T) {
	data := []byte(`{"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7,"thoughtsTokenCount":3,"totalTokenCount":21,"cachedContentTokenCount":5}}`)
	detail := ParseGeminiCLIUsage(data)
	if detail.InputTokens != 11 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 11)
	}
	if detail.OutputTokens != 7 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 7)
	}
	if detail.ReasoningTokens != 3 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 3)
	}
	if detail.TotalTokens != 21 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 21)
	}
	if detail.CachedTokens != 5 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 5)
	}
}

func TestParseGeminiCLIStreamUsage_ResponseSnakeCaseUsageMetadata(t *testing.T) {
	line := []byte(`data: {"response":{"usage_metadata":{"promptTokenCount":13,"candidatesTokenCount":2,"totalTokenCount":15}}}`)
	detail, ok := ParseGeminiCLIStreamUsage(line)
	if !ok {
		t.Fatal("ParseGeminiCLIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 13 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 13)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 15)
	}
}

func TestParseGeminiCLIStreamUsage_IgnoresTrafficTypeOnlyUsageMetadata(t *testing.T) {
	line := []byte(`data: {"response":{"usageMetadata":{"trafficType":"ON_DEMAND"}}}`)
	if detail, ok := ParseGeminiCLIStreamUsage(line); ok {
		t.Fatalf("ParseGeminiCLIStreamUsage() = (%+v, true), want false for traffic-only usage metadata", detail)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}

func TestUsageReporterTrackHTTPClientStartsTTFTBeforeRoundTrip(t *testing.T) {
	delay := 40 * time.Millisecond
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	client := reporter.TrackHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			time.Sleep(delay)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		}),
	})

	req, errNewRequest := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader("{}"))
	if errNewRequest != nil {
		t.Fatalf("NewRequestWithContext() error = %v", errNewRequest)
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		t.Fatalf("Do() error = %v", errDo)
	}
	if _, errRead := io.ReadAll(resp.Body); errRead != nil {
		t.Fatalf("ReadAll() error = %v", errRead)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("response body close error = %v", errClose)
	}
	if got := reporter.ttftDuration(); got < delay {
		t.Fatalf("ttft = %v, want >= %v", got, delay)
	}
}

func TestUsageReporterBuildRecordIncludesRequestedModelAlias(t *testing.T) {
	ctx := usage.WithRequestedModelAlias(context.Background(), "client-gpt")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want %q", record.Model, "gpt-5.4")
	}
	if record.Alias != "client-gpt" {
		t.Fatalf("alias = %q, want %q", record.Alias, "client-gpt")
	}
}

func TestUsageReporterBuildRecordIncludesReasoningEffort(t *testing.T) {
	ctx := usage.WithReasoningEffort(context.Background(), "medium")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %q, want %q", record.ReasoningEffort, "medium")
	}
}

func TestUsageReporterSetTranslatedReasoningEffortPreservesContextValue(t *testing.T) {
	ctx := usage.WithReasoningEffort(context.Background(), "max")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.SetTranslatedReasoningEffort([]byte(`{"model":"gpt-5.4"}`), "openai")

	record := reporter.buildRecord(usage.Detail{TotalTokens: 1}, false)
	if record.ReasoningEffort != "max" {
		t.Fatalf("reasoning effort = %q, want %q (context value should be preserved when translated extraction is empty)", record.ReasoningEffort, "max")
	}
}

func TestUsageReporterSetTranslatedReasoningEffortOverridesWhenNonEmpty(t *testing.T) {
	ctx := usage.WithReasoningEffort(context.Background(), "low")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.SetTranslatedReasoningEffort([]byte(`{"reasoning_effort":"high"}`), "openai")

	record := reporter.buildRecord(usage.Detail{TotalTokens: 1}, false)
	if record.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want %q (translated value should override context)", record.ReasoningEffort, "high")
	}
}

func TestUsageReporterBuildAdditionalModelRecordSkipsZeroTokens(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "codex",
		model:       "gpt-5.4",
		requestedAt: time.Now(),
	}

	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{}); ok {
		t.Fatalf("expected all-zero token usage to be skipped")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{InputTokens: 2}); !ok {
		t.Fatalf("expected non-zero input token usage to be recorded")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{CachedTokens: 2}); !ok {
		t.Fatalf("expected non-zero cached token usage to be recorded")
	}
}

func TestUsageReporterSetStreamAndResponseModel(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	reporter.SetStream(true)
	reporter.SetResponseModel("gpt-5.4-real")

	record := reporter.buildRecord(usage.Detail{TotalTokens: 1}, false)
	if !record.Stream {
		t.Fatalf("stream = false, want true")
	}
	if record.ResponseModel != "gpt-5.4-real" {
		t.Fatalf("response model = %q, want %q", record.ResponseModel, "gpt-5.4-real")
	}
}

func TestUsageReporterSetResponseModelFirstNonEmptyWins(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	reporter.SetResponseModel("first-model")
	reporter.SetResponseModel("second-model")

	if got := reporter.responseModel(); got != "first-model" {
		t.Fatalf("response model = %q, want %q", got, "first-model")
	}
}

func TestUsageReporterSetResponseModelIgnoresEmpty(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	reporter.SetResponseModel("")

	if got := reporter.responseModel(); got != "" {
		t.Fatalf("response model = %q, want empty", got)
	}
}

func TestExtractOpenAIResponseModel(t *testing.T) {
	if got := ExtractOpenAIResponseModel([]byte(`{"model":"gpt-5.4","choices":[]}`)); got != "gpt-5.4" {
		t.Fatalf("got %q, want gpt-5.4", got)
	}
	if got := ExtractOpenAIResponseModel([]byte(`{"response":{"model":"gpt-5.5"}}`)); got != "gpt-5.5" {
		t.Fatalf("got %q, want gpt-5.5", got)
	}
	if got := ExtractOpenAIResponseModel([]byte(`{"choices":[]}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractOpenAIStreamResponseModel(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","model":"gpt-5.4","choices":[]}`)
	if got := ExtractOpenAIStreamResponseModel(line); got != "gpt-5.4" {
		t.Fatalf("got %q, want gpt-5.4", got)
	}
	lineResponse := []byte(`data: {"type":"response.completed","response":{"model":"gpt-5.5"}}`)
	if got := ExtractOpenAIStreamResponseModel(lineResponse); got != "gpt-5.5" {
		t.Fatalf("got %q, want gpt-5.5", got)
	}
	if got := ExtractOpenAIStreamResponseModel([]byte(`data: {"choices":[]}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractCodexResponseModel(t *testing.T) {
	data := []byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5-codex","usage":{"input_tokens":1}}}`)
	if got := ExtractCodexResponseModel(data); got != "gpt-5-codex" {
		t.Fatalf("got %q, want gpt-5-codex", got)
	}
	if got := ExtractCodexResponseModel([]byte(`{"type":"response.created"}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractClaudeResponseModel(t *testing.T) {
	data := []byte(`{"id":"msg_1","type":"message","model":"claude-sonnet-4-5","role":"assistant"}`)
	if got := ExtractClaudeResponseModel(data); got != "claude-sonnet-4-5" {
		t.Fatalf("got %q, want claude-sonnet-4-5", got)
	}
	if got := ExtractClaudeResponseModel([]byte(`{"id":"msg_1","type":"message"}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractClaudeStreamResponseModel(t *testing.T) {
	line := []byte(`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5"}}`)
	if got := ExtractClaudeStreamResponseModel(line); got != "claude-sonnet-4-5" {
		t.Fatalf("got %q, want claude-sonnet-4-5", got)
	}
	nonStart := []byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
	if got := ExtractClaudeStreamResponseModel(nonStart); got != "" {
		t.Fatalf("got %q, want empty for non-message_start", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
