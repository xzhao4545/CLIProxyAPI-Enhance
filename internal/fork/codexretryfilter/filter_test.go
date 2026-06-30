package codexretryfilter

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestNormalizeDefaults(t *testing.T) {
	cfg := Normalize(config.CodexResponseRetryFilterConfig{})
	if cfg.Enabled {
		t.Fatal("enabled default = true, want false")
	}
	if len(cfg.Models) != 1 || cfg.Models[0] != "gpt-*" {
		t.Fatalf("models = %#v, want [gpt-*]", cfg.Models)
	}
	if got := cfg.ReasoningTokenLengths; len(got) != 3 || got[0] != 516 || got[1] != 1034 || got[2] != 1552 {
		t.Fatalf("reasoning lengths = %#v, want defaults", got)
	}
	if !cfg.InterceptStreaming || !cfg.InterceptNonStreaming {
		t.Fatalf("intercepts = %v/%v, want true/true", cfg.InterceptStreaming, cfg.InterceptNonStreaming)
	}
	if cfg.GuardRetryAttempts != 3 {
		t.Fatalf("guard retry attempts = %d, want 3", cfg.GuardRetryAttempts)
	}
}

func TestNormalizeAllowsExplicitZeroGuardRetries(t *testing.T) {
	zero := 0
	cfg := Normalize(config.CodexResponseRetryFilterConfig{GuardRetryAttempts: &zero})
	if cfg.GuardRetryAttempts != 0 {
		t.Fatalf("guard retry attempts = %d, want explicit 0", cfg.GuardRetryAttempts)
	}
}

func TestValidateRejectsEnabledWithNoInterceptModes(t *testing.T) {
	err := Validate(RuntimeConfig{
		Enabled:               true,
		Models:                []string{"gpt-*"},
		ReasoningTokenLengths: []int64{516},
		InterceptStreaming:    false,
		InterceptNonStreaming: false,
	})
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestEligibleModelPatternAndProtocol(t *testing.T) {
	cfg := RuntimeConfig{
		Enabled:               true,
		Models:                []string{"gpt-*"},
		ReasoningTokenLengths: []int64{516},
		InterceptStreaming:    true,
		InterceptNonStreaming: true,
	}
	if !Eligible(cfg, "openai-response", "gpt-5-codex") {
		t.Fatal("Eligible() = false, want true")
	}
	if Eligible(cfg, "openai", "gpt-5-codex") {
		t.Fatal("Eligible(non-response) = true, want false")
	}
	if Eligible(cfg, "openai-response", "claude-sonnet-4-5") {
		t.Fatal("Eligible(non-gpt) = true, want false")
	}
}

func TestExtractReasoningTokensPaths(t *testing.T) {
	cases := []string{
		`{"response":{"usage":{"output_tokens_details":{"reasoning_tokens":516}}}}`,
		`{"response":{"usage":{"completion_tokens_details":{"reasoning_tokens":1034}}}}`,
		`{"usage":{"output_tokens_details":{"reasoning_tokens":1552}}}`,
		`{"usage":{"completion_tokens_details":{"reasoning_tokens":516}}}`,
	}
	for _, tc := range cases {
		if got, ok := ExtractReasoningTokens([]byte(tc)); !ok || got <= 0 {
			t.Fatalf("ExtractReasoningTokens(%s) = %d/%v, want token", tc, got, ok)
		}
	}
	if got, ok := ExtractReasoningTokens([]byte(`{"usage":null}`)); ok || got != 0 {
		t.Fatalf("ExtractReasoningTokens(missing) = %d/%v, want 0/false", got, ok)
	}
}

func TestMatchCompletedEventAndAction(t *testing.T) {
	cfg := RuntimeConfig{ReasoningTokenLengths: []int64{516}}
	match, inspected := MatchCompletedEvent(cfg, []byte(`{"usage":{"output_tokens_details":{"reasoning_tokens":516}}}`))
	if !inspected || match == nil {
		t.Fatalf("MatchCompletedEvent() = %#v/%v, want match", match, inspected)
	}
	if action := DecideAction(match, true, 1); action != ActionInternalRetry {
		t.Fatalf("action = %q, want %q", action, ActionInternalRetry)
	}
	if action := DecideAction(match, true, 0); action != ActionConductorRetry {
		t.Fatalf("action = %q, want %q", action, ActionConductorRetry)
	}
	if action := DecideAction(match, false, 1); action != ActionObserveOnly {
		t.Fatalf("action = %q, want %q", action, ActionObserveOnly)
	}
	if action := DecideAction(nil, true, 1); action != ActionPass {
		t.Fatalf("action = %q, want %q", action, ActionPass)
	}
}
