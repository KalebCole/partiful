package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/KalebCole/partiful/internal/command"
)

type invocationOptions struct {
	json    bool
	plain   bool
	noInput bool
}

func (adapter *CLI) parse(args []string) (command.Descriptor, any, invocationOptions, error) {
	options := invocationOptions{}
	filtered := make([]string, 0, len(args))
	for _, argument := range args {
		switch argument {
		case "--json":
			if options.json {
				return command.Descriptor{}, nil, options, fmt.Errorf("--json was repeated")
			}
			options.json = true
		case "--plain":
			if options.plain {
				return command.Descriptor{}, nil, options, fmt.Errorf("--plain was repeated")
			}
			options.plain = true
		case "--no-input":
			if options.noInput {
				return command.Descriptor{}, nil, options, fmt.Errorf("--no-input was repeated")
			}
			options.noInput = true
		default:
			filtered = append(filtered, argument)
		}
	}
	if options.json && options.plain {
		return command.Descriptor{}, nil, options, fmt.Errorf("--json conflicts with --plain")
	}
	if len(filtered) == 1 && filtered[0] == "--version" {
		filtered[0] = "version"
	}

	descriptor, commandArgs, found := adapter.matchCommand(filtered)
	if !found {
		return command.Descriptor{}, nil, options, fmt.Errorf("unknown command")
	}
	if descriptor.CLI.Path == "auth login" {
		if len(commandArgs) != 0 {
			return command.Descriptor{}, nil, options, fmt.Errorf("auth login takes no arguments")
		}
		if options.noInput || !adapter.isTerminal {
			return command.Descriptor{}, nil, options, fmt.Errorf("auth login requires an interactive terminal")
		}
		return descriptor, struct{}{}, options, nil
	}

	values, err := adapter.parseValues(descriptor, commandArgs)
	if err != nil {
		return command.Descriptor{}, nil, options, err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return command.Descriptor{}, nil, options, fmt.Errorf("encode command input")
	}
	input, err := adapter.catalog.DecodeMCP(descriptor.MCP.Name, encoded)
	if err != nil {
		return command.Descriptor{}, nil, options, err
	}
	return descriptor, input, options, nil
}

func (adapter *CLI) matchCommand(args []string) (command.Descriptor, []string, bool) {
	var matched command.Descriptor
	matchedWords := 0
	for _, descriptor := range adapter.catalog.Definitions() {
		words := strings.Fields(descriptor.CLI.Path)
		if len(words) <= matchedWords || len(args) < len(words) {
			continue
		}
		matches := true
		for index := range words {
			if args[index] != words[index] {
				matches = false
				break
			}
		}
		if matches {
			matched = descriptor
			matchedWords = len(words)
		}
	}
	if matchedWords == 0 {
		return command.Descriptor{}, nil, false
	}
	return matched, args[matchedWords:], true
}

func (adapter *CLI) parseValues(descriptor command.Descriptor, args []string) (map[string]any, error) {
	values := make(map[string]any)
	position := 0
	flags := make(map[string]command.Flag, len(descriptor.CLI.Flags))
	for _, flag := range descriptor.CLI.Flags {
		flags[flag.Name] = flag
	}
	seen := make(map[string]string)
	stdinRead := false

	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") {
			if position >= len(descriptor.CLI.Positionals) {
				return nil, fmt.Errorf("unexpected positional argument")
			}
			positional := descriptor.CLI.Positionals[position]
			values[positional.Field] = argument
			position++
			continue
		}
		name := strings.TrimPrefix(argument, "--")
		flag, ok := flags[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if previous, exists := seen[flag.Field]; exists && (!flag.Repeated || previous != name) {
			return nil, fmt.Errorf("--%s conflicts with or repeats --%s", name, previous)
		}
		seen[flag.Field] = name

		if strings.HasPrefix(name, "clear-") {
			values[flag.Field] = nil
			continue
		}
		property, found := schemaProperty(descriptor.InputSchema, flag.Field)
		if !found {
			return nil, fmt.Errorf("flag --%s has no input schema", name)
		}
		if property.Kind == "boolean" && (index+1 == len(args) || strings.HasPrefix(args[index+1], "--")) {
			values[flag.Field] = true
			continue
		}
		if index+1 == len(args) || strings.HasPrefix(args[index+1], "--") {
			return nil, fmt.Errorf("--%s requires a value", name)
		}
		index++
		raw := args[index]
		var value any = raw
		if flag.ContentSource {
			var content []byte
			var err error
			if raw == "-" {
				if stdinRead {
					return nil, fmt.Errorf("stdin content source was repeated")
				}
				stdinRead = true
				content, err = io.ReadAll(adapter.stdin)
			} else {
				content, err = os.ReadFile(raw)
			}
			if err != nil {
				return nil, fmt.Errorf("read --%s content", name)
			}
			if flag.Field == "questionnaire_response" {
				var object map[string]string
				if err := json.Unmarshal(content, &object); err != nil {
					return nil, fmt.Errorf("--%s must contain a JSON string object", name)
				}
				value = object
			} else {
				value = string(content)
			}
		} else {
			converted, err := parseScalar(property, raw)
			if err != nil {
				return nil, fmt.Errorf("--%s: %w", name, err)
			}
			value = converted
		}
		if flag.Repeated {
			if flag.Field == "links" {
				parts := strings.SplitN(raw, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return nil, fmt.Errorf("--%s requires label=url", name)
				}
				values[flag.Field] = appendMap(values[flag.Field], map[string]any{"label": parts[0], "url": parts[1]})
			} else {
				values[flag.Field] = appendString(values[flag.Field], fmt.Sprint(value))
			}
		} else {
			values[flag.Field] = value
		}
	}
	for ; position < len(descriptor.CLI.Positionals); position++ {
		if descriptor.CLI.Positionals[position].Required {
			return nil, fmt.Errorf("missing required positional %s", descriptor.CLI.Positionals[position].Name)
		}
	}
	if _, exists := values["limit"]; !exists && schemaContains(descriptor.InputSchema, "limit") {
		values["limit"] = 25
	}
	if all, _ := values["all"].(bool); all {
		if _, exists := values["max_items"]; !exists {
			return nil, fmt.Errorf("--all requires --max-items")
		}
	}
	if limit, ok := values["limit"].(int); ok && limit > 100 {
		return nil, fmt.Errorf("--limit must be at most 100")
	}
	if maximum, ok := values["max_items"].(int); ok && maximum > 1000 {
		return nil, fmt.Errorf("--max-items must be at most 1000")
	}
	return values, nil
}

func schemaProperty(schema command.Schema, name string) (command.Schema, bool) {
	for _, property := range schema.Properties {
		if property.Name == name {
			return property.Schema, true
		}
	}
	for _, branch := range schema.OneOf {
		if property, ok := schemaProperty(branch, name); ok {
			return property, true
		}
	}
	return command.Schema{}, false
}

func schemaContains(schema command.Schema, name string) bool {
	_, found := schemaProperty(schema, name)
	return found
}

func parseScalar(schema command.Schema, raw string) (any, error) {
	switch schema.Kind {
	case "integer":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("requires an integer")
		}
		return value, nil
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("requires true or false")
		}
		return value, nil
	default:
		return raw, nil
	}
}

func appendString(current any, value string) []string {
	values, _ := current.([]string)
	return append(values, value)
}

func appendMap(current any, value map[string]any) []map[string]any {
	values, _ := current.([]map[string]any)
	return append(values, value)
}
