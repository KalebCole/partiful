package command_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

func TestCatalogHasExactImmutableInventory(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	definitions := catalog.Definitions()
	if len(definitions) != 24 {
		t.Fatalf("definition count = %d, want 24", len(definitions))
	}

	wantIDs := make([]command.ID, 24)
	for index := range wantIDs {
		wantIDs[index] = command.ID("CMD-" + []string{
			"001", "002", "003", "004", "005", "006", "007", "008",
			"009", "010", "011", "012", "013", "014", "015", "016",
			"017", "018", "019", "020", "021", "022", "023", "024",
		}[index])
	}
	gotIDs := make([]command.ID, len(definitions))
	toolCount := 0
	for index, definition := range definitions {
		gotIDs[index] = definition.ID
		if definition.MCP != nil {
			toolCount++
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("IDs = %#v, want %#v", gotIDs, wantIDs)
	}
	if toolCount != 23 {
		t.Fatalf("paired MCP tool count = %d, want 23", toolCount)
	}
	if definitions[0].CLI.Path != "auth login" || definitions[0].MCP != nil {
		t.Fatalf("first definition = %#v, want auth.login CLI-only", definitions[0])
	}

	definitions[0].CLI.Path = "modified"
	fresh := catalog.Definitions()
	if fresh[0].CLI.Path != "auth login" {
		t.Fatal("Definitions returned mutable catalog state")
	}
}

func TestCatalogDerivesSettledRiskAndStrictSchemas(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	definitions := catalog.Definitions()

	var dryRunPaths []string
	for _, definition := range definitions {
		assertStrictObjectSchema(t, definition.CLI.Path+" input", definition.InputSchema)
		assertStrictObjectSchema(t, definition.CLI.Path+" result", definition.ResultSchema)
		if definition.DryRun {
			dryRunPaths = append(dryRunPaths, definition.CLI.Path)
			if !hasProperty(definition.InputSchema, "dry_run") {
				t.Errorf("%s supports dry-run without a dry_run input field", definition.CLI.Path)
			}
		}
		if definition.Risk == command.RiskRead || definition.Risk == command.RiskDiagnostic {
			if definition.DryRun {
				t.Errorf("%s is a read/diagnostic with dry-run", definition.CLI.Path)
			}
		}
	}

	sort.Strings(dryRunPaths)
	wantDryRunPaths := []string{
		"auth logout", "blasts send", "cohosts invite", "cohosts link create",
		"cohosts link revoke", "cohosts remove", "cohosts revoke-invite",
		"events cancel", "events create", "events update", "guests invite", "rsvp set",
	}
	sort.Strings(wantDryRunPaths)
	if !reflect.DeepEqual(dryRunPaths, wantDryRunPaths) {
		t.Fatalf("dry-run paths = %#v, want %#v", dryRunPaths, wantDryRunPaths)
	}
}

func assertStrictObjectSchema(t *testing.T, name string, schema command.Schema) {
	t.Helper()
	if schema.Kind != "object" {
		t.Errorf("%s kind = %q, want object", name, schema.Kind)
	}
	if schema.AdditionalProperties {
		t.Errorf("%s permits additional properties", name)
	}
	for _, property := range schema.Properties {
		assertNestedSchemasStrict(t, name+"."+property.Name, property.Schema)
	}
	for index, branch := range schema.OneOf {
		assertNestedSchemasStrict(t, name+".oneOf", branch)
		if branch.Kind == "object" && branch.AdditionalProperties {
			t.Errorf("%s oneOf[%d] permits additional properties", name, index)
		}
	}
}

func assertNestedSchemasStrict(t *testing.T, name string, schema command.Schema) {
	t.Helper()
	if schema.Kind == "object" && schema.AdditionalProperties {
		t.Errorf("%s permits additional properties", name)
	}
	for _, property := range schema.Properties {
		assertNestedSchemasStrict(t, name+"."+property.Name, property.Schema)
	}
	if schema.Items != nil {
		assertNestedSchemasStrict(t, name+"[]", *schema.Items)
	}
	for _, branch := range schema.OneOf {
		assertNestedSchemasStrict(t, name+".oneOf", branch)
	}
}

func hasProperty(schema command.Schema, name string) bool {
	for _, property := range schema.Properties {
		if property.Name == name {
			return true
		}
	}
	for _, branch := range schema.OneOf {
		if hasProperty(branch, name) {
			return true
		}
	}
	return false
}

func TestAdapterDescriptorsPreserveLogicalContentParity(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}

	want := map[string]map[string]string{
		"guests invite": {"message-file": "message"},
		"rsvp set": {
			"message-file":                "message",
			"questionnaire-response-file": "questionnaire_response",
		},
		"blasts send": {"message-file": "message"},
	}
	for _, definition := range catalog.Definitions() {
		expected, relevant := want[definition.CLI.Path]
		if !relevant {
			continue
		}
		got := make(map[string]string)
		for _, flag := range definition.CLI.Flags {
			if flag.ContentSource {
				got[flag.Name] = flag.Field
				if !hasProperty(definition.InputSchema, flag.Field) {
					t.Errorf("%s --%s maps to missing logical field %s", definition.CLI.Path, flag.Name, flag.Field)
				}
				if hasProperty(definition.InputSchema, flag.Name) {
					t.Errorf("%s leaks CLI content-source field %s into MCP input", definition.CLI.Path, flag.Name)
				}
			}
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s content mappings = %#v, want %#v", definition.CLI.Path, got, expected)
		}
	}
}

