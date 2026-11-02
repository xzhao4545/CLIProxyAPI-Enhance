package helps

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestResponseModelGuardUsesExistingLooseMatching(t *testing.T) {
	for _, tc := range []struct {
		requested, served string
		blocked           bool
	}{
		{"gpt-5.4(high)", "gpt-5.4", false},
		{"gpt-5.4(128000)", "gpt-5.4-2026-05-13", false},
		{"claude-sonnet-4-latest", "claude-sonnet-4-20260513", false},
		{"gemini-1.5-pro", "gemini-1.5-pro-002", false},
		{"openai/gpt-5.4", "GPT-5.4", false},
		{"gpt-5.4", "", false},
		{"gpt-5.4", "gpt-5.4-mini", true},
	} {
		ctx, guard := usage.WithResponseModelGuard(context.Background(), "openai", tc.requested)
		reporter := NewUsageReporter(ctx, "openai", tc.requested, nil)
		reporter.SetResponseModel(tc.served)
		var mismatch *usage.ResponseModelValidationError
		if blocked := errors.As(guard.Finish(nil), &mismatch); blocked != tc.blocked {
			t.Fatalf("%s -> %s: blocked=%v, want %v", tc.requested, tc.served, blocked, tc.blocked)
		}
	}
}

func TestResponseModelGuardObservesModelsAfterTerminalFrame(t *testing.T) {
	ctx, guard := usage.WithResponseModelGuard(context.Background(), "openai", "gpt-5.4")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	observer := NewStreamResponseModelObserver(reporter)
	observer.Feed([]byte("data: {\"model\":\"gpt-5.4\",\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
	observer.Feed([]byte("data: {\"mo"))
	observer.Feed([]byte("del\":\"gpt-5.4-mini\",\"usage\":{}}\n\n"))
	observer.Finish()
	var mismatch *usage.ResponseModelValidationError
	if !errors.As(guard.Finish(nil), &mismatch) || mismatch.ResponseModel != "gpt-5.4-mini" {
		t.Fatal("model mismatch after a finish frame escaped validation")
	}
}

func TestResponseModelGuardDoesNotValidateInternalOtherModel(t *testing.T) {
	ctx, guard := usage.WithResponseModelGuard(context.Background(), "codex", "gpt-5.4")
	reporter := NewUsageReporter(ctx, "codex", "gpt-5.4-mini", nil)
	reporter.SetResponseModel("gpt-5.4-nano")
	if err := guard.Finish(nil); err != nil {
		t.Fatalf("internal execution of another model affected the outer response: %v", err)
	}
}
