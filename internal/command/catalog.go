package command

import (
	"reflect"

	"github.com/KalebCole/partiful/internal/domain"
)

// ID is the stable command inventory identity.
type ID string

// Disposition controls whether a command also has an MCP tool.
type Disposition string

const (
	Paired  Disposition = "paired"
	CLIOnly Disposition = "cli-only"
)

// Risk is the settled command risk class.
type Risk string

const (
	RiskInteractive       Risk = "interactive"
	RiskCredentialRefresh Risk = "credential-refresh"
	RiskCredentialDelete  Risk = "credential-delete"
	RiskRead              Risk = "read"
	RiskWrite             Risk = "write"
	RiskDestructiveWrite  Risk = "destructive-write"
	RiskDiagnostic        Risk = "diagnostic"
)

// Descriptor is immutable when obtained through Catalog.
type Descriptor struct {
	ID            ID
	Disposition   Disposition
	Help          string
	FailureTypes  []domain.ErrorType
	Operation     domain.OperationID
	Authorization string
	Risk          Risk
	DryRun        bool
	CLI           CLIDescriptor
	MCP           *MCPDescriptor
	InputSchema   Schema
	ResultSchema  Schema
	InputType     reflect.Type
	ResultType    reflect.Type
	decodeInput   func([]byte) (any, error)
	validateInput func(any) error
}

// CLIDescriptor is adapter metadata for one canonical CLI path.
type CLIDescriptor struct {
	Path        string
	Aliases     []string
	Positionals []Positional
	Flags       []Flag
}

// Positional describes one logical positional argument.
type Positional struct {
	Name     string
	Field    string
	Required bool
}

// Flag describes one logical CLI flag.
type Flag struct {
	Name          string
	Field         string
	Required      bool
	Repeated      bool
	ContentSource bool
}

// MCPDescriptor is adapter metadata for one paired tool.
type MCPDescriptor struct {
	Name  string
	Hints MCPHints
}

// MCPHints contains the settled MCP safety annotations.
type MCPHints struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
}

// Schema is the closed JSON-schema subset required by the command catalog.
type Schema struct {
	Kind                 string
	Nullable             bool
	Format               string
	DefaultBool          *bool
	Minimum              *int
	AdditionalProperties bool
	Required             []string
	Enum                 []string
	Properties           []Property
	Items                *Schema
	OneOf                []Schema
	Discriminator        string
}

// Property is one named object property.
type Property struct {
	Name   string
	Schema Schema
}

// Catalog provides ordered iteration and immutable lookups.
type Catalog struct {
	definitions []Descriptor
	byID        map[ID]int
	byCLI       map[string]int
	byOperation map[domain.OperationID]int
	byMCP       map[string]int
}

// Definition binds one descriptor to concrete input and result types.
type Definition[I, O any] struct {
	descriptor Descriptor
}

func define[I, O any](descriptor Descriptor) Definition[I, O] {
	descriptor.InputType = reflect.TypeOf((*I)(nil)).Elem()
	descriptor.ResultType = reflect.TypeOf((*O)(nil)).Elem()
	descriptor.decodeInput = decodeInput[I]
	return Definition[I, O]{descriptor: descriptor}
}

func (definition Definition[I, O]) erased() Descriptor {
	return definition.descriptor
}

// DefaultCatalog constructs and validates the closed first-release catalog.
func DefaultCatalog() (Catalog, error) {
	return NewCatalog(catalogDefinitions())
}

// Definitions returns a deep copy in command-model order.
func (catalog Catalog) Definitions() []Descriptor {
	return cloneDefinitions(catalog.definitions)
}

func base(id ID, disposition Disposition, path string, operation domain.OperationID, authorization string, risk Risk, tool string, hints MCPHints, dryRun bool) Descriptor {
	descriptor := Descriptor{
		ID: id, Disposition: disposition, Operation: operation, Authorization: authorization,
		Risk: risk, DryRun: dryRun, CLI: CLIDescriptor{Path: path},
	}
	if tool != "" {
		descriptor.MCP = &MCPDescriptor{Name: tool, Hints: hints}
	}
	return descriptor
}

