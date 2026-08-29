package version_test

import (
	"testing"

	"github.com/KalebCole/partiful/internal/version"
)

func TestCurrentIsTheImmutableReviewedVersion(t *testing.T) {
	t.Parallel()

	want := version.Info{
		CLIVersion:                "0.1.0",
		CommandContractRevision:   "1",
		TransportContractRevision: "2026-08-28.2",
	}
	if got := version.Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}
