package credentialstore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/KalebCole/partiful/internal/auth"
)

func TestDataRootUsesFixedPlatformLocation(t *testing.T) {
	cases := []struct{ os, home, xdg, local, want string }{
		{"darwin", "/home/test", "", "", "/home/test/Library/Application Support/partiful"},
		{"linux", "/home/test", "", "", "/home/test/.local/share/partiful"},
		{"linux", "/home/test", "/attacker-controlled", "", "/home/test/.local/share/partiful"},
		{"windows", `C:\Users	est`, "", `C:\attacker-controlled`, `C:\Users	est/AppData/Local/Partiful`},
	}
	for _, test := range cases {
		if got := DataRoot(test.os, test.home, test.xdg, test.local); got != test.want {
			t.Errorf("DataRoot(%s) = %q, want %q", test.os, got, test.want)
		}
	}
}

func TestFileStoreWritesOwnerOnlyAndRejectsAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode and symlink test")
	}
	root := filepath.Join(t.TempDir(), "partiful")
	store := NewFileStore(root)
	if err := store.Store(context.Background(), auth.SlotA, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials", "slot-a.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", dir.Mode().Perm())
	}
	if got, err := store.Load(context.Background(), auth.SlotA); err != nil || string(got) != "secret" {
		t.Fatalf("Load = %q, %v", got, err)
	}

	external := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), auth.SlotA); err == nil {
		t.Fatal("Load followed symlink")
	}
	if got, _ := os.ReadFile(external); string(got) != "external" {
		t.Fatal("external target changed")
	}
}

func TestFileStoreRejectsHardLinkedCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlink metadata test")
	}
	root := filepath.Join(t.TempDir(), "partiful")
	store := NewFileStore(root)
	if err := store.Store(context.Background(), auth.SlotA, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials", "slot-a.json")
	peer := filepath.Join(t.TempDir(), "peer")
	if err := os.Link(path, peer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), auth.SlotA); err == nil {
		t.Fatal("Load accepted hardlink")
	}
}