func catalogDefinitions() []Descriptor {
	read := MCPHints{ReadOnly: true, Idempotent: true, OpenWorld: true}
	diagnostic := MCPHints{ReadOnly: true, Idempotent: true}
	refresh := MCPHints{Idempotent: true, OpenWorld: true}
	write := MCPHints{OpenWorld: true}
	destructive := MCPHints{Destructive: true, OpenWorld: true}
	localDelete := MCPHints{Destructive: true, Idempotent: true}

	definitions := []Descriptor{
		define[struct{}, domain.AuthState](base("CMD-001", CLIOnly, "auth login", domain.OperationAuthLoginInteractive, "private interactive terminal", RiskInteractive, "", MCPHints{}, false)).erased(),
		define[struct{}, domain.AuthState](base("CMD-002", Paired, "auth status", domain.OperationGetAuthStatus, "local credentials; refresh when required", RiskCredentialRefresh, "auth_status", refresh, false)).erased(),
		define[domain.LogoutInput, domain.AuthState](base("CMD-003", Paired, "auth logout", domain.OperationLogout, "local credential store", RiskCredentialDelete, "auth_logout", localDelete, true)).erased(),
		define[domain.ListEventsInput, domain.EventsResult](base("CMD-004", Paired, "events list", domain.OperationListEvents, "authenticated account", RiskRead, "events_list", read, false)).erased(),
		define[domain.GetEventInput, domain.EventResult](base("CMD-005", Paired, "events get", domain.OperationGetEvent, "authenticated account; backend decides access", RiskRead, "events_get", read, false)).erased(),
		define[domain.CreateEventInput, domain.SubmittedResult](base("CMD-006", Paired, "events create", domain.OperationCreateEvent, "authenticated account; backend decides access", RiskWrite, "events_create", write, true)).erased(),
		define[domain.UpdateEventInput, domain.UpdateEventResult](base("CMD-007", Paired, "events update", domain.OperationUpdateEvent, "authenticated account; backend decides access", RiskDestructiveWrite, "events_update", destructive, true)).erased(),
		define[domain.CancelEventInput, domain.CancelEventResult](base("CMD-008", Paired, "events cancel", domain.OperationCancelEvent, "authenticated account; backend decides access", RiskDestructiveWrite, "events_cancel", destructive, true)).erased(),
		define[domain.ListGuestsInput, domain.GuestsResult](base("CMD-009", Paired, "guests list", domain.OperationListGuests, "authenticated account; backend decides access", RiskRead, "guests_list", read, false)).erased(),
		define[domain.InviteGuestInput, domain.SubmittedResult](base("CMD-010", Paired, "guests invite", domain.OperationInviteGuest, "authenticated account; backend decides access", RiskWrite, "guests_invite", write, true)).erased(),
		define[domain.GetEventInput, domain.RSVPResult](base("CMD-011", Paired, "rsvp get", domain.OperationGetRSVP, "authenticated account; backend decides access", RiskRead, "rsvp_get", read, false)).erased(),
		define[domain.SetRSVPInput, domain.SetRSVPResult](base("CMD-012", Paired, "rsvp set", domain.OperationSetRSVP, "authenticated account; backend decides access", RiskDestructiveWrite, "rsvp_set", destructive, true)).erased(),
		define[domain.ListContactsInput, domain.ContactsResult](base("CMD-013", Paired, "contacts list", domain.OperationListContacts, "authenticated account", RiskRead, "contacts_list", read, false)).erased(),
		define[domain.CohostInput, domain.CohostResult](base("CMD-014", Paired, "cohosts invite", domain.OperationInviteCohost, "authenticated account; backend decides access", RiskWrite, "cohosts_invite", write, true)).erased(),
		define[domain.CohostInput, domain.CohostResult](base("CMD-015", Paired, "cohosts revoke-invite", domain.OperationRevokeCohostInvite, "authenticated account; backend decides access", RiskDestructiveWrite, "cohosts_revoke_invite", destructive, true)).erased(),
		define[domain.CohostInput, domain.CohostResult](base("CMD-016", Paired, "cohosts remove", domain.OperationRemoveCohost, "authenticated account; backend decides access", RiskDestructiveWrite, "cohosts_remove", destructive, true)).erased(),
		define[domain.CohostLinkInput, domain.CohostLinkResult](base("CMD-017", Paired, "cohosts link create", domain.OperationCreateCohostLink, "authenticated account; backend decides access", RiskWrite, "cohosts_link_create", write, true)).erased(),
		define[domain.CohostLinkInput, domain.CohostLinkResult](base("CMD-018", Paired, "cohosts link revoke", domain.OperationRevokeCohostLink, "authenticated account; backend decides access", RiskDestructiveWrite, "cohosts_link_revoke", destructive, true)).erased(),
		define[domain.SendBlastInput, domain.BlastResult](base("CMD-019", Paired, "blasts send", domain.OperationSendBlast, "authenticated account; backend decides access", RiskDestructiveWrite, "blasts_send", destructive, true)).erased(),
		define[domain.ListPostersInput, domain.PostersResult](base("CMD-020", Paired, "posters list", domain.OperationListPosters, "none", RiskRead, "posters_list", read, false)).erased(),
		define[domain.SearchPostersInput, domain.PostersResult](base("CMD-021", Paired, "posters search", domain.OperationSearchPosters, "none", RiskRead, "posters_search", read, false)).erased(),
		define[domain.CommandSchemaInput, domain.CommandSchemaResult](base("CMD-022", Paired, "schema", domain.OperationGetCommandSchema, "none", RiskDiagnostic, "schema", diagnostic, false)).erased(),
		define[struct{}, domain.DoctorResult](base("CMD-023", Paired, "doctor", domain.OperationRunDoctor, "none; credentials are inspected but not printed", RiskDiagnostic, "doctor", diagnostic, false)).erased(),
		define[struct{}, domain.VersionResult](base("CMD-024", Paired, "version", domain.OperationGetVersion, "none", RiskDiagnostic, "version", diagnostic, false)).erased(),
	}
	applySchemas(definitions)
	applyAdapters(definitions)
	applyMetadata(definitions)
	return definitions
}

