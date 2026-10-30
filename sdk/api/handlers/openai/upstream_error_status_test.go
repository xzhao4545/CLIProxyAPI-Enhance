package openai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
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
	msg := &interfaces.ErrorMessage{StatusCode: 403, Error: videoContentStatusError{error: fmt.Errorf("upstream failure"), status: 403}}
	errs := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, errUpgrade := upgrader.Upgrade(w, r, nil)
		if errUpgrade != nil {
			errs <- errUpgrade
			return
		}
		_, errWrite := writeResponsesWebsocketError(conn, nil, h.DownstreamErrorMessage(msg))
		errs <- errWrite
		_ = conn.Close()
	}))
	defer server.Close()
	conn, _, errDial := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if errDial != nil {
		t.Fatal(errDial)
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("close websocket: %v", errClose)
		}
	}()
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
	if gjson.GetBytes(failedPayload, "response.error.code").String() != "internal_server_error" || gjson.GetBytes(payload, "status").Int() != 502 || msg.StatusCode != 403 {
		t.Fatalf("websocket=%s upstream=%d", payload, msg.StatusCode)
	}
}
