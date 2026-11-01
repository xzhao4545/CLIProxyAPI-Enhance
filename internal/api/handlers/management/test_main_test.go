package management

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// Upstream tests expect cooldown on the first transient failure; the fork default waits for 5.
	coreauth.SetTransientFailureCoolDownMinFailures(1)
	os.Exit(m.Run())
}
