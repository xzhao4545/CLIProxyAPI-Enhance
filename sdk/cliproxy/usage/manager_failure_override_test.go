package usage

import (
	"context"
	"sync"
	"testing"
	"time"
)

type capturePlugin struct {
	mu      sync.Mutex
	records []Record
}

func (p *capturePlugin) HandleUsage(_ context.Context, record Record) {
	p.mu.Lock()
	p.records = append(p.records, record)
	p.mu.Unlock()
}

func (p *capturePlugin) snapshot() []Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Record, len(p.records))
	copy(out, p.records)
	return out
}

func TestFailureOverrideDefersUntilFlush(t *testing.T) {
	mgr := NewManager(8)
	plugin := &capturePlugin{}
	mgr.Register(plugin)
	mgr.Start(context.Background())
	t.Cleanup(mgr.Stop)

	ctx := WithFreshFailureOverride(context.Background())
	mgr.Publish(ctx, Record{Provider: "codex", Model: "gpt-5.5", Failed: false})

	// Still deferred: no plugin delivery yet.
	time.Sleep(30 * time.Millisecond)
	if got := plugin.snapshot(); len(got) != 0 {
		t.Fatalf("records before flush = %d, want 0", len(got))
	}

	MarkFailureOverride(ctx, Failure{StatusCode: 408, Stage: "upstream_request", Body: "stream closed before response.completed"})
	FlushFailureOverrideRecords(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		got := plugin.snapshot()
		if len(got) == 1 {
			if !got[0].Failed {
				t.Fatalf("record not marked failed: %+v", got[0])
			}
			if got[0].Fail.StatusCode != 408 {
				t.Fatalf("fail status = %d, want 408", got[0].Fail.StatusCode)
			}
			if got[0].Fail.Body == "" {
				t.Fatalf("fail body empty")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("records after flush = %d, want 1", len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFreshFailureOverrideIsolatesAttempts(t *testing.T) {
	mgr := NewManager(8)
	plugin := &capturePlugin{}
	mgr.Register(plugin)
	mgr.Start(context.Background())
	t.Cleanup(mgr.Stop)

	first := WithFreshFailureOverride(context.Background())
	second := WithFreshFailureOverride(context.Background())

	mgr.Publish(first, Record{Provider: "codex", Model: "m1"})
	mgr.Publish(second, Record{Provider: "codex", Model: "m2"})

	MarkFailureOverride(first, Failure{StatusCode: 408, Body: "first failed"})
	FlushFailureOverrideRecords(first)

	deadline := time.Now().Add(2 * time.Second)
	for {
		got := plugin.snapshot()
		if len(got) == 1 {
			if got[0].Model != "m1" || !got[0].Failed {
				t.Fatalf("unexpected first flush record: %+v", got[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first flush records = %d, want 1", len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}

	// second attempt still deferred until its own flush
	if got := plugin.snapshot(); len(got) != 1 {
		t.Fatalf("records before second flush = %d, want 1", len(got))
	}
	FlushFailureOverrideRecords(second)
	deadline = time.Now().Add(2 * time.Second)
	for {
		got := plugin.snapshot()
		if len(got) == 2 {
			if got[1].Model != "m2" {
				t.Fatalf("second record model = %q, want m2", got[1].Model)
			}
			if got[1].Failed {
				t.Fatalf("second record unexpectedly failed: %+v", got[1])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("records after second flush = %d, want 2", len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
