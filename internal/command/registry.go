package command

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/KalebCole/partiful/internal/domain"
)

var commandIDPattern = regexp.MustCompile(`^CMD-[0-9]{3}$`)

// NewCatalog validates and freezes the complete built-in command inventory.
// It rejects partial or extended inventories, so it is not a registration API.
func NewCatalog(definitions []Descriptor) (Catalog, error) {
	if err := validateDefinitions(definitions); err != nil {
		return Catalog{}, err
	}

	catalog := Catalog{
		definitions: make([]Descriptor, len(definitions)),
		byID:        make(map[ID]int, len(definitions)),
		byCLI:       make(map[string]int, len(definitions)),
		byOperation: make(map[domain.OperationID]int, len(definitions)),
		byMCP:       make(map[string]int, len(definitions)-1),
	}
	for index, definition := range definitions {
		catalog.definitions[index] = cloneDescriptor(definition)
		catalog.byID[definition.ID] = index
		catalog.byCLI[definition.CLI.Path] = index
		catalog.byOperation[definition.Operation] = index
		if definition.MCP != nil {
			catalog.byMCP[definition.MCP.Name] = index
		}
	}
	return catalog, nil
}

func (catalog Catalog) LookupID(id ID) (Descriptor, bool) {
	index, ok := catalog.byID[id]
	if !ok {
		return Descriptor{}, false
	}
	return catalog.lookup(index)
}

func (catalog Catalog) LookupCLI(path string) (Descriptor, bool) {
	index, ok := catalog.byCLI[path]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(catalog.definitions[index]), true
}

func (catalog Catalog) LookupOperation(operation domain.OperationID) (Descriptor, bool) {
	index, ok := catalog.byOperation[operation]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(catalog.definitions[index]), true
}

func (catalog Catalog) LookupMCP(name string) (Descriptor, bool) {
	index, ok := catalog.byMCP[name]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(catalog.definitions[index]), true
}

func (catalog Catalog) lookup(index int) (Descriptor, bool) {
	if index < 0 || index >= len(catalog.definitions) {
		return Descriptor{}, false
	}
	return cloneDescriptor(catalog.definitions[index]), true
}

func validateDefinitions(definitions []Descriptor) error {
	if len(definitions) != 24 {
		return fmt.Errorf("command catalog: got %d definitions, want 24", len(definitions))
	}

	ids := make(map[ID]struct{}, len(definitions))
	cliPaths := make(map[string]struct{}, len(definitions))
	operations := make(map[domain.OperationID]struct{}, len(definitions))
	mcpNames := make(map[string]struct{}, len(definitions)-1)
	cliOnly := 0
	for index, definition := range definitions {
		wantID := ID(fmt.Sprintf("CMD-%03d", index+1))
		if definition.ID != wantID || !commandIDPattern.MatchString(string(definition.ID)) {
			return fmt.Errorf("command catalog: definition %d has ID %q, want %q", index, definition.ID, wantID)
		}
		if _, exists := ids[definition.ID]; exists {
			return fmt.Errorf("command catalog: duplicate command ID %q", definition.ID)
		}
		ids[definition.ID] = struct{}{}
		if definition.Disposition != Paired && definition.Disposition != CLIOnly {
			return fmt.Errorf("command catalog: %s has invalid disposition %q", definition.ID, definition.Disposition)
		}
		if definition.CLI.Path == "" || strings.HasPrefix(definition.CLI.Path, "partiful ") {
			return fmt.Errorf("command catalog: %s has invalid CLI path", definition.ID)
		}
		if _, exists := cliPaths[definition.CLI.Path]; exists {
			return fmt.Errorf("command catalog: duplicate CLI path %q", definition.CLI.Path)
		}
		cliPaths[definition.CLI.Path] = struct{}{}
		if definition.Operation == "" {
			return fmt.Errorf("command catalog: %s has empty operation", definition.ID)
		}
		if _, exists := operations[definition.Operation]; exists {
			return fmt.Errorf("command catalog: duplicate operation %q", definition.Operation)
		}
		operations[definition.Operation] = struct{}{}
		if definition.InputType == nil || definition.ResultType == nil || definition.decodeInput == nil {
			return fmt.Errorf("command catalog: %s lacks typed input or result", definition.ID)
		}
		if definition.Help == "" || len(definition.FailureTypes) == 0 {
			return fmt.Errorf("command catalog: %s lacks help or failure metadata", definition.ID)
		}
		if err := validateSchema(definition.InputSchema, "input"); err != nil {
			return fmt.Errorf("command catalog: %s: %w", definition.ID, err)
		}
		if err := validateSchema(definition.ResultSchema, "result"); err != nil {
			return fmt.Errorf("command catalog: %s: %w", definition.ID, err)
		}
		if definition.DryRun != definition.Risk.isMutation() {
			return fmt.Errorf("command catalog: %s dry-run metadata disagrees with mutation risk", definition.ID)
		}
		if definition.DryRun && !schemaHasProperty(definition.InputSchema, "dry_run") {
			return fmt.Errorf("command catalog: %s lacks dry_run input", definition.ID)
		}

		switch definition.Disposition {
		case CLIOnly:
			cliOnly++
			if definition.CLI.Path != "auth login" || definition.MCP != nil {
				return fmt.Errorf("command catalog: %s is not the auth.login CLI-only exception", definition.ID)
			}
		case Paired:
			if definition.MCP == nil || definition.MCP.Name == "" {
				return fmt.Errorf("command catalog: %s lacks paired MCP tool", definition.ID)
			}
			if _, exists := mcpNames[definition.MCP.Name]; exists {
				return fmt.Errorf("command catalog: duplicate MCP tool %q", definition.MCP.Name)
			}
			mcpNames[definition.MCP.Name] = struct{}{}
			if err := validateHints(definition); err != nil {
				return err
			}
		}
	}
	if cliOnly != 1 || len(mcpNames) != 23 {
		return fmt.Errorf("command catalog: got %d CLI-only and %d MCP tools, want 1 and 23", cliOnly, len(mcpNames))
	}
	return nil
}

