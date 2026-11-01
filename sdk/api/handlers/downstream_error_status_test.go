package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type downstreamTestUpstreamError struct{}

func (downstreamTestUpstreamError) Error() string           { return "rate limited upstream" }
func (downstreamTestUpstreamError) UpstreamStatusCode() int { return 429 }

type downstreamStatusError int

func (e downstreamStatusError) Error() string           { return "upstream failure" }
func (e downstreamStatusError) UpstreamStatusCode() int { return int(e) }

func TestDownstreamErrorMessageStatusBoundaries(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: enabled}, nil)
		for _, status := range []int{0, 399, 400, 401, 403, 404, 408, 429, 499, 500, 503} {
			msg := &interfaces.ErrorMessage{
				StatusCode: status,
				Error:      fmt.Errorf("wrapped: %w", downstreamStatusError(status)),
				Addon:      http.Header{"Retry-After": {"5"}},
			}
			got := h.DownstreamErrorMessage(msg)
			want := status
			if enabled && status >= 400 && status < 500 {
				want = http.StatusBadGateway
			}
			if got.StatusCode != want || msg.StatusCode != status || got.Error != msg.Error || got.Addon.Get("Retry-After") != "5" {
				t.Fatalf("enabled=%t upstream=%d: downstream=%+v original=%+v", enabled, status, got, msg)
			}
		}
	}
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: true}, nil)
	local := &interfaces.ErrorMessage{StatusCode: 400, Error: errors.New("invalid request")}
	if h.DownstreamErrorMessage(local) != local || h.DownstreamErrorMessage(nil) != nil {
		t.Fatal("local validation or nil error changed")
	}
}

func TestProtocolStreamErrorPreservesUpstreamStatus(t *testing.T) {
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: true}, nil)
	errOriginal := downstreamTestUpstreamError{}
	streamErr := modelExecutionStreamErrorFromMessage(&interfaces.ErrorMessage{StatusCode: 429, Error: errOriginal})
	msg := &interfaces.ErrorMessage{StatusCode: streamErr.StatusCode, Error: streamErr}
	if h.DownstreamErrorMessage(msg).StatusCode != 502 || !errors.Is(streamErr, errOriginal) || streamErr.StatusCode != 429 {
		t.Fatal("protocol stream lost the upstream status or original error")
	}
}

func TestForwardStreamReplacesTerminalStatusAndKeepsCancellationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: true}, nil)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()
	errOriginal := downstreamTestUpstreamError{}
	msg := &interfaces.ErrorMessage{StatusCode: 429, Error: errOriginal}
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- msg
	close(errs)
	var terminalStatus int
	var canceledWith error
	h.ForwardStream(c, response, func(err error) { canceledWith = err }, make(chan []byte), errs, StreamForwardOptions{
		WriteTerminalError: func(err *interfaces.ErrorMessage) { terminalStatus = err.StatusCode },
	})
	if terminalStatus != 502 || msg.StatusCode != 429 || canceledWith != errOriginal || response.Code != 200 {
		t.Fatalf("terminal/original/http=%d/%d/%d cancel=%v", terminalStatus, msg.StatusCode, response.Code, canceledWith)
	}
	msg.DirectResponse = true
	if h.DownstreamErrorMessage(msg) != msg {
		t.Fatal("trusted direct rejection was replaced")
	}
}
