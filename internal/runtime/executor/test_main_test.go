package executor

import (
	"os"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestMain(m *testing.M) {
	// Upstream tests expect cooldown on the first transient failure; the fork default waits for 5.
	cliproxyauth.SetTransientFailureCoolDownMinFailures(1)
	os.Exit(m.Run())
}