func TestCatalogEncodesContactSelectorAndFourRSVPBranches(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	definitions := catalog.Definitions()

	for _, index := range []int{9, 13, 14, 15} {
		schema := definitions[index].InputSchema
		if len(schema.OneOf) != 2 {
			t.Fatalf("%s selector branch count = %d, want 2", definitions[index].CLI.Path, len(schema.OneOf))
		}
		if !hasProperty(schema.OneOf[0], "contact_ref") || hasProperty(schema.OneOf[0], "contact") {
			t.Errorf("%s first selector branch is not contact_ref-only", definitions[index].CLI.Path)
		}
		if !hasProperty(schema.OneOf[1], "contact") || hasProperty(schema.OneOf[1], "contact_ref") {
			t.Errorf("%s second selector branch is not contact-only", definitions[index].CLI.Path)
		}
	}

	rsvp := definitions[11].InputSchema
	if rsvp.Discriminator != "status" || len(rsvp.OneOf) != 4 {
		t.Fatalf("rsvp.set discriminator/branches = %q/%d, want status/4", rsvp.Discriminator, len(rsvp.OneOf))
	}
	wantStatuses := []string{"going", "not-going", "interested", "not-interested"}
	for index, branch := range rsvp.OneOf {
		status, ok := property(branch, "status")
		if !ok || !reflect.DeepEqual(status.Enum, []string{wantStatuses[index]}) {
			t.Errorf("rsvp.set branch %d status = %#v, want %q", index, status.Enum, wantStatuses[index])
		}
		profile := hasProperty(branch, "display_name")
		if index < 2 && !profile {
			t.Errorf("rsvp.set branch %q lacks RSVP profile", wantStatuses[index])
		}
		if index >= 2 && profile {
			t.Errorf("rsvp.set branch %q accepts RSVP profile", wantStatuses[index])
		}
	}
}

func property(schema command.Schema, name string) (command.Schema, bool) {
	for _, candidate := range schema.Properties {
		if candidate.Name == name {
			return candidate.Schema, true
		}
	}
	return command.Schema{}, false
}

func TestCatalogLookupIndexesAndConstructionValidation(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	want := catalog.Definitions()[4]
	lookups := []struct {
		name string
		get  func() (command.Descriptor, bool)
	}{
		{"id", func() (command.Descriptor, bool) { return catalog.LookupID("CMD-005") }},
		{"cli path", func() (command.Descriptor, bool) { return catalog.LookupCLI("events get") }},
		{"operation", func() (command.Descriptor, bool) { return catalog.LookupOperation(domain.OperationGetEvent) }},
		{"mcp tool", func() (command.Descriptor, bool) { return catalog.LookupMCP("events_get") }},
	}
	for _, lookup := range lookups {
		got, ok := lookup.get()
		if !ok || got.ID != want.ID {
			t.Errorf("Lookup %s = %q, %t, want %q, true", lookup.name, got.ID, ok, want.ID)
		}
	}
	if _, ok := catalog.LookupMCP("auth_login"); ok {
		t.Error("LookupMCP(auth_login) succeeded for CLI-only command")
	}

	invalid := catalog.Definitions()
	invalid[1].ID = invalid[0].ID
	if _, err := command.NewCatalog(invalid); err == nil {
		t.Fatal("NewCatalog() accepted duplicate command ID")
	}
}