func validateHints(definition Descriptor) error {
	hints := definition.MCP.Hints
	wantReadOnly := definition.Risk == RiskRead || definition.Risk == RiskDiagnostic
	wantDestructive := definition.Risk == RiskCredentialDelete || definition.Risk == RiskDestructiveWrite
	if hints.ReadOnly != wantReadOnly || hints.Destructive != wantDestructive {
		return fmt.Errorf("command catalog: %s MCP hints disagree with risk", definition.ID)
	}
	return nil
}

func validateSchema(schema Schema, location string) error {
	switch schema.Kind {
	case "string", "integer", "boolean":
	case "array":
		if schema.Items == nil {
			return fmt.Errorf("%s array schema lacks items", location)
		}
		if err := validateSchema(*schema.Items, location+" items"); err != nil {
			return err
		}
	case "object":
		if schema.AdditionalProperties {
			return fmt.Errorf("%s object schema permits additional properties", location)
		}
		seen := make(map[string]struct{}, len(schema.Properties))
		for _, property := range schema.Properties {
			if property.Name == "" {
				return fmt.Errorf("%s object schema has empty property name", location)
			}
			if _, exists := seen[property.Name]; exists {
				return fmt.Errorf("%s object schema repeats property %q", location, property.Name)
			}
			seen[property.Name] = struct{}{}
			if err := validateSchema(property.Schema, location+"."+property.Name); err != nil {
				return err
			}
		}
		for _, required := range schema.Required {
			if _, exists := seen[required]; !exists {
				return fmt.Errorf("%s requires unknown property %q", location, required)
			}
		}
		for index, branch := range schema.OneOf {
			if err := validateSchema(branch, fmt.Sprintf("%s oneOf[%d]", location, index)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s schema has invalid kind %q", location, schema.Kind)
	}
	return nil
}

func schemaHasProperty(schema Schema, name string) bool {
	for _, property := range schema.Properties {
		if property.Name == name {
			return true
		}
	}
	for _, branch := range schema.OneOf {
		if schemaHasProperty(branch, name) {
			return true
		}
	}
	return false
}

func (risk Risk) isMutation() bool {
	switch risk {
	case RiskCredentialDelete, RiskWrite, RiskDestructiveWrite:
		return true
	default:
		return false
	}
}
