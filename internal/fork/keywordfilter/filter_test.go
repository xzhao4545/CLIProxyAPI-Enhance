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