func cloneDefinitions(source []Descriptor) []Descriptor {
	result := make([]Descriptor, len(source))
	for index, definition := range source {
		result[index] = cloneDescriptor(definition)
	}
	return result
}

func cloneDescriptor(definition Descriptor) Descriptor {
	copy := definition
	copy.CLI.Aliases = append([]string(nil), definition.CLI.Aliases...)
	copy.CLI.Positionals = append([]Positional(nil), definition.CLI.Positionals...)
	copy.CLI.Flags = append([]Flag(nil), definition.CLI.Flags...)
	copy.FailureTypes = append([]domain.ErrorType(nil), definition.FailureTypes...)
	if definition.MCP != nil {
		mcp := *definition.MCP
		copy.MCP = &mcp
	}
	copy.InputSchema = cloneSchema(definition.InputSchema)
	copy.ResultSchema = cloneSchema(definition.ResultSchema)
	return copy
}

func cloneSchema(schema Schema) Schema {
	copy := schema
	copy.DefaultBool = cloneBoolPointer(schema.DefaultBool)
	copy.Minimum = cloneIntPointer(schema.Minimum)
	copy.Required = append([]string(nil), schema.Required...)
	copy.Enum = append([]string(nil), schema.Enum...)
	copy.Properties = make([]Property, len(schema.Properties))
	for index, property := range schema.Properties {
		copy.Properties[index] = Property{Name: property.Name, Schema: cloneSchema(property.Schema)}
	}
	if schema.Items != nil {
		items := cloneSchema(*schema.Items)
		copy.Items = &items
	}
	copy.OneOf = make([]Schema, len(schema.OneOf))
	for index := range schema.OneOf {
		copy.OneOf[index] = cloneSchema(schema.OneOf[index])
	}
	return copy
}
