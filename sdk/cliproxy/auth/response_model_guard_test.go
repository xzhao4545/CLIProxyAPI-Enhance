package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type modelValidationExecutor struct {
	ProviderExecutor
	execute func(context.Context, cliproxyexecutor.Options) (cliproxyexecutor.Response, error)
	stream  func(context.Context, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error)
}

func (*modelValidationExecutor) Identifier() string { return "codex" }
func (e *modelValidationExecutor) Execute(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.execute(ctx, opts)
}
func (e *modelValidationExecutor) ExecuteStream(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.stream(ctx, opts)
}

func modelValidationManager() *Manager {
	m := NewManager(nil, nil, nil)
	m.SetConfig(&config.Config{ResponseModelValidation: []config.ResponseModelValidationRule{{Provider: "codex", Models: []string{"gpt-5.4"}}}})
	return m
}

func TestResponseModelValidationScopeAndReload(t *testing.T) {
	m := modelValidationManager()
	for _, tc := range []struct {
		provider, model string
		want            bool
	}{
		{"codex", "gpt-5.4", true}, {"CODEX", "gpt-5.4(high)", true},
		{"claude", "gpt-5.4", false}, {"codex", "gpt-5.4-mini", false},
	} {
		if got := m.validatesResponseModel(tc.provider, tc.model, nil); got != tc.want {
			t.Fatalf("scope %s/%s = %v, want %v", tc.provider, tc.model, got, tc.want)
		}
	}
	m.SetConfig(&config.Config{ResponseModelValidation: []config.ResponseModelValidationRule{{Provider: "codex", Label: "upstream-a", Models: []string{"gpt-5.4"}}}})
	for _, tc := range []struct {
		provider string
		auth     *Auth
		want     bool
	}{
		{"codex", &Auth{Label: "upstream-a"}, true},
		{"codex", &Auth{Label: "upstream-b"}, false},
		{"codex", &Auth{Label: " upstream-a "}, true},
		{"codex", &Auth{Label: "Upstream-A"}, false},
		{"claude", &Auth{Label: "upstream-a"}, false},
		{"codex", nil, false},
	} {
		if got := m.validatesResponseModel(tc.provider, "gpt-5.4", tc.auth); got != tc.want {
			t.Fatalf("provider label scope %s/%+v = %v, want %v", tc.provider, tc.auth, got, tc.want)
		}
	}
	m.SetConfig(&config.Config{})
	if m.validatesResponseModel("codex", "gpt-5.4", &Auth{Label: "upstream-a"}) {
		t.Fatal("removed validation rule remained active")
	}
}

func TestResponseModelValidationBlocksPayloadAndWebsocketCallbacks(t *testing.T) {
	m := modelValidationManager()
	var observed int
	executor := &modelValidationExecutor{execute: func(ctx context.Context, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
		opts.WebSocketResponseObserver(ctx, cliproxyexecutor.WebSocketResponseEvent{Payload: []byte(`{"secret":"wrong-model-output"}`)})
		usage.ResponseModelGuardFromContext(ctx, "gpt-5.4").Observe("gpt-5.4-mini", true)
		return cliproxyexecutor.Response{Payload: []byte("wrong-model-output")}, nil
	}}
	response, errExecute := m.executeWithResponseModelValidation(context.Background(), executor, nil, cliproxyexecutor.Request{Model: "gpt-5.4"}, cliproxyexecutor.Options{
		WebSocketResponseObserver: func(context.Context, cliproxyexecutor.WebSocketResponseEvent) { observed++ },
	})
	var mismatch *usage.ResponseModelValidationError
	if !errors.As(errExecute, &mismatch) || mismatch.StatusCode() != http.StatusBadGateway || !isRequestInvalidError(errExecute) {
		t.Fatalf("expected validation error stopping retries, got %v", errExecute)
	}
	if len(response.Payload) != 0 || observed != 0 {
		t.Fatal("rejected response escaped the validation barrier")
	}
	if result := resultErrorFromError(errExecute); !result.IsRequestScoped() {
		t.Fatalf("validation failure would penalize credentials: %+v", result)
	}
	cfg := &config.Config{OAuthRequestScopedErrors: map[string][]config.RequestScopedErrorRule{
		"codex": {{Status: 502, Match: []string{"response_model_mismatch"}, Action: RequestScopedActionContinueAndCooldown}},
	}}
	if _, matched := matchRequestScopedErrorAction(&Auth{Provider: "codex", Attributes: map[string]string{"auth_kind": "oauth"}}, errExecute, cfg); matched {
		t.Fatal("upstream error rules overrode local model validation")
	}
}

