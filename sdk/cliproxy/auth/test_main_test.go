package auth

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Upstream tests expect cooldown on the first transient failure; the fork default waits for 5.
	// Fork threshold tests set their own value explicitly.
	SetTransientFailureCoolDownMinFailures(1)
	os.Exit(m.Run())
}
