package credentialstore

import (
	"context"
	"runtime"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
)

type OSDriver interface {
	Load(context.Context, auth.Slot) ([]byte, error)
	Store(context.Context, auth.Slot, []byte) error
	Delete(context.Context, auth.Slot) error
}

type OSStore struct {
	driver  OSDriver
	timeout time.Duration
}

func NewOSStore(driver OSDriver, timeout time.Duration) *OSStore {
	return &OSStore{driver: driver, timeout: timeout}
}

func PlatformStoreTimeout(goos string) time.Duration {
	if goos == "darwin" {
		return 30 * time.Second
	}
	return 10 * time.Second
}

func DefaultOSStore() (*OSStore, error) {
	driver, err := DefaultCommandDriver()
	if err != nil {
		return nil, err
	}
	return NewOSStore(driver, PlatformStoreTimeout(runtime.GOOS)), nil
}

func (store *OSStore) Backend() auth.BackendKind { return "os-credential-store" }
func (store *OSStore) operation(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, store.timeout)
}
func (store *OSStore) Load(ctx context.Context, slot auth.Slot) ([]byte, error) {
	bounded, cancel := store.operation(ctx)
	defer cancel()
	return store.driver.Load(bounded, slot)
}
func (store *OSStore) Store(ctx context.Context, slot auth.Slot, value []byte) error {
	bounded, cancel := store.operation(ctx)
	defer cancel()
	return store.driver.Store(bounded, slot, value)
}
func (store *OSStore) Delete(ctx context.Context, slot auth.Slot) error {
	bounded, cancel := store.operation(ctx)
	defer cancel()
	return store.driver.Delete(bounded, slot)
}

var _ auth.CredentialStore = (*OSStore)(nil)
