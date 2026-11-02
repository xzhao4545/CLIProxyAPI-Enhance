package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestManagerResponseModelValidationWithUpstream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for index, served := range []string{"gpt-5.4-mini", "gpt-5.4-2026-05-13", ""} {
			t.Run(fmt.Sprintf("stream=%t/model=%s", stream, served), func(t *testing.T) {
				provider := fmt.Sprintf("model-validation-%t-%d", stream, index)
				var attempts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					attempts.Add(1)
					payload := map[string]any{"id": "chatcmpl-test", "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "answer"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16}}
					if served != "" {
						payload["model"] = served
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"}}]}\n\n")
						w.(http.Flusher).Flush()
						payload["object"] = "chat.completion.chunk"
						payload["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}
						encoded, _ := json.Marshal(payload)
						_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(payload)
				}))
				defer server.Close()
				cfg := &config.Config{ResponseModelValidation: []config.ResponseModelValidationRule{{Provider: provider, Models: []string{"gpt-5.4"}}}}
				manager := cliproxyauth.NewManager(nil, nil, nil)
				manager.SetConfig(cfg)
				manager.SetRetryConfig(2, 0, 0)
				manager.RegisterExecutor(NewOpenAICompatExecutor(provider, cfg))
				auth := &cliproxyauth.Auth{ID: t.Name(), Provider: provider, Attributes: map[string]string{"auth_kind": "apikey", "base_url": server.URL, "api_key": "test"}}
				registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
					t.Fatal(errRegister)
				}
				capture := &captureResponseModelUsagePlugin{provider: provider, records: make(chan usage.Record, 1)}
				usage.RegisterPlugin(capture)
				req := cliproxyexecutor.Request{Model: "gpt-5.4", Payload: []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`)}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: stream}
				var errExecute error
				var output strings.Builder
				if stream {
					var result *cliproxyexecutor.StreamResult
					result, errExecute = manager.ExecuteStream(context.Background(), []string{provider}, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								t.Fatal(chunk.Err)
							}
							output.Write(chunk.Payload)
						}
					}
				} else {
					var response cliproxyexecutor.Response
					response, errExecute = manager.Execute(context.Background(), []string{provider}, req, opts)
					output.Write(response.Payload)
				}
				blocked := served == "gpt-5.4-mini"
				var mismatch *usage.ResponseModelValidationError
				if errors.As(errExecute, &mismatch) != blocked || (errExecute != nil && !blocked) {
					t.Fatalf("unexpected validation result: %v", errExecute)
				}
				if blocked && output.Len() != 0 {
					t.Fatal("wrong-model content escaped")
				}
				if !blocked && !strings.Contains(output.String(), "answer") {
					t.Fatal("accepted response lost content")
				}
				if attempts.Load() != 1 {
					t.Fatalf("validation triggered upstream replay: %d attempts", attempts.Load())
				}
				record := waitForResponseModelUsageRecord(t, capture.records)
				if record.Failed != blocked || record.ResponseModel != served || record.Detail.TotalTokens != 16 {
					t.Fatalf("incorrect validation usage: %+v", record)
				}
				currentAuth, _ := manager.GetByID(auth.ID)
				if currentAuth.Unavailable || !currentAuth.NextRetryAfter.IsZero() {
					t.Fatal("validation cooled the credential")
				}
			})
		}
	}
}
