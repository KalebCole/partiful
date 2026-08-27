package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
)

// DecodeMCP strictly decodes one MCP argument object to its concrete domain input type.
func (catalog Catalog) DecodeMCP(name string, data []byte) (any, error) {
	definition, ok := catalog.LookupMCP(name)
	if !ok {
		return nil, invalidInput("unknown MCP tool")
	}
	if err := validateJSONValue(definition.InputSchema, data); err != nil {
		return nil, invalidInput(err.Error())
	}
	data, err := applySchemaDefaults(definition.InputSchema, data)
	if err != nil {
		return nil, invalidInput("arguments do not match the command input")
	}
	input, err := definition.decodeInput(data)
	if err != nil {
		return nil, invalidInput("arguments do not match the command input")
	}
	if definition.validateInput != nil {
		if err := definition.validateInput(input); err != nil {
			return nil, invalidInput(err.Error())
		}
	}
	return input, nil
}

func applySchemaDefaults(schema Schema, data []byte) ([]byte, error) {
	if len(schema.OneOf) > 0 {
		for _, branch := range schema.OneOf {
			if validateJSONValue(branch, data) == nil {
				return applySchemaDefaults(branch, data)
			}
		}
	}
	if schema.Kind != "object" {
		return data, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	for _, property := range schema.Properties {
		raw, exists := value[property.Name]
		if !exists {
			if property.Schema.DefaultBool != nil {
				value[property.Name] = json.RawMessage(strconv.FormatBool(*property.Schema.DefaultBool))
			}
			continue
		}
		withDefaults, err := applySchemaDefaults(property.Schema, raw)
		if err != nil {
			return nil, err
		}
		value[property.Name] = withDefaults
	}
	return json.Marshal(value)
}

func decodeInput[I any](data []byte) (any, error) {
	inputType := reflect.TypeOf((*I)(nil)).Elem()
	transformed, err := transformJSONNames(data, inputType)
	if err != nil {
		return nil, err
	}
	var input I
	decoder := json.NewDecoder(bytes.NewReader(transformed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return input, nil
}

func invalidInput(message string) *domain.Error {
	return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_INPUT", Message: message}
}

func validateRSVPInput(value any) error {
	input, ok := value.(domain.SetRSVPInput)
	if !ok {
		return fmt.Errorf("arguments do not match the RSVP input")
	}
	if input.Status == domain.RSVPIntentGoing || input.Status == domain.RSVPIntentNotGoing {
		if input.PartySize == nil || *input.PartySize != 1+len(input.PlusOnes) {
			return fmt.Errorf("party_size must equal one plus the plus_ones count")
		}
	}
	return nil
}

func validateJSONValue(schema Schema, data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if schema.Nullable {
			return nil
		}
		return fmt.Errorf("value cannot be null")
	}
	if len(schema.OneOf) > 0 {
		matches := 0
		for _, branch := range schema.OneOf {
			if validateJSONValue(branch, data) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("value must match exactly one input branch")
		}
		return nil
	}

	switch schema.Kind {
	case "object":
		var objectValue map[string]json.RawMessage
		if err := json.Unmarshal(data, &objectValue); err != nil || objectValue == nil {
			return fmt.Errorf("value must be an object")
		}
		properties := make(map[string]Schema, len(schema.Properties))
		for _, property := range schema.Properties {
			properties[property.Name] = property.Schema
		}
		for name, raw := range objectValue {
			propertySchema, ok := properties[name]
			if !ok {
				return fmt.Errorf("unknown input field %q", name)
			}
			if err := validateJSONValue(propertySchema, raw); err != nil {
				return fmt.Errorf("field %q: %w", name, err)
			}
		}
		for _, required := range schema.Required {
			if _, ok := objectValue[required]; !ok {
				return fmt.Errorf("missing required input field %q", required)
			}
		}
	case "array":
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("value must be an array")
		}
		for _, value := range values {
			if err := validateJSONValue(*schema.Items, value); err != nil {
				return err
			}
		}
	case "string":
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("value must be a string")
		}
		if len(schema.Enum) > 0 && !contains(schema.Enum, value) {
			return fmt.Errorf("value is outside the closed enum")
		}
		if schema.Format == "date-time" {
			if _, err := time.Parse(time.RFC3339, value); err != nil {
				return fmt.Errorf("value must use RFC 3339")
			}
		}
		if schema.Format == "iana-timezone" {
			if _, err := time.LoadLocation(value); err != nil {
				return fmt.Errorf("value must use an IANA timezone")
			}
		}
	case "integer":
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("value must be an integer")
		}
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(number.String(), ".eE") {
			return fmt.Errorf("value must be an integer")
		}
		integer, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return fmt.Errorf("value must be an integer")
		}
		if schema.Minimum != nil && integer < int64(*schema.Minimum) {
			return fmt.Errorf("value is below its minimum")
		}
	case "boolean":
		var value bool
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("value must be a boolean")
		}
	default:
		return fmt.Errorf("unsupported input schema")
	}
	return nil
}

func transformJSONNames(data []byte, target reflect.Type) ([]byte, error) {
	if target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target.Kind() != reflect.Struct || target.PkgPath() == "time" {
		return data, nil
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	transformed := make(map[string]json.RawMessage, len(source))
	for name, raw := range source {
		field, ok := fieldForLogicalName(target, name)
		if !ok {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		value, err := transformFieldValue(raw, field.Type)
		if err != nil {
			return nil, err
		}
		transformed[field.Name] = value
	}
	return json.Marshal(transformed)
}

func transformFieldValue(data []byte, fieldType reflect.Type) ([]byte, error) {
	if fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() == reflect.Struct && fieldType.PkgPath() == reflect.TypeOf(domain.Change[string]{}).PkgPath() && strings.HasPrefix(fieldType.Name(), "Change[") {
		valueField, ok := fieldType.FieldByName("Value")
		if !ok {
			return nil, fmt.Errorf("invalid change type")
		}
		value, err := transformFieldValue(data, valueField.Type)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]json.RawMessage{
			"Set":   json.RawMessage("true"),
			"Value": value,
		})
	}
	if fieldType.Kind() == reflect.Slice {
		return data, nil
	}
	return transformJSONNames(data, fieldType)
}

func fieldForLogicalName(target reflect.Type, logicalName string) (reflect.StructField, bool) {
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		if snakeCase(field.Name) == logicalName {
			return field, true
		}
		if field.Anonymous {
			embedded := field.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				if match, ok := fieldForLogicalName(embedded, logicalName); ok {
					return match, true
				}
			}
		}
	}
	return reflect.StructField{}, false
}

func snakeCase(name string) string {
	var result strings.Builder
	for index, current := range name {
		if index > 0 && current >= 'A' && current <= 'Z' {
			previous := rune(name[index-1])
			nextLower := index+1 < len(name) && name[index+1] >= 'a' && name[index+1] <= 'z'
			if previous >= 'a' && previous <= 'z' || nextLower {
				result.WriteByte('_')
			}
		}
		result.WriteRune(current)
	}
	return strings.ToLower(result.String())
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
