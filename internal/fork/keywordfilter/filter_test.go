package keywordfilter

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestCheckPayloadExtractsLongMatchExcerptAroundKeyword(t *testing.T) {
	longPrefix := strings.Repeat("a", maxMatchTextRunes+200)
	longSuffix := strings.Repeat("b", maxMatchTextRunes+200)
	payload := []byte(longPrefix + "quota exhausted for this account" + longSuffix)

	match := CheckPayload(payload, []config.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want match")
	}
	if !strings.Contains(match.Text, "quota exhausted for this account") {
		t.Fatalf("match text = %q, want keyword context", match.Text)
	}
	if len([]rune(match.Text)) > maxMatchTextRunes+6 {
		t.Fatalf("match text rune length = %d, want bounded excerpt", len([]rune(match.Text)))
	}
}

func TestCheckPayloadPreservesRuleOrder(t *testing.T) {
	match := CheckPayload([]byte("first second"), []config.KeywordFilterRule{
		{Keyword: "second", Enabled: true},
		{Keyword: "first", Enabled: true},
	})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want match")
	}
	if match.RuleIndex != 0 || match.Keyword != "second" {
		t.Fatalf("match = %#v, want first enabled matching rule", match)
	}
}

func TestCheckPayloadCaseInsensitiveMatchKeepsOriginalText(t *testing.T) {
	match := CheckPayload([]byte(`{"choices":[{"delta":{"content":"Quota Exhausted for this account"}}]}`), []config.KeywordFilterRule{{
		Keyword: "quota exhausted",
		Enabled: true,
	}})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want match")
	}
	if !strings.Contains(match.Text, "Quota Exhausted for this account") {
		t.Fatalf("match text = %q, want original casing", match.Text)
	}
}

func TestCheckPayloadStartModeExtractsSSEDataText(t *testing.T) {
	match := CheckPayload([]byte(`data: {"choices":[{"delta":{"content":"quota exhausted for this account"}}]}`), []config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want start match inside SSE data payload")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestStreamCheckerStartModeMatchesSplitSSEText(t *testing.T) {
	checker := NewStreamChecker([]config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	if match := checker.CheckPayload([]byte(`data: {"choices":[{"delta":{"content":"quota "}}]}`)); match != nil {
		t.Fatalf("first partial match = %#v, want nil", match)
	}
	match := checker.CheckPayload([]byte(`data: {"choices":[{"delta":{"content":"exhausted for this account"}}]}`))
	if match == nil {
		t.Fatal("second partial match = nil, want split start match")
	}
	if !strings.Contains(match.Text, "quota exhausted for this account") {
		t.Fatalf("match text = %q, want accumulated response text", match.Text)
	}
}

func TestCheckPayloadExtractsOpenAIResponsesTextEvents(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "output text delta",
			payload: `{"type":"response.output_text.delta","delta":"quota exhausted for this account"}`,
			want:    "quota exhausted for this account",
		},
		{
			name:    "output text done",
			payload: `{"type":"response.output_text.done","text":"quota exhausted for this account"}`,
			want:    "quota exhausted for this account",
		},
		{
			name:    "content part done",
			payload: `{"type":"response.content_part.done","part":{"type":"output_text","text":"quota exhausted for this account"}}`,
			want:    "quota exhausted for this account",
		},
		{
			name:    "output item done",
			payload: `{"type":"response.output_item.done","item":{"content":[{"type":"output_text","text":"quota exhausted for this account"}]}}`,
			want:    "quota exhausted for this account",
		},
		{
			name:    "response completed",
			payload: `{"type":"response.completed","response":{"output":[{"content":[{"type":"output_text","text":"quota exhausted for this account"}]}]}}`,
			want:    "quota exhausted for this account",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := CheckPayload([]byte(tt.payload), []config.KeywordFilterRule{{
				Keyword:   "quota exhausted",
				MatchMode: "start",
				Enabled:   true,
			}})
			if match == nil {
				t.Fatal("CheckPayload() = nil, want start match in OpenAI Responses event")
			}
			if match.Text != tt.want {
				t.Fatalf("match text = %q, want %q", match.Text, tt.want)
			}
		})
	}
}

