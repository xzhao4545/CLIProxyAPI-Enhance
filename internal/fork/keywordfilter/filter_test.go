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
