package command

import "encoding/json"

type jsonSchemaType []string

func (types jsonSchemaType) MarshalJSON() ([]byte, error) {
	if len(types) == 1 {
		return json.Marshal(types[0])
	}
	return json.Marshal([]string(types))
}

type catalogSnapshot struct {
	Commands []commandSnapshot `json:"commands"`
}

type commandSnapshot struct {
	ID            ID             `json:"id"`
	Disposition   Disposition    `json:"disposition"`
	Help          string         `json:"help"`
	FailureTypes  []string       `json:"failure_types"`
	CLI           cliSnapshot    `json:"cli"`
	Operation     string         `json:"operation"`
	Authorization string         `json:"authorization"`
	Risk          Risk           `json:"risk"`
	DryRun        bool           `json:"dry_run"`
	MCP           *mcpSnapshot   `json:"mcp"`
	InputType     string         `json:"input_type"`
	ResultType    string         `json:"result_type"`
	InputSchema   schemaSnapshot `json:"input_schema"`
	ResultSchema  schemaSnapshot `json:"result_schema"`
}

type cliSnapshot struct {
	Path        string               `json:"path"`
	Aliases     []string             `json:"aliases,omitempty"`
	Positionals []positionalSnapshot `json:"positionals,omitempty"`
	Flags       []flagSnapshot       `json:"flags,omitempty"`
}

type positionalSnapshot struct {
	Name     string `json:"name"`
	Field    string `json:"field"`
	Required bool   `json:"required"`
}

type flagSnapshot struct {
	Name          string `json:"name"`
	Field         string `json:"field"`
	Required      bool   `json:"required"`
	Repeated      bool   `json:"repeated"`
	ContentSource bool   `json:"content_source"`
}

type mcpSnapshot struct {
	Name  string           `json:"name"`
	Hints mcpHintsSnapshot `json:"hints"`
}

type mcpHintsSnapshot struct {
	ReadOnly    bool `json:"read_only"`
	Destructive bool `json:"destructive"`
	Idempotent  bool `json:"idempotent"`
	OpenWorld   bool `json:"open_world"`
}

type schemaSnapshot struct {
	Type                 jsonSchemaType            `json:"type"`
	Format               string                    `json:"format,omitempty"`
	DefaultBool          *bool                     `json:"default,omitempty"`
	Minimum              *int                      `json:"minimum,omitempty"`
	AdditionalProperties *bool                     `json:"additionalProperties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Enum                 []string                  `json:"enum,omitempty"`
	Properties           map[string]schemaSnapshot `json:"properties,omitempty"`
	Items                *schemaSnapshot           `json:"items,omitempty"`
	OneOf                []schemaSnapshot          `json:"oneOf,omitempty"`
	Discriminator        *discriminatorSnapshot    `json:"discriminator,omitempty"`
}

type discriminatorSnapshot struct {
	PropertyName string `json:"propertyName"`
}

// Snapshot returns a deterministic generated snapshot of the complete catalog.
func (catalog Catalog) Snapshot() ([]byte, error) {
	snapshot := catalogSnapshot{Commands: make([]commandSnapshot, len(catalog.definitions))}
	for index, definition := range catalog.definitions {
		failureTypes := make([]string, len(definition.FailureTypes))
		for failureIndex, failureType := range definition.FailureTypes {
			failureTypes[failureIndex] = string(failureType)
		}
		var mcp *mcpSnapshot
		if definition.MCP != nil {
			mcp = &mcpSnapshot{Name: definition.MCP.Name, Hints: mcpHintsSnapshot{
				ReadOnly: definition.MCP.Hints.ReadOnly, Destructive: definition.MCP.Hints.Destructive,
				Idempotent: definition.MCP.Hints.Idempotent, OpenWorld: definition.MCP.Hints.OpenWorld,
			}}
		}
		positionals := make([]positionalSnapshot, len(definition.CLI.Positionals))
		for positionalIndex, positional := range definition.CLI.Positionals {
			positionals[positionalIndex] = positionalSnapshot{Name: positional.Name, Field: positional.Field, Required: positional.Required}
		}
		flags := make([]flagSnapshot, len(definition.CLI.Flags))
		for flagIndex, flag := range definition.CLI.Flags {
			flags[flagIndex] = flagSnapshot{Name: flag.Name, Field: flag.Field, Required: flag.Required,
				Repeated: flag.Repeated, ContentSource: flag.ContentSource}
		}
		snapshot.Commands[index] = commandSnapshot{
			ID: definition.ID, Disposition: definition.Disposition,
			Help: definition.Help, FailureTypes: failureTypes,
			CLI: cliSnapshot{Path: definition.CLI.Path, Aliases: definition.CLI.Aliases,
				Positionals: positionals, Flags: flags},
			Operation: string(definition.Operation), Authorization: definition.Authorization,
			Risk: definition.Risk, DryRun: definition.DryRun, MCP: mcp,
			InputType: definition.InputType.String(), ResultType: definition.ResultType.String(),
			InputSchema: snapshotSchema(definition.InputSchema), ResultSchema: snapshotSchema(definition.ResultSchema),
		}
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func snapshotSchema(schema Schema) schemaSnapshot {
	types := jsonSchemaType{schema.Kind}
	if schema.Nullable {
		types = append(types, "null")
	}
	result := schemaSnapshot{
		Type: types, Format: schema.Format,
		DefaultBool: cloneBoolPointer(schema.DefaultBool), Minimum: cloneIntPointer(schema.Minimum),
	}
	if schema.Kind == "object" {
		additionalProperties := schema.AdditionalProperties
		result.AdditionalProperties = &additionalProperties
		result.Required = append([]string(nil), schema.Required...)
		if len(schema.Properties) > 0 {
			result.Properties = make(map[string]schemaSnapshot, len(schema.Properties))
			for _, property := range schema.Properties {
				result.Properties[property.Name] = snapshotSchema(property.Schema)
			}
		}
	}
	result.Enum = append([]string(nil), schema.Enum...)
	if schema.Items != nil {
		items := snapshotSchema(*schema.Items)
		result.Items = &items
	}
	if len(schema.OneOf) > 0 {
		result.OneOf = make([]schemaSnapshot, len(schema.OneOf))
		for index, branch := range schema.OneOf {
			result.OneOf[index] = snapshotSchema(branch)
		}
	}
	if schema.Discriminator != "" {
		result.Discriminator = &discriminatorSnapshot{PropertyName: schema.Discriminator}
	}
	return result
}
