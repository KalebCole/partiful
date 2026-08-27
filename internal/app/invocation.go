package app

import (
	"fmt"
	"sync"

	"github.com/KalebCole/partiful/internal/domain"
)

// Invocation contains per-call state that must never be shared across commands.
type Invocation struct {
	Operation domain.OperationID

	mutationMu       sync.Mutex
	mutationAttempts int
}

func (invocation *Invocation) MutationAttempts() int {
	if invocation == nil {
		return 0
	}
	invocation.mutationMu.Lock()
	defer invocation.mutationMu.Unlock()
	return invocation.mutationAttempts
}

// DispatchMutation is the only application-kernel mutation dispatch seam.
// It records the attempt before calling fn because the remote outcome may be unknown.
func DispatchMutation[T any](invocation *Invocation, fn func() (T, error)) (T, error) {
	var zero T
	if invocation == nil || fn == nil {
		return zero, fmt.Errorf("mutation dispatch: invalid invocation")
	}
	invocation.mutationMu.Lock()
	if invocation.mutationAttempts != 0 {
		invocation.mutationMu.Unlock()
		return zero, &domain.Error{Type: domain.ErrorInternalFailure, Code: "MUTATION_ALREADY_ATTEMPTED", Message: "mutation dispatch was already attempted"}
	}
	invocation.mutationAttempts = 1
	invocation.mutationMu.Unlock()
	return fn()
}
