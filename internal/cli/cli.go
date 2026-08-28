package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

// Application is the public application dispatch seam used by the CLI adapter.
type Application interface {
	Invoke(context.Context, domain.OperationID, any) (any, error)
}

// Config supplies the immutable registry, application service, and process I/O.
type Config struct {
	Catalog     command.Catalog
	Application Application
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	IsTerminal  bool
}

// CLI adapts process arguments and streams to typed application operations.
type CLI struct {
	catalog     command.Catalog
	application Application
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	isTerminal  bool
}

func New(config Config) (*CLI, error) {
	if config.Application == nil || config.Stdin == nil || config.Stdout == nil || config.Stderr == nil {
		return nil, fmt.Errorf("cli: missing dependency")
	}
	if len(config.Catalog.Definitions()) != 24 {
		return nil, fmt.Errorf("cli: invalid command catalog")
	}
	return &CLI{
		catalog: config.Catalog, application: config.Application,
		stdin: config.Stdin, stdout: config.Stdout, stderr: config.Stderr,
		isTerminal: config.IsTerminal,
	}, nil
}

// Run executes one argument vector and returns a stable process exit code.
func (adapter *CLI) Run(ctx context.Context, args []string) int {
	if len(args) == 0 || len(args) == 1 && args[0] == "--help" {
		adapter.writeRootHelp()
		return int(domain.ExitSuccess)
	}
	if len(args) > 0 && args[len(args)-1] == "--help" {
		path := strings.Join(args[:len(args)-1], " ")
		if descriptor, ok := adapter.catalog.LookupCLI(path); ok {
			adapter.writeHelp(descriptor)
			return int(domain.ExitSuccess)
		}
	}
	descriptor, input, options, parseErr := adapter.parse(args)
	if parseErr != nil {
		return adapter.writeError(&domain.Error{Type: domain.ErrorUsageInvalid, Code: "USAGE_INVALID", Message: parseErr.Error()}, options.json)
	}
	result, err := adapter.application.Invoke(ctx, descriptor.Operation, input)
	if err != nil {
		return adapter.writeError(err, options.json)
	}
	publicResult := publicValue(reflect.ValueOf(result))
	if descriptor.Operation == domain.OperationGetRSVP {
		publicResult = map[string]any{"rsvp": publicResult}
	}
	if options.json {
		encoded, err := json.Marshal(publicResult)
		if err != nil {
			return int(domain.ExitInternal)
		}
		fmt.Fprintln(adapter.stdout, string(encoded))
		return int(domain.ExitSuccess)
	}
	if options.plain {
		if err := adapter.writePlain(descriptor, publicResult); err != nil {
			return adapter.writeError(err, false)
		}
		return int(domain.ExitSuccess)
	}
	if object, ok := publicResult.(map[string]any); ok {
		for _, property := range descriptor.ResultSchema.Properties {
			value, exists := object[property.Name]
			if !exists {
				continue
			}
			if isScalar(value) {
				fmt.Fprintf(adapter.stdout, "%s: %v\n", property.Name, value)
				continue
			}
			encoded, _ := json.MarshalIndent(value, "", "  ")
			fmt.Fprintf(adapter.stdout, "%s: %s\n", property.Name, encoded)
		}
	} else {
		fmt.Fprintln(adapter.stdout, publicResult)
	}
	return int(domain.ExitSuccess)
}

func (adapter *CLI) writeRootHelp() {
	fmt.Fprintln(adapter.stdout, "Usage: partiful <command> [flags]")
	fmt.Fprintln(adapter.stdout, "Commands:")
	for _, descriptor := range adapter.catalog.Definitions() {
		fmt.Fprintf(adapter.stdout, "  %s	%s\n", descriptor.CLI.Path, descriptor.Help)
	}
	fmt.Fprintln(adapter.stdout, "Global flags:")
	writeGlobalFlags(adapter.stdout)
}

func (adapter *CLI) writePlain(descriptor command.Descriptor, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return plainUnsupported()
	}
	for _, property := range descriptor.ResultSchema.Properties {
		if property.Schema.Kind != "array" || property.Schema.Items == nil {
			continue
		}
		rows, ok := object[property.Name].([]any)
		if !ok {
			continue
		}
		columns := property.Schema.Items.Required
		if len(columns) == 0 {
			continue
		}
		fmt.Fprintln(adapter.stdout, strings.Join(columns, "	"))
		for _, row := range rows {
			fields, ok := row.(map[string]any)
			if !ok {
				return &domain.Error{Type: domain.ErrorInternalFailure, Code: "INTERNAL_FAILURE", Message: "the operation failed"}
			}
			cells := make([]string, len(columns))
			for index, column := range columns {
				cells[index] = plainCell(fields[column])
			}
			fmt.Fprintln(adapter.stdout, strings.Join(cells, "	"))
		}
		return nil
	}
	return plainUnsupported()
}

func plainUnsupported() error {
	return &domain.Error{Type: domain.ErrorUsageInvalid, Code: "PLAIN_UNSUPPORTED", Message: "plain output is not supported for this command"}
}

func plainCell(value any) string {
	if value == nil {
		return ""
	}
	text := fmt.Sprint(value)
	text = strings.ReplaceAll(text, "	", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.ReplaceAll(text, "\n", " ")
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func publicValue(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return publicValue(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		if value.Type().PkgPath() == "time" {
			return value.Interface()
		}
		result := make(map[string]any)
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			if field.Anonymous {
				if embedded, ok := publicValue(value.Field(index)).(map[string]any); ok {
					for name, item := range embedded {
						result[name] = item
					}
				}
				continue
			}
			result[toSnakeCase(field.Name)] = publicValue(value.Field(index))
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			result[index] = publicValue(value.Index(index))
		}
		return result
	case reflect.Map:
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result[fmt.Sprint(iterator.Key().Interface())] = publicValue(iterator.Value())
		}
		return result
	default:
		return value.Interface()
	}
}

func toSnakeCase(name string) string {
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

func (adapter *CLI) writeHelp(descriptor command.Descriptor) {
	fmt.Fprintf(adapter.stdout, "Usage: partiful %s", descriptor.CLI.Path)
	for _, positional := range descriptor.CLI.Positionals {
		if positional.Required {
			fmt.Fprintf(adapter.stdout, " <%s>", positional.Name)
		} else {
			fmt.Fprintf(adapter.stdout, " [%s]", positional.Name)
		}
	}
	if len(descriptor.CLI.Flags) > 0 {
		fmt.Fprint(adapter.stdout, " [flags]")
	}
	fmt.Fprintf(adapter.stdout, "\n\n%s\n", descriptor.Help)
	fmt.Fprintln(adapter.stdout, "\nFlags:")
	for _, flag := range descriptor.CLI.Flags {
		fmt.Fprintf(adapter.stdout, "  --%s\n", flag.Name)
	}
	writeGlobalFlags(adapter.stdout)
}

func writeGlobalFlags(output io.Writer) {
	fmt.Fprintln(output, "  --json")
	fmt.Fprintln(output, "  --plain")
	fmt.Fprintln(output, "  --no-input")
	fmt.Fprintln(output, "  --version")
}
