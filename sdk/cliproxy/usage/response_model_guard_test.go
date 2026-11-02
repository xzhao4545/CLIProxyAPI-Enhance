package usage

import (
	"context"
	"errors"
	"testing"
	"time"
)

type modelGuardCapture struct {
	provider string
	records  chan Record
}

func (p *modelGuardCapture) HandleUsage(_ context.Context, record Record) {
	if record.Provider == p.provider {
		p.records <- record
	}
}

func TestResponseModelGuardReclassifiesUsageBeforeAndAfterFinish(t *testing.T) {
	provider := t.Name()
	capture := &modelGuardCapture{provider: provider, records: make(chan Record, 4)}
	RegisterNamedPlugin(provider, capture)
	t.Cleanup(func() { RegisterNamedPlugin(provider, nil) })
	ctx, guard := WithResponseModelGuard(context.Background(), provider, "gpt-5.4(high)")
	record := Record{Provider: provider, Model: "gpt-5.4", Alias: "client-alias", Detail: Detail{InputTokens: 12, OutputTokens: 4}}
	PublishRecord(ctx, record)
	guard.mu.Lock()
	if len(guard.pending) != 1 {
		t.Error("usage was not deferred until validation")
	}
	guard.mu.Unlock()
	guard.Observe("gpt-5.4-mini", true)
	guard.Observe("gpt-5.4", false)
	var mismatch *ResponseModelValidationError
	if !errors.As(guard.Finish(nil), &mismatch) {
		t.Fatal("matching later model erased the first mismatch")
	}
	PublishRecord(ctx, record)
	for range 2 {
		select {
		case got := <-capture.records:
			if !got.Failed || got.Fail.StatusCode != 502 || got.Fail.Code != "response_model_mismatch" || got.ResponseModel != "gpt-5.4-mini" {
				t.Fatalf("mismatch usage recorded incorrectly: %+v", got)
			}
			if got.Alias != record.Alias || got.Model != record.Model || got.Detail.InputTokens != 12 || got.Detail.OutputTokens != 4 {
				t.Fatal("validation changed request identity or token accounting")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("validated usage was not published")
		}
	}
}
