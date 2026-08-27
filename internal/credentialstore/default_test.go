package credentialstore

import (
	"testing"
	"time"
)

func TestPlatformStoreTimeout(t *testing.T) {
	if got := PlatformStoreTimeout("darwin"); got != 30*time.Second {
		t.Fatalf("darwin timeout = %s", got)
	}
	if got := PlatformStoreTimeout("linux"); got != 10*time.Second {
		t.Fatalf("linux timeout = %s", got)
	}
	if got := PlatformStoreTimeout("windows"); got != 10*time.Second {
		t.Fatalf("windows timeout = %s", got)
	}
}