func TestResponseModelValidationDrainsStreamBeforeRelease(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "terminal-mismatch"}[mismatch], func(t *testing.T) {
			m := modelValidationManager()
			firstConsumed := make(chan struct{})
			finish := make(chan struct{})
			executor := &modelValidationExecutor{stream: func(ctx context.Context, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
				if usage.StreamDeliveryTracked(ctx) {
					t.Error("internal stream still waits for downstream delivery")
				}
				chunks := make(chan cliproxyexecutor.StreamChunk)
				go func() {
					defer close(chunks)
					chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("first")}
					close(firstConsumed)
					<-finish
					usage.ResponseModelGuardFromContext(ctx, "gpt-5.4").Observe("terminal-model", mismatch)
					chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("last"), Incomplete: true}
				}()
				return &cliproxyexecutor.StreamResult{Headers: http.Header{"X-Test": {"kept"}}, Chunks: chunks}, nil
			}}
			type result struct {
				stream *cliproxyexecutor.StreamResult
				err    error
			}
			done := make(chan result, 1)
			ctx, acknowledge := usage.WithStreamDelivery(context.Background())
			defer acknowledge(nil)
			go func() {
				stream, errStream := m.executeStreamWithResponseModelValidation(ctx, executor, nil, cliproxyexecutor.Request{Model: "gpt-5.4"}, cliproxyexecutor.Options{})
				done <- result{stream, errStream}
			}()
			<-firstConsumed
			select {
			case <-done:
				t.Fatal("stream released before terminal model was known")
			default:
			}
			close(finish)
			got := <-done
			if mismatch {
				var validationErr *usage.ResponseModelValidationError
				if got.stream != nil || !errors.As(got.err, &validationErr) {
					t.Fatalf("terminal mismatch was not blocked: %+v", got)
				}
				return
			}
			if got.err != nil || got.stream.Headers.Get("X-Test") != "kept" {
				t.Fatalf("matching stream failed: %+v", got)
			}
			first, last := <-got.stream.Chunks, <-got.stream.Chunks
			if string(first.Payload) != "first" || string(last.Payload) != "last" || !last.Incomplete {
				t.Fatal("buffering changed payload order or incomplete state")
			}
		})
	}
}

func TestResponseModelValidationAllowsMissingModelAndPreservesUpstreamError(t *testing.T) {
	m := modelValidationManager()
	upstreamErr := errors.New("upstream failed")
	for _, originalErr := range []error{nil, upstreamErr} {
		executor := &modelValidationExecutor{execute: func(context.Context, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
			return cliproxyexecutor.Response{Payload: []byte("ok")}, originalErr
		}}
		response, errExecute := m.executeWithResponseModelValidation(context.Background(), executor, nil, cliproxyexecutor.Request{Model: "gpt-5.4"}, cliproxyexecutor.Options{})
		if !errors.Is(errExecute, originalErr) || (originalErr == nil && string(response.Payload) != "ok") {
			t.Fatalf("missing model or upstream error changed: %v", errExecute)
		}
	}
}

func TestResponseModelValidationStreamCancellation(t *testing.T) {
	m := modelValidationManager()
	ctx, cancel := context.WithCancel(context.Background())
	executor := &modelValidationExecutor{stream: func(context.Context, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
		cancel()
		return &cliproxyexecutor.StreamResult{Chunks: make(chan cliproxyexecutor.StreamChunk)}, nil
	}}
	stream, errStream := m.executeStreamWithResponseModelValidation(ctx, executor, nil, cliproxyexecutor.Request{Model: "gpt-5.4"}, cliproxyexecutor.Options{})
	if stream != nil || !errors.Is(errStream, context.Canceled) {
		t.Fatalf("cancellation did not stop buffering: %v", errStream)
	}
}

func TestResponseModelValidationStreamBufferLimit(t *testing.T) {
	m := modelValidationManager()
	executor := &modelValidationExecutor{stream: func(context.Context, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
		chunks := make(chan cliproxyexecutor.StreamChunk, 1)
		chunks <- cliproxyexecutor.StreamChunk{Payload: make([]byte, maxValidatedStreamBytes+1)}
		close(chunks)
		return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
	}}
	stream, errStream := m.executeStreamWithResponseModelValidation(context.Background(), executor, nil, cliproxyexecutor.Request{Model: "gpt-5.4"}, cliproxyexecutor.Options{})
	if stream != nil || statusCodeFromError(errStream) != 502 || !isRequestScopedError(errStream) {
		t.Fatalf("oversized stream escaped validation: %v", errStream)
	}
}
