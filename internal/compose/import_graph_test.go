package compose_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/transport"
)

func TestAuthSecretsRedactOrdinaryFormatting(t *testing.T) {
	t.Parallel()

	secret := auth.NewSecret("do-not-print")
	if got := fmt.Sprintf("%v %#v", secret, secret); got != "[REDACTED] [REDACTED]" {
		t.Fatalf("secret formatting = %q", got)
	}
	if got := secret.Reveal(); got != "do-not-print" {
		t.Fatal("secret did not preserve its private value")
	}
}

func TestProtocolFailureUsesSanitizedMessage(t *testing.T) {
	t.Parallel()

	var err error = &transport.ProtocolFailure{Operation: "getEventInfo"}
	if got := err.Error(); got != "remote protocol failure" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestProductionPackageGraph(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate import graph test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	command := exec.Command("python3", filepath.Join(root, "scripts", "verify_go_package_graph.py"))
	command.Dir = root
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("verify package graph: %v\n%s", err, output.String())
	}
	if !strings.HasPrefix(output.String(), "PASS: production_packages=") ||
		!strings.Contains(output.String(), " internal_edges=") {
		t.Fatalf("unexpected verifier output: %q", output.String())
	}
}

func TestProductionPackageGraphRejectsUnknownPackage(t *testing.T) {
	t.Parallel()

	output := runGraphVerifier(t, map[string]string{
		"internal/fakes/fakes.go": "package fakes\n",
	})
	if !strings.Contains(output, "packages outside the closed catalog: ['internal/fakes']") {
		t.Fatalf("unexpected verifier output: %q", output)
	}
}

func TestProductionPackageGraphRejectsDisallowedEdge(t *testing.T) {
	t.Parallel()

	output := runGraphVerifier(t, map[string]string{
		"internal/domain/domain.go": "package domain\n\nimport _ \"github.com/KalebCole/partiful/internal/auth\"\n",
		"internal/auth/auth.go":     "package auth\n",
	})
	if !strings.Contains(output, "disallowed internal imports from internal/domain: ['internal/auth']") {
		t.Fatalf("unexpected verifier output: %q", output)
	}
}

func TestProductionPackageGraphRejectsCycle(t *testing.T) {
	t.Parallel()

	output := runGraphVerifier(t, map[string]string{
		"internal/domain/domain.go": "package domain\n\nimport _ \"github.com/KalebCole/partiful/internal/auth\"\n",
		"internal/auth/auth.go":     "package auth\n\nimport _ \"github.com/KalebCole/partiful/internal/domain\"\n",
	})
	if !strings.Contains(output, "go list failed:") || !strings.Contains(output, "import cycle not allowed") {
		t.Fatalf("unexpected verifier output: %q", output)
	}
}

func runGraphVerifier(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	files["go.mod"] = "module github.com/KalebCole/partiful\n\ngo 1.24\n"
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate import graph test")
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	command := exec.Command(
		"python3",
		filepath.Join(projectRoot, "scripts", "verify_go_package_graph.py"),
		"--root",
		root,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("verifier accepted invalid graph:\n%s", output)
	}
	return string(output)
}
