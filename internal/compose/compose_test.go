package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/credentialstore"
	"github.com/KalebCole/partiful/internal/domain"
)

type unavailableCredentialStore struct {
	probeHadDeadline bool
	storeCalled      bool
	deleteCalled     bool
}

func (store *unavailableCredentialStore) Backend() auth.BackendKind {
	return "os-credential-store"
}

func (store *unavailableCredentialStore) Load(ctx context.Context, _ auth.Slot) ([]byte, error) {
	_, store.probeHadDeadline = ctx.Deadline()
	return nil, errors.New("platform store unavailable")
}

func (store *unavailableCredentialStore) Store(context.Context, auth.Slot, []byte) error {
	store.storeCalled = true
	return errors.New("unexpected store")
}

func (store *unavailableCredentialStore) Delete(context.Context, auth.Slot) error {
	store.deleteCalled = true
	return errors.New("unexpected delete")
}

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

func TestSelectCredentialStoreFallsBackAfterBoundedNonMutatingOSProbe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	osStore := &unavailableCredentialStore{}
	var diagnostics bytes.Buffer

	selected, err := selectCredentialStore(context.Background(), root, osStore, &diagnostics)
	if err != nil {
		t.Fatalf("selectCredentialStore() error = %v", err)
	}
	if selected.Backend() != credentialstore.NewFileStore(root).Backend() {
		t.Fatalf("selected backend = %q, want protected-file", selected.Backend())
	}
	if !osStore.probeHadDeadline {
		t.Fatal("OS credential-store probe had no deadline")
	}
	if osStore.storeCalled || osStore.deleteCalled {
		t.Fatal("OS credential-store probe mutated the store")
	}
	if !strings.Contains(diagnostics.String(), "platform_store_unavailable") {
		t.Fatalf("diagnostics = %q, want fallback warning", diagnostics.String())
	}
}
