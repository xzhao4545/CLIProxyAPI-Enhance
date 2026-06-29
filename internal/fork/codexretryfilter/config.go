package codexretryfilter

import (
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

var (
	defaultModels                = []string{"gpt-*"}
	defaultReasoningTokenLengths = []int64{516, 1034, 1552}
)

const defaultGuardRetryAttempts = 3

// RuntimeConfig is the normalized immutable view used by runtime code.
type RuntimeConfig struct {
	Enabled               bool     `json:"enabled"`
	Models                []string `json:"models"`
	ReasoningTokenLengths []int64  `json:"reasoning-token-lengths"`
	InterceptStreaming    bool     `json:"intercept-streaming"`
	InterceptNonStreaming bool     `json:"intercept-non-streaming"`
	GuardRetryAttempts    int      `json:"guard-retry-attempts"`
}

func Normalize(cfg config.CodexResponseRetryFilterConfig) RuntimeConfig {
	out := RuntimeConfig{
		Enabled:               cfg.Enabled,
		Models:                normalizeModels(cfg.Models),
		ReasoningTokenLengths: normalizeLengths(cfg.ReasoningTokenLengths),
		InterceptStreaming:    true,
		InterceptNonStreaming: true,
		GuardRetryAttempts:    defaultGuardRetryAttempts,
	}
	if cfg.InterceptStreaming != nil {
		out.InterceptStreaming = *cfg.InterceptStreaming
	}
	if cfg.InterceptNonStreaming != nil {
		out.InterceptNonStreaming = *cfg.InterceptNonStreaming
	}
	if cfg.GuardRetryAttempts != nil {
		out.GuardRetryAttempts = *cfg.GuardRetryAttempts
	}
	return out
}

func ToConfig(runtime RuntimeConfig) config.CodexResponseRetryFilterConfig {
	streaming := runtime.InterceptStreaming
	nonStreaming := runtime.InterceptNonStreaming
	guardRetryAttempts := runtime.GuardRetryAttempts
	return config.CodexResponseRetryFilterConfig{
		Enabled:               runtime.Enabled,
		Models:                append([]string(nil), runtime.Models...),
		ReasoningTokenLengths: append([]int64(nil), runtime.ReasoningTokenLengths...),
		InterceptStreaming:    &streaming,
		InterceptNonStreaming: &nonStreaming,
		GuardRetryAttempts:    &guardRetryAttempts,
	}
}

func Validate(runtime RuntimeConfig) error {
	runtime.Models = normalizeModels(runtime.Models)
	runtime.ReasoningTokenLengths = normalizeLengths(runtime.ReasoningTokenLengths)
	if runtime.Enabled && !runtime.InterceptStreaming && !runtime.InterceptNonStreaming {
		return fmt.Errorf("at least one intercept mode must be enabled")
	}
	if runtime.GuardRetryAttempts < 0 {
		return fmt.Errorf("guard-retry-attempts must be non-negative")
	}
	for _, model := range runtime.Models {
		if _, err := path.Match(model, "gpt-5"); err != nil {
			return fmt.Errorf("invalid model pattern %q: %w", model, err)
		}
	}
	for _, length := range runtime.ReasoningTokenLengths {
		if length <= 0 {
			return fmt.Errorf("reasoning-token-lengths must contain positive integers")
		}
	}
	return nil
}

func normalizeModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return append([]string(nil), defaultModels...)
	}
	return out
}

func normalizeLengths(lengths []int64) []int64 {
	seen := make(map[int64]struct{}, len(lengths))
	out := make([]int64, 0, len(lengths))
	for _, length := range lengths {
		if length <= 0 {
			continue
		}
		if _, ok := seen[length]; ok {
			continue
		}
		seen[length] = struct{}{}
		out = append(out, length)
	}
	if len(out) == 0 {
		return append([]int64(nil), defaultReasoningTokenLengths...)
	}
	return out
}

func modelMatches(patterns []string, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, pattern := range normalizeModels(patterns) {
		matched, err := path.Match(pattern, model)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func newRequestID() string {
	return uuid.NewString()
}
