package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/credentialstore"
	"github.com/KalebCole/partiful/internal/domain"
)

func TestNewCLIComposesVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := filepath.Join(t.TempDir(), "data")
	adapter, err := newCLI(context.Background(), cliConfig{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		stderr: &stderr,
		store:  credentialstore.NewFileStore(root),
		root:   root,
	})
	if err != nil {
		t.Fatalf("newCLI() error = %v", err)
	}

	if exitCode := adapter.Run(context.Background(), []string{"version", "--json"}); exitCode != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("version output is not JSON: %v", err)
	}
	for _, field := range []string{"cli_version", "command_contract_revision", "transport_contract_revision"} {
		if result[field] == "" {
			t.Errorf("version output %q is empty", field)
		}
	}
}

func TestNewCLIProtectedCommandRequiresAuthentication(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := filepath.Join(t.TempDir(), "data")
	adapter, err := newCLI(context.Background(), cliConfig{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		stderr: &stderr,
		store:  credentialstore.NewFileStore(root),
		root:   root,
	})
	if err != nil {
		t.Fatalf("newCLI() error = %v", err)
	}

	exitCode := adapter.Run(context.Background(), []string{"events", "cancel", "event_test", "--json"})
	if exitCode != int(domain.ExitAuth) {
		t.Fatalf("Run() exit code = %d, want %d; stdout = %q stderr = %q", exitCode, domain.ExitAuth, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"type":"auth.required"`) {
		t.Fatalf("stderr = %q, want auth.required", stderr.String())
	}
}
