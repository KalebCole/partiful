package credentialstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type FileCoordinator struct {
	root    string
	timeout time.Duration
}

func NewFileCoordinator(root string, timeout time.Duration) *FileCoordinator {
	return &FileCoordinator{root: filepath.Clean(root), timeout: timeout}
}

func (coordinator *FileCoordinator) Do(ctx context.Context, operation func(context.Context) error) error {
	if err := os.MkdirAll(coordinator.root, 0o700); err != nil {
		return errors.New("authentication lock unavailable")
	}
	if err := requireRealDirectory(coordinator.root, 0o700); err != nil {
		return err
	}
	path := filepath.Join(coordinator.root, "auth.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return errors.New("authentication lock unavailable")
	}
	defer file.Close()
	if _, err := credentialFile(path); err != nil {
		return err
	}
	deadline := time.Now().Add(coordinator.timeout)
	for {
		locked, err := tryFileLock(file)
		if err != nil {
			return errors.New("authentication lock unavailable")
		}
		if locked {
			defer unlockFile(file)
			return operation(ctx)
		}
		if time.Now().After(deadline) {
			return errors.New("authentication lock busy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
