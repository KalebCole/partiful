package credentialstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
)

type memoryDriver struct{ values map[auth.Slot][]byte }

func (driver *memoryDriver) Load(_ context.Context, slot auth.Slot) ([]byte, error) {
	return driver.values[slot], nil
}
func (driver *memoryDriver) Store(_ context.Context, slot auth.Slot, value []byte) error {
	driver.values[slot] = append([]byte(nil), value...)
	return nil
}
func (driver *memoryDriver) Delete(_ context.Context, slot auth.Slot) error {
	delete(driver.values, slot)
	return nil
}

type warningSink struct{ count int }

func (sink *warningSink) Warn(context.Context, auth.Diagnostic) { sink.count++ }

func TestSelectorPersistsFallbackAndWarnsOnce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	file := NewFileStore(root)
	osStore := NewOSStore(&memoryDriver{values: map[auth.Slot][]byte{}}, time.Second)
	warnings := &warningSink{}
	selector := Selector{Root: root, OS: osStore, File: file, Probe: func(context.Context) error { return errors.New("unavailable") }, Diagnostics: warnings}
	selected, err := selector.Select(context.Background())
	if err != nil || selected.Backend() != file.Backend() {
		t.Fatalf("Select = %v, %v", selected, err)
	}
	selected, err = selector.Select(context.Background())
	if err != nil || selected.Backend() != file.Backend() || warnings.count != 1 {
		t.Fatalf("second Select backend=%v warnings=%d error=%v", selected.Backend(), warnings.count, err)
	}
}

func TestCommandDriverKeepsSecretOutOfArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	directory := t.TempDir()
	executable := filepath.Join(directory, "driver")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$(dirname \"$0\")/args\"\ncat > \"$(dirname \"$0\")/stdin\"\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &CommandDriver{goos: "darwin", executable: executable}
	secret := []byte("private-credential-value")
	if err := driver.Store(context.Background(), auth.SlotA, secret); err != nil {
		t.Fatal(err)
	}
	arguments, _ := os.ReadFile(filepath.Join(directory, "args"))
	input, _ := os.ReadFile(filepath.Join(directory, "stdin"))
	if strings.Contains(string(arguments), string(secret)) {
		t.Fatal("secret was present in process arguments")
	}
	if !strings.Contains(string(input), string(secret)) {
		t.Fatal("secret was not passed through stdin")
	}
}

func TestWindowsRequestUsesStableStdinFields(t *testing.T) {
	payload, err := encodeWindowsRequest("store", "slot-a", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["action"] != "store" || decoded["slot"] != "slot-a" || decoded["value"] != "secret" || len(decoded) != 3 {
		t.Fatalf("payload = %s", payload)
	}
}
