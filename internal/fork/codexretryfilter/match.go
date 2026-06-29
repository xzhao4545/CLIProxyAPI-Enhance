package codexretryfilter

import (
	"strings"

	"github.com/tidwall/gjson"
)

const (
	ActionPass           = "pass"
	ActionObserveOnly    = "observe_only"
	ActionInternalRetry  = "internal_retry"
	ActionConductorRetry = "conductor_retry"
)

var reasoningTokenPaths = []string{
	"response.usage.output_tokens_details.reasoning_tokens",
	"response.usage.completion_tokens_details.reasoning_tokens",
	"usage.output_tokens_details.reasoning_tokens",
	"usage.completion_tokens_details.reasoning_tokens",
}

type Match struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
	MatchedLength   int64 `json:"matched_length"`
}

func Eligible(cfg RuntimeConfig, sourceFormat, model string) bool {
	if !cfg.Enabled {
		return false
	}
	if strings.TrimSpace(sourceFormat) != "openai-response" {
		return false
	}
	return modelMatches(cfg.Models, model)
}

func ExtractReasoningTokens(eventData []byte) (int64, bool) {
	for _, path := range reasoningTokenPaths {
		result := gjson.GetBytes(eventData, path)
		if result.Exists() && result.Type == gjson.Number {
			return result.Int(), true
		}
	}
	return 0, false
}

func MatchCompletedEvent(cfg RuntimeConfig, eventData []byte) (*Match, bool) {
	tokens, ok := ExtractReasoningTokens(eventData)
	if !ok {
		return nil, false
	}
	for _, length := range cfg.ReasoningTokenLengths {
		if tokens == length {
			return &Match{ReasoningTokens: tokens, MatchedLength: length}, true
		}
	}
	return nil, true
}

func DecideAction(match *Match, intercept bool, remainingRetries int) string {
	if match == nil {
		return ActionPass
	}
	if !intercept {
		return ActionObserveOnly
	}
	if remainingRetries > 0 {
		return ActionInternalRetry
	}
	return ActionConductorRetry
}

type RetryError struct {
	match Match
}

func NewRetryError(match Match) RetryError {
	return RetryError{match: match}
}

func (e RetryError) Error() string {
	return "codex_response_retry_filtered"
}

func (e RetryError) StatusCode() int {
	return 429
}

func (e RetryError) Match() Match {
	return e.match
}
