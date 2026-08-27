package mcp

import "github.com/KalebCole/partiful/internal/command"

func schemaJSON(schema command.Schema) map[string]any {
	result := make(map[string]any)
	if len(schema.OneOf) > 0 {
		branches := make([]any, len(schema.OneOf))
		for index, branch := range schema.OneOf {
			branches[index] = schemaJSON(branch)
		}
		result["oneOf"] = branches
		return result
	}
	if schema.Nullable {
		result["type"] = []string{schema.Kind, "null"}
	} else if schema.Kind != "" {
		result["type"] = schema.Kind
	}
	if schema.Format != "" {
		result["format"] = schema.Format
	}
	if schema.DefaultBool != nil {
		result["default"] = *schema.DefaultBool
	}
	if schema.Minimum != nil {
		result["minimum"] = *schema.Minimum
	}
	if schema.Kind == "object" {
		result["additionalProperties"] = schema.AdditionalProperties
		properties := make(map[string]any, len(schema.Properties))
		for _, property := range schema.Properties {
			properties[property.Name] = schemaJSON(property.Schema)
		}
		result["properties"] = properties
		if len(schema.Required) > 0 {
			result["required"] = append([]string(nil), schema.Required...)
		}
	}
	if len(schema.Enum) > 0 {
		result["enum"] = append([]string(nil), schema.Enum...)
	}
	if schema.Items != nil {
		result["items"] = schemaJSON(*schema.Items)
	}
	return result
}
