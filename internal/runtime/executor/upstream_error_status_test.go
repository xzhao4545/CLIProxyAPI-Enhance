package executor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestUpstream4xxTo502KeepsActualUsageStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, status := range []int{400, 401, 403, 404, 408, 429, 499, 500} {
				name := fmt.Sprintf("enabled=%t/stream=%t/status=%d", enabled, stream, status)
				t.Run(name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(status)
						_, _ = w.Write([]byte(`{"error":{"message":"upstream failure"}}`))
					}))
					defer server.Close()
					plugin := &captureResponseModelUsagePlugin{provider: name, records: make(chan usage.Record, 1)}
					usage.RegisterPlugin(plugin)
					cfg := &config.Config{SDKConfig: config.SDKConfig{Upstream4xxTo502: enabled}}
					exec := NewOpenAICompatExecutor(name, cfg)
					auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL, "api_key": "test"}}
					req := cliproxyexecutor.Request{Model: "test-model", Payload: []byte(`{"model":"test-model","messages":[]}`)}
					opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI}
					var errExecute error
					if stream {
						_, errExecute = exec.ExecuteStream(context.Background(), auth, req, opts)
					} else {
						_, errExecute = exec.Execute(context.Background(), auth, req, opts)
					}
					var upstream interface{ UpstreamStatusCode() int }
					if !errors.As(errExecute, &upstream) || upstream.UpstreamStatusCode() != status {
						t.Fatalf("upstream status not preserved: %v", errExecute)
					}
					msg := &interfaces.ErrorMessage{StatusCode: status, Error: errExecute}
					response := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(response)
					handlers.NewBaseAPIHandlers(&cfg.SDKConfig, nil).WriteErrorResponse(c, msg)
					want := status
					if enabled && status < 500 {
						want = http.StatusBadGateway
					}
					if response.Code != want || msg.StatusCode != status {
						t.Fatalf("downstream/original status = %d/%d, want %d/%d", response.Code, msg.StatusCode, want, status)
					}
					record := waitForResponseModelUsageRecord(t, plugin.records)
					if !record.Failed || record.Fail.StatusCode != status {
						t.Fatalf("usage failure = %+v, want status %d", record.Fail, status)
					}
				})
			}
		}
	}
}

func TestUpstream4xxTo502PreservesLocalExecutorError(t *testing.T) {
	h := handlers.NewBaseAPIHandlers(&config.SDKConfig{Upstream4xxTo502: true}, nil)
	msg := &interfaces.ErrorMessage{StatusCode: 401, Error: statusErr{code: 401, msg: "missing provider baseURL"}}
	if h.DownstreamErrorMessage(msg) != msg {
		t.Fatal("local executor error was replaced")
	}
	upstream := newCodexStatusErr(400, []byte(`{"error":{"code":"usage_limit_reached"}}`))
	if upstream.UpstreamStatusCode() != 400 {
		t.Fatalf("Codex upstream status = %d, want 400", upstream.UpstreamStatusCode())
	}
}