func TestCatalogProjectsTypedSchemaResult(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	all, err := catalog.Project(nil)
	if err != nil {
		t.Fatalf("Project(nil) error = %v", err)
	}
	if len(all.Commands) != 24 || len(all.MCPTools) != 23 || !reflect.DeepEqual(all.CLIOnlyCommands, []string{"auth login"}) {
		t.Fatalf("Project(nil) cardinality = commands:%d tools:%d cli-only:%v", len(all.Commands), len(all.MCPTools), all.CLIOnlyCommands)
	}
	if all.Commands[0].Help == "" {
		t.Fatal("Project(nil) omitted command help")
	}

	path := "events get"
	one, err := catalog.Project(&path)
	if err != nil {
		t.Fatalf("Project(events get) error = %v", err)
	}
	if len(one.Commands) != 1 || one.Commands[0].ID != "CMD-005" || one.Commands[0].CLIPath != path {
		t.Fatalf("Project(events get) = %#v", one.Commands)
	}
	if one.Commands[0].InputSchema.AdditionalProperties {
		t.Error("projected input schema permits additional properties")
	}

	missing := "events missing"
	if _, err := catalog.Project(&missing); err == nil {
		t.Fatal("Project(events missing) succeeded")
	}
}

func TestMCPDecoderReturnsConcreteInputAndRejectsUnknownMembers(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	decoded, err := catalog.DecodeMCP("guests_invite", []byte(`{"event_id":"event_redacted","contact_ref":"contact_redacted","message":"content_redacted"}`))
	if err != nil {
		t.Fatalf("DecodeMCP(valid) error = %v", err)
	}
	input, ok := decoded.(domain.InviteGuestInput)
	if !ok {
		t.Fatalf("DecodeMCP(valid) type = %T, want domain.InviteGuestInput", decoded)
	}
	if input.EventID != "event_redacted" || input.ContactRef == nil || *input.ContactRef != "contact_redacted" || input.Message == nil || *input.Message != "content_redacted" || input.DryRun {
		t.Error("DecodeMCP(valid) did not preserve the sanitized logical input")
	}

	_, err = catalog.DecodeMCP("guests_invite", []byte(`{"event_id":"event_redacted","contact_ref":"contact_redacted","message_file":"path_redacted","dry_run":true}`))
	var publicError *domain.Error
	if !errors.As(err, &publicError) || publicError.Type != domain.ErrorInputInvalid {
		t.Fatalf("DecodeMCP(unknown) error = %#v, want input.invalid", err)
	}

	_, err = catalog.DecodeMCP("rsvp_set", []byte(`{"event_id":"event_redacted","status":"going","display_name":"name_redacted","party_size":0,"timezone":"America/Los_Angeles"}`))
	if !errors.As(err, &publicError) || publicError.Type != domain.ErrorInputInvalid {
		t.Fatalf("DecodeMCP(out-of-range) error type = %T, want input.invalid", err)
	}
}

func TestGeneratedCatalogSchemaSnapshot(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	got, err := catalog.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("MkdirAll(testdata) error = %v", err)
		}
		if err := os.WriteFile("testdata/catalog.golden.json", got, 0o644); err != nil {
			t.Fatalf("WriteFile(snapshot) error = %v", err)
		}
	}
	want, err := os.ReadFile("testdata/catalog.golden.json")
	if err != nil {
		t.Fatalf("ReadFile(snapshot) error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("generated catalog schema differs from testdata/catalog.golden.json")
	}
}

func TestCatalogProvidesHelpAndFailureMetadataWithoutDormantUpload(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	for _, definition := range catalog.Definitions() {
		if definition.Help == "" {
			t.Errorf("%s has empty help", definition.ID)
		}
		if len(definition.FailureTypes) == 0 {
			t.Errorf("%s has no failure types", definition.ID)
		}
		if strings.Contains(definition.CLI.Path, "upload") {
			t.Errorf("%s exposes dormant OP11-UPLOAD-PHOTO", definition.ID)
		}
	}
}

func TestUpdateEventSchemaContainsOnlySettledMutableFields(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	definition, ok := catalog.LookupCLI("events update")
	if !ok {
		t.Fatal("events update missing")
	}
	got := make([]string, len(definition.InputSchema.Properties))
	for index, property := range definition.InputSchema.Properties {
		got[index] = property.Name
	}
	sort.Strings(got)
	want := []string{"description", "dry_run", "end", "event_id", "guest_limit", "links", "poster_id", "start", "timezone", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events.update properties = %v, want %v", got, want)
	}
}

