package domain

// LogoutInput is the application-visible local credential deletion input.
type LogoutInput struct {
	DryRun bool
}

// CommandSchemaInput optionally selects one canonical command path.
type CommandSchemaInput struct {
	Command *string
}

// CommandSchemaResult is the typed application projection of the command catalog.
type CommandSchemaResult struct {
	Commands        []CommandSchemaCommand
	MCPTools        []string
	CLIOnlyCommands []string
}

// CommandSchemaCommand describes one public command without exposing executable registry state.
type CommandSchemaCommand struct {
	ID            string
	CLIPath       string
	Help          string
	Positionals   []CommandSchemaPositional
	Flags         []CommandSchemaFlag
	InputSchema   CommandSchemaValue
	ResultSchema  CommandSchemaValue
	FailureTypes  []ErrorType
	Authorization string
	Risk          string
	DryRun        bool
	MCP           *CommandSchemaMCP
}

// CommandSchemaPositional describes one ordered CLI argument.
type CommandSchemaPositional struct {
	Name     string
	Required bool
}

// CommandSchemaFlag describes one CLI flag and its logical input mapping.
type CommandSchemaFlag struct {
	Name          string
	Field         string
	Required      bool
	Repeated      bool
	ContentSource bool
}

// CommandSchemaMCP describes the paired MCP tool, when one exists.
type CommandSchemaMCP struct {
	Name  string
	Hints CommandSchemaMCPHints
}

// CommandSchemaMCPHints contains the four settled MCP safety hints.
type CommandSchemaMCPHints struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
}

// CommandSchemaValue is the named, recursive schema projection used by schema results.
type CommandSchemaValue struct {
	Kind                 string
	Nullable             bool
	Format               string
	DefaultBool          *bool
	Minimum              *int
	AdditionalProperties bool
	Required             []string
	Enum                 []string
	Properties           []CommandSchemaProperty
	Items                *CommandSchemaValue
	OneOf                []CommandSchemaValue
	Discriminator        *string
}

// CommandSchemaProperty is one named object property.
type CommandSchemaProperty struct {
	Name   string
	Schema CommandSchemaValue
}

// VersionResult is the public immutable release and contract revision tuple.
type VersionResult struct {
	CLIVersion                string
	CommandContractRevision   string
	TransportContractRevision string
}
