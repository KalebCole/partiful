package version_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/version"
)

func TestCurrentIsTheImmutableReviewedVersion(t *testing.T) {
	t.Parallel()

	want := version.Info{
		CLIVersion:                "0.1.0",
		CommandContractRevision:   "1",
		TransportContractRevision: "2026-08-12.7",
	}
	if got := version.Current(); got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func TestCLIVersionAcceptsLinkerInjectionWithoutChangingContractRevisions(t *testing.T) {
	output := filepath.Join(t.TempDir(), "partiful")
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-X github.com/KalebCole/partiful/internal/version.CLIVersion=v9.8.7", "-o", output, "./cmd/partiful")
	command.Dir = filepath.Join("..", "..")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build release-shaped CLI: %v\n%s", err, result)
	}
	result, err := exec.Command(output, "--json", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("run release-shaped CLI: %v\n%s", err, result)
	}
	for _, value := range []string{"\"cli_version\":\"v9.8.7\"", "\"command_contract_revision\":\"1\"", "\"transport_contract_revision\":\"2026-08-12.7\""} {
		if !strings.Contains(string(result), value) {
			t.Fatalf("version output %q does not contain %q", result, value)
		}
	}
}