func TestSchemasCarryDefaultsFormatsAndBounds(t *testing.T) {
	t.Parallel()

	definitions, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	cancel, _ := definitions.LookupCLI("events cancel")
	notify, _ := property(cancel.InputSchema, "notify_guests")
	if notify.DefaultBool == nil || !*notify.DefaultBool {
		t.Fatal("events.cancel notify_guests must default true")
	}
	blast, _ := definitions.LookupCLI("blasts send")
	show, _ := property(blast.InputSchema, "show_on_event_page")
	if show.DefaultBool == nil || *show.DefaultBool {
		t.Fatal("blasts.send show_on_event_page must default false")
	}
	rsvp, _ := definitions.LookupCLI("rsvp set")
	for _, branch := range rsvp.InputSchema.OneOf[:2] {
		partySize, _ := property(branch, "party_size")
		timezone, _ := property(branch, "timezone")
		if partySize.Minimum == nil || *partySize.Minimum != 1 || timezone.Format != "iana-timezone" {
			t.Fatal("RSVP profile constraints are incomplete")
		}
	}
}

func TestMCPDecoderMapsNullableUpdateFieldsToTypedChanges(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	decoded, err := catalog.DecodeMCP("events_update", []byte(`{"event_id":"event_redacted","title":"title_redacted","description":null}`))
	if err != nil {
		t.Fatalf("DecodeMCP(events_update) error = %v", err)
	}
	input, ok := decoded.(domain.UpdateEventInput)
	if !ok {
		t.Fatalf("DecodeMCP(events_update) type = %T, want domain.UpdateEventInput", decoded)
	}
	if !input.Title.Set || input.Title.Value == nil || *input.Title.Value != "title_redacted" || !input.Description.Set || input.Description.Value != nil {
		t.Fatal("events.update logical setters did not map to typed changes")
	}
}

func TestMCPDecoderAppliesSchemaDefaults(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	decoded, err := catalog.DecodeMCP("events_cancel", []byte(`{"event_id":"event_redacted"}`))
	if err != nil {
		t.Fatalf("DecodeMCP(events_cancel) error = %v", err)
	}
	input, ok := decoded.(domain.CancelEventInput)
	if !ok || !input.NotifyGuests || input.DryRun {
		t.Fatal("events.cancel schema defaults did not reach the typed input")
	}
}

func TestCLIRequiredFlagsFollowSettledContract(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	expected := map[string][]string{
		"events list":    {"when"},
		"events create":  {"start", "timezone", "title"},
		"rsvp set":       {"status"},
		"blasts send":    {"audience", "message-file"},
		"posters search": {"query"},
	}
	for path, want := range expected {
		definition, _ := catalog.LookupCLI(path)
		var got []string
		for _, flag := range definition.CLI.Flags {
			if flag.Required {
				got = append(got, flag.Name)
			}
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s required flags = %v, want %v", path, got, want)
		}
	}
}

func TestMCPDecoderBuildsRSVPProfilesAndEnforcesPartySize(t *testing.T) {
	t.Parallel()

	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog() error = %v", err)
	}
	decoded, err := catalog.DecodeMCP("rsvp_set", []byte(`{"event_id":"event_redacted","status":"going","display_name":"name_redacted","party_size":2,"plus_ones":["guest_redacted"],"timezone":"America/Los_Angeles"}`))
	if err != nil {
		t.Fatalf("DecodeMCP(rsvp_set going) error = %v", err)
	}
	input, ok := decoded.(domain.SetRSVPInput)
	if !ok || input.PartySize == nil || *input.PartySize != 2 || len(input.PlusOnes) != 1 {
		t.Fatal("rsvp.set going branch did not build the typed profile")
	}
	_, err = catalog.DecodeMCP("rsvp_set", []byte(`{"event_id":"event_redacted","status":"going","display_name":"name_redacted","party_size":1,"plus_ones":["guest_redacted"],"timezone":"America/Los_Angeles"}`))
	var publicError *domain.Error
	if !errors.As(err, &publicError) || publicError.Type != domain.ErrorInputInvalid {
		t.Fatalf("DecodeMCP(rsvp_set mismatched party) error type = %T, want input.invalid", err)
	}
}
