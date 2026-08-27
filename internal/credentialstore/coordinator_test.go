package credentialstore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileCoordinatorSerializesCallers(t *testing.T) {
	coordinator := NewFileCoordinator(t.TempDir(), time.Second)
	var active atomic.Int32
	var maximum atomic.Int32
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := coordinator.Do(context.Background(), func(context.Context) error {
				current := active.Add(1)
				for {
					previous := maximum.Load()
					if current <= previous || maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				active.Add(-1)
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent operations = %d", maximum.Load())
	}
}
