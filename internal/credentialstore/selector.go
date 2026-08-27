package credentialstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/KalebCole/partiful/internal/auth"
)

type Probe func(context.Context) error
type Selector struct {
	Root        string
	OS          auth.CredentialStore
	File        auth.CredentialStore
	Probe       Probe
	Diagnostics auth.DiagnosticSink
}

func (selector Selector) Select(ctx context.Context) (auth.CredentialStore, error) {
	marker, err := selector.readMarker()
	if err != nil {
		return nil, err
	}
	if marker != "" {
		if selector.OS != nil && marker == string(selector.OS.Backend()) {
			return selector.OS, nil
		}
		if selector.File != nil && marker == string(selector.File.Backend()) {
			return selector.File, nil
		}
		return nil, errors.New("credential backend marker is invalid")
	}
	selected := selector.OS
	if selected == nil || selector.Probe == nil || selector.Probe(ctx) != nil {
		selected = selector.File
		if selected == nil {
			return nil, errors.New("credential storage unavailable")
		}
		if selector.Diagnostics != nil {
			selector.Diagnostics.Warn(ctx, auth.Diagnostic{Kind: "fallback", Backend: selected.Backend(), State: "platform_store_unavailable", Remediation: "Platform credential storage can be selected after local logout."})
		}
	}
	if err := selector.writeMarker(string(selected.Backend())); err != nil {
		return nil, err
	}
	return selected, nil
}
func (selector Selector) markerPath(create bool) (string, error) {
	directory, err := NewFileStore(selector.Root).directory(create)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(directory), "credential-backend"), nil
}
func (selector Selector) readMarker() (string, error) {
	path, err := selector.markerPath(false)
	if err != nil {
		return "", err
	}
	if _, err := credentialFile(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("credential backend marker unavailable")
	}
	return string(value), nil
}
func (selector Selector) writeMarker(value string) error {
	path, err := selector.markerPath(true)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".backend-*")
	if err != nil {
		return errors.New("credential backend marker unavailable")
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return errors.New("credential backend marker unavailable")
	}
	if _, err := temporary.WriteString(value); err != nil {
		temporary.Close()
		return errors.New("credential backend marker unavailable")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return errors.New("credential backend marker unavailable")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("credential backend marker unavailable")
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return errors.New("credential backend marker unavailable")
	}
	return syncDirectory(filepath.Dir(path))
}
func (selector Selector) ClearMarker() error {
	path, err := selector.markerPath(false)
	if err != nil {
		return err
	}
	if _, err := credentialFile(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
