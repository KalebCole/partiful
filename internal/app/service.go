package app

import (
	"context"
	"fmt"
	"reflect"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

type Service struct {
	catalog  command.Catalog
	gates    GateManifest
	handlers map[domain.OperationID]operationHandler
}

type OperationSpec[Input, Result any] struct {
	Operation     domain.OperationID
	RequiredGates []string
	ErrorGate     string
	OutcomeGate   string
	Execute       func(context.Context, *Invocation, Input) (Result, error)
}

type operationHandler interface {
	invoke(context.Context, any) (any, *Invocation, error)
	requiredGates() []string
	errorGate() string
	outcomeGate() string
}

type typedOperation[Input, Result any] struct {
	spec OperationSpec[Input, Result]
}

func NewService(catalog command.Catalog, gates GateManifest) *Service {
	return &Service{catalog: catalog, gates: gates, handlers: make(map[domain.OperationID]operationHandler)}
}

func BindOperation[Input, Result any](service *Service, spec OperationSpec[Input, Result]) error {
	if service == nil || spec.Operation == "" || spec.Execute == nil {
		return fmt.Errorf("bind operation: invalid specification")
	}
	descriptor, found := service.catalog.LookupOperation(spec.Operation)
	if !found {
		return fmt.Errorf("bind operation: unknown operation %q", spec.Operation)
	}
	inputType := reflect.TypeOf((*Input)(nil)).Elem()
	resultType := reflect.TypeOf((*Result)(nil)).Elem()
	if inputType != descriptor.InputType || resultType != descriptor.ResultType {
		return fmt.Errorf("bind operation: %s type contract disagrees with command catalog", spec.Operation)
	}
	if _, exists := service.handlers[spec.Operation]; exists {
		return fmt.Errorf("bind operation: duplicate operation %q", spec.Operation)
	}
	for _, identity := range append(append([]string(nil), spec.RequiredGates...), spec.ErrorGate, spec.OutcomeGate) {
		if identity == "" {
			continue
		}
		if _, found := service.gates.Lookup(identity); !found {
			return fmt.Errorf("bind operation: missing gate %q", identity)
		}
	}
	service.handlers[spec.Operation] = typedOperation[Input, Result]{spec: spec}
	return nil
}

func (service *Service) Invoke(ctx context.Context, operation domain.OperationID, input any) (any, error) {
	if service == nil {
		return nil, operationUnavailable()
	}
	handler, found := service.handlers[operation]
	if !found {
		return nil, operationUnavailable()
	}
	for _, identity := range handler.requiredGates() {
		if !service.gates.Allows(identity) {
			return nil, evidenceGateOpen()
		}
	}
	result, _, err := handler.invoke(ctx, input)
	if err != nil {
		if identity := handler.errorGate(); identity != "" && !service.gates.Allows(identity) && !hasEvidencedClassification(err) {
			return nil, evidenceClaimOpen()
		}
		return nil, NormalizeError(err)
	}
	return result, nil
}

func (operation typedOperation[Input, Result]) invoke(ctx context.Context, input any) (any, *Invocation, error) {
	typedInput, ok := input.(Input)
	if !ok {
		return nil, &Invocation{Operation: operation.spec.Operation}, invalidOperationInput()
	}
	invocation := &Invocation{Operation: operation.spec.Operation}
	result, err := operation.spec.Execute(ctx, invocation, typedInput)
	return result, invocation, err
}

func (operation typedOperation[Input, Result]) requiredGates() []string {
	return append([]string(nil), operation.spec.RequiredGates...)
}
func (operation typedOperation[Input, Result]) errorGate() string { return operation.spec.ErrorGate }
func (operation typedOperation[Input, Result]) outcomeGate() string {
	return operation.spec.OutcomeGate
}
