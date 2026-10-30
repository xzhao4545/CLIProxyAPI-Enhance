package usage

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestRecorderKeepsUpstreamStatusAfterDownstreamReplacement(t *testing.T) {
	ctx := logging.WithResponseStatusHolder(context.Background())
	logging.SetResponseStatus(ctx, 502)
	event := buildEvent(ctx, coreusage.Record{
		Provider: "claude", Failed: true,
		Fail: coreusage.Failure{StatusCode: 429, Body: "rate limited"},
	}, Config{})
	store := openTestStore(t)
	defer closeTestStore(t, store)
	mustInsertEvent(t, store, event)
	page, err := store.QueryEvents(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].HTTPStatus != 429 || page.Events[0].UpstreamStatus != 429 {
		t.Fatalf("recorded errors = %+v, want real upstream HTTP status 429", page.Events)
	}
}

func TestRecorderCacheHitRateUsesTotalInput(t *testing.T) {
	for _, tc := range []struct {
		name, provider                 string
		input, read, write, wantPrompt int64
	}{
		{"claude cache read and creation", "claude", 100, 900, 100, 1100},
		{"claude fully cached", "claude", 0, 900, 0, 900},
		{"claude cache creation only", "claude", 100, 0, 900, 1000},
		{"codex cache is a subset", "codex", 1000, 900, 0, 1000},
		{"gemini cache is a subset", "gemini", 1000, 900, 0, 1000},
		{"unknown provider keeps input", "custom", 1000, 900, 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openTestStore(t)
			defer closeTestStore(t, store)
			base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
			event := buildEvent(context.Background(), coreusage.Record{
				Provider: tc.provider, RequestedAt: base.Add(30 * time.Minute),
				Detail: coreusage.Detail{
					InputTokens: tc.input, OutputTokens: 10, CachedTokens: tc.read,
					CacheReadTokens: tc.read, CacheCreationTokens: tc.write,
				},
			}, Config{})
			if event.PromptTokens != tc.wantPrompt {
				t.Fatalf("prompt tokens = %d, want %d", event.PromptTokens, tc.wantPrompt)
			}
			if event.TotalTokens != tc.wantPrompt+10 {
				t.Fatalf("total tokens = %d, want %d", event.TotalTokens, tc.wantPrompt+10)
			}
			mustInsertEvent(t, store, event)
			for _, filter := range []QueryFilter{
				{RawProvider: tc.provider, DateFrom: ptrTime(base), DateTo: ptrTime(base.Add(time.Hour))},
				{DateFrom: ptrTime(base), DateTo: ptrTime(base.Add(time.Hour))},
				{DateFrom: ptrTime(base.Add(-15 * time.Minute)), DateTo: ptrTime(base.Add(time.Hour + 15*time.Minute))},
			} {
				metrics, err := store.QueryMetrics(filter)
				if err != nil {
					t.Fatal(err)
				}
				wantRate := float64(tc.read) / float64(tc.wantPrompt)
				if metrics.TotalPromptTokens != tc.wantPrompt || metrics.CacheHitRate != wantRate {
					t.Fatalf("metrics prompt=%d rate=%v, want %d/%v", metrics.TotalPromptTokens, metrics.CacheHitRate, tc.wantPrompt, wantRate)
				}
				for _, rows := range [][]ProviderMetric{metrics.ProviderRequestTotals, metrics.ProviderTokenTotals, metrics.ProviderSuccessRates} {
					provider := requireProviderMetric(t, rows, tc.provider)
					if provider.PromptTokens != tc.wantPrompt || provider.CacheHitRate != wantRate {
						t.Fatalf("provider prompt=%d rate=%v, want %d/%v", provider.PromptTokens, provider.CacheHitRate, tc.wantPrompt, wantRate)
					}
				}
			}
		})
	}
}

func TestBuildEventUsesProviderSuppliedTokenBreakdown(t *testing.T) {
	event := buildEvent(context.Background(), coreusage.Record{
		Provider: "custom",
		Detail: coreusage.Detail{
			InputTokens: 100, OutputTokens: 10, CacheReadTokens: 900, CacheCreationTokens: 100,
			TokenBreakdown: coreusage.NewIndependentTokenBreakdown(100, 900, 100, 10, 0, 1110),
		},
	}, Config{})
	if event.PromptTokens != 1100 {
		t.Fatalf("prompt tokens = %d, want 1100", event.PromptTokens)
	}
}