func TestStreamCheckerIgnoresSSEControlLines(t *testing.T) {
	checker := NewStreamChecker([]config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	if match := checker.CheckPayload([]byte("event: response.output_text.delta")); match != nil {
		t.Fatalf("control line match = %#v, want nil", match)
	}
	match := checker.CheckPayload([]byte(`data: {"type":"response.output_text.delta","delta":"quota exhausted for this account"}`))
	if match == nil {
		t.Fatal("data line match = nil, want start match after ignored control line")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestStreamCheckerStartModeIgnoresOpenAIResponsesMetadata(t *testing.T) {
	checker := NewStreamChecker([]config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	metadataPayloads := [][]byte{
		[]byte(`data: {"type":"response.created","response":{"output":[]}}`),
		[]byte(`data: {"type":"response.output_item.added","item":{"content":[]}}`),
		[]byte(`data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}`),
	}
	for _, payload := range metadataPayloads {
		if match := checker.CheckPayload(payload); match != nil {
			t.Fatalf("metadata payload match = %#v, want nil", match)
		}
	}
	match := checker.CheckPayload([]byte(`data: {"type":"response.output_text.delta","delta":"quota exhausted for this account"}`))
	if match == nil {
		t.Fatal("data line match = nil, want start match after ignored metadata")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestCheckPayloadIgnoresOpenAIResponsesMetadataRawJSON(t *testing.T) {
	match := CheckPayload([]byte(`{"type":"response.created","response":{"output":[]}}`), []config.KeywordFilterRule{{
		Keyword: "response.created",
		Enabled: true,
	}})
	if match != nil {
		t.Fatalf("metadata match = %#v, want nil", match)
	}
}

func TestStreamCheckerStartModeIgnoresAnthropicMetadata(t *testing.T) {
	checker := NewStreamChecker([]config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	metadataPayloads := [][]byte{
		[]byte(`event: message_start
data: {"type":"message_start","message":{"content":[]}}

`),
		[]byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`),
		[]byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":null}}

`),
	}
	for _, payload := range metadataPayloads {
		if match := checker.CheckPayload(payload); match != nil {
			t.Fatalf("metadata payload match = %#v, want nil", match)
		}
	}
	match := checker.CheckPayload([]byte(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"quota exhausted for this account"}}

`))
	if match == nil {
		t.Fatal("data line match = nil, want start match after ignored metadata")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestCheckPayloadIgnoresAnthropicMetadataRawJSON(t *testing.T) {
	match := CheckPayload([]byte(`{"type":"message_start","message":{"content":[]}}`), []config.KeywordFilterRule{{
		Keyword: "message_start",
		Enabled: true,
	}})
	if match != nil {
		t.Fatalf("metadata match = %#v, want nil", match)
	}
}

func TestCheckPayloadMatchesAnthropicContentBlockStartText(t *testing.T) {
	match := CheckPayload([]byte(`{"type":"content_block_start","content_block":{"type":"text","text":"quota exhausted for this account"}}`), []config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want Anthropic content block start match")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestStreamCheckerStartModeIgnoresGeminiMetadata(t *testing.T) {
	checker := NewStreamChecker([]config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	metadataPayloads := [][]byte{
		[]byte(`{"candidates":[{"content":{"role":"model","parts":[]}}],"usageMetadata":{"trafficType":"PROVISIONED_THROUGHPUT"}}`),
		[]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]}}],"usageMetadata":{"promptTokenCount":1}}`),
	}
	for _, payload := range metadataPayloads {
		if match := checker.CheckPayload(payload); match != nil {
			t.Fatalf("metadata payload match = %#v, want nil", match)
		}
	}
	match := checker.CheckPayload([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"quota exhausted for this account"}]}}]}`))
	if match == nil {
		t.Fatal("data payload match = nil, want start match after ignored Gemini metadata")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestCheckPayloadIgnoresGeminiMetadataRawJSON(t *testing.T) {
	match := CheckPayload([]byte(`{"candidates":[{"content":{"role":"model","parts":[]}}],"usageMetadata":{"promptTokenCount":0}}`), []config.KeywordFilterRule{{
		Keyword: "candidates",
		Enabled: true,
	}})
	if match != nil {
		t.Fatalf("metadata match = %#v, want nil", match)
	}
}

func TestCheckPayloadExtractsNestedGeminiResponseCandidates(t *testing.T) {
	match := CheckPayload([]byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"quota exhausted for this account"}]}}]}}`), []config.KeywordFilterRule{{
		Keyword:   "quota exhausted",
		MatchMode: "start",
		Enabled:   true,
	}})
	if match == nil {
		t.Fatal("CheckPayload() = nil, want nested Gemini response match")
	}
	if match.Text != "quota exhausted for this account" {
		t.Fatalf("match text = %q, want extracted response text", match.Text)
	}
}

func TestCheckPayloadIgnoresSSEDoneFrame(t *testing.T) {
	match := CheckPayload([]byte("data: [DONE]"), []config.KeywordFilterRule{{
		Keyword:   "DONE",
		MatchMode: "anywhere",
		Enabled:   true,
	}})
	if match != nil {
		t.Fatalf("done frame match = %#v, want nil", match)
	}
}
