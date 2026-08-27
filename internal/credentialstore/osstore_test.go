package credentialstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
)

type blockingDriver struct{}

func (blockingDriver) Load(ctx context.Context, _ auth.Slot) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingDriver) Store(ctx context.Context, _ auth.Slot, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}
func (blockingDriver) Delete(ctx context.Context, _ auth.Slot) error { <-ctx.Done(); return ctx.Err() }

func TestOSStoreBoundsEveryOperation(t *testing.T) {
	store := NewOSStore(blockingDriver{}, 20*time.Millisecond)
	started := time.Now()
	if _, err := store.Load(context.Background(), auth.SlotA); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load error = %v", err)
	}
	if err := store.Store(context.Background(), auth.SlotA, []byte("secret")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Store error = %v", err)
	}
	if err := store.Delete(context.Background(), auth.SlotA); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Delete error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("operations took %s", elapsed)
	}
}
