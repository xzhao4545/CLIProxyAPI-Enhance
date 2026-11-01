package openai

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/tidwall/gjson"
)

func TestImagesStreamReplacesUpstream4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &OpenAIAPIHandler{BaseAPIHandler: handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: true}, nil)}
	errUpstream := videoContentStatusError{error: fmt.Errorf("upstream failure"), status: 429}
	msg := &interfaces.ErrorMessage{StatusCode: 429, Error: errUpstream}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.writeImagesStreamErrorEvent(c, msg)
	if msg.StatusCode != 429 || gjson.Get(strings.TrimPrefix(w.Body.String(), "event: error\ndata: "), "error.type").String() != "server_error" {
		t.Fatalf("upstream=%d stream=%s", msg.StatusCode, w.Body.String())
	}
}

func TestResponsesWebsocketReplacesStatusAndKeepsUpstreamError(t *testing.T) {
	h := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{Upstream4xxTo502: true}, nil)
	// Upstream exposes only request faults on the websocket, so use an exposed 400.
	msg := &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: videoContentStatusError{error: fmt.Errorf("upstream failure"), status: http.StatusBadRequest}}
	errs := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, errUpgrade := upgrader.Upgrade(w, r, nil)
		if errUpgrade != nil {
			errs <- errUpgrade
			return
		}
		writer := newResponsesWebsocketWriter(conn)
		writer.downstreamError = h.DownstreamErrorMessage
		_, _, errWrite := writeResponsesWebsocketTerminalError(writer, nil, msg, nil)
		if errors.Is(errWrite, websocket.ErrCloseSent) {
			errWrite = nil
		}
		errs <- errWrite
	}))
	defer server.Close()
	conn, _, errDial := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if errDial != nil {
		t.Fatal(errDial)
	}
	defer func() { _ = conn.Close() }()
	_, failedPayload, errRead := conn.ReadMessage()
	if errRead != nil {
		t.Fatal(errRead)
	}
	_, payload, errRead := conn.ReadMessage()
	if errRead != nil {
		t.Fatal(errRead)
	}
	if errWrite := <-errs; errWrite != nil {
		t.Fatal(errWrite)
	}
	if gjson.GetBytes(failedPayload, "type").String() != "response.failed" ||
		gjson.GetBytes(failedPayload, "response.error.code").String() != "internal_server_error" ||
		gjson.GetBytes(payload, "type").String() != wsEventTypeError ||
		gjson.GetBytes(payload, "status").Int() != http.StatusBadGateway ||
		msg.StatusCode != http.StatusBadRequest {
		t.Fatalf("failed=%s websocket=%s upstream=%d", failedPayload, payload, msg.StatusCode)
	}
}
