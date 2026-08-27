package command

import (
	"fmt"

	"github.com/KalebCole/partiful/internal/domain"
)

// Project derives the public schema result from the immutable catalog.
func (catalog Catalog) Project(path *string) (domain.CommandSchemaResult, error) {
	definitions := catalog.definitions
	if path != nil {
		definition, ok := catalog.LookupCLI(*path)
		if !ok {
			return domain.CommandSchemaResult{}, fmt.Errorf("unknown command path %q", *path)
		}
		definitions = []Descriptor{definition}
	}

	result := domain.CommandSchemaResult{
		Commands: make([]domain.CommandSchemaCommand, 0, len(definitions)),
	}
	for _, definition := range definitions {
		result.Commands = append(result.Commands, projectDescriptor(definition))
		if path == nil {
			if definition.MCP == nil {
				result.CLIOnlyCommands = append(result.CLIOnlyCommands, definition.CLI.Path)
			} else {
				result.MCPTools = append(result.MCPTools, definition.MCP.Name)
			}
		}
	}
	return result, nil
}

func projectDescriptor(definition Descriptor) domain.CommandSchemaCommand {
	projection := domain.CommandSchemaCommand{
		ID:            string(definition.ID),
		CLIPath:       definition.CLI.Path,
		Help:          definition.Help,
		FailureTypes:  append([]domain.ErrorType(nil), definition.FailureTypes...),
		Positionals:   make([]domain.CommandSchemaPositional, len(definition.CLI.Positionals)),
		Flags:         make([]domain.CommandSchemaFlag, len(definition.CLI.Flags)),
		InputSchema:   projectSchema(definition.InputSchema),
		ResultSchema:  projectSchema(definition.ResultSchema),
		Authorization: definition.Authorization,
		Risk:          string(definition.Risk),
		DryRun:        definition.DryRun,
	}
	for index, positional := range definition.CLI.Positionals {
		projection.Positionals[index] = domain.CommandSchemaPositional{Name: positional.Name, Required: positional.Required}
	}
	for index, flag := range definition.CLI.Flags {
		projection.Flags[index] = domain.CommandSchemaFlag{
			Name: flag.Name, Field: flag.Field, Required: flag.Required,
			Repeated: flag.Repeated, ContentSource: flag.ContentSource,
		}
	}
	if definition.MCP != nil {
		projection.MCP = &domain.CommandSchemaMCP{
			Name: definition.MCP.Name,
			Hints: domain.CommandSchemaMCPHints{
				ReadOnly: definition.MCP.Hints.ReadOnly, Destructive: definition.MCP.Hints.Destructive,
				Idempotent: definition.MCP.Hints.Idempotent, OpenWorld: definition.MCP.Hints.OpenWorld,
			},
		}
	}
	return projection
}

func projectSchema(schema Schema) domain.CommandSchemaValue {
	projection := domain.CommandSchemaValue{
		Kind: schema.Kind, Nullable: schema.Nullable, Format: schema.Format,
		DefaultBool: cloneBoolPointer(schema.DefaultBool), Minimum: cloneIntPointer(schema.Minimum),
		AdditionalProperties: schema.AdditionalProperties,
		Required:             append([]string(nil), schema.Required...),
		Enum:                 append([]string(nil), schema.Enum...),
		Properties:           make([]domain.CommandSchemaProperty, len(schema.Properties)),
		OneOf:                make([]domain.CommandSchemaValue, len(schema.OneOf)),
	}
	for index, property := range schema.Properties {
		projection.Properties[index] = domain.CommandSchemaProperty{Name: property.Name, Schema: projectSchema(property.Schema)}
	}
	if schema.Items != nil {
		items := projectSchema(*schema.Items)
		projection.Items = &items
	}
	for index, branch := range schema.OneOf {
		projection.OneOf[index] = projectSchema(branch)
	}
	if schema.Discriminator != "" {
		discriminator := schema.Discriminator
		projection.Discriminator = &discriminator
	}
	return projection
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
