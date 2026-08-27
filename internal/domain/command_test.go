package domain_test

import (
	"reflect"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
)

func TestCommandBoundaryTypesAreTyped(t *testing.T) {
	t.Parallel()

	logout := domain.LogoutInput{DryRun: true}
	if !logout.DryRun {
		t.Fatal("LogoutInput does not retain dry-run")
	}

	resultType := reflect.TypeOf(domain.CommandSchemaResult{})
	for index := 0; index < resultType.NumField(); index++ {
		field := resultType.Field(index)
		if field.Type.Kind() == reflect.Map || field.Type.String() == "json.RawMessage" {
			t.Fatalf("CommandSchemaResult field %s is opaque: %s", field.Name, field.Type)
		}
	}

	version := domain.VersionResult{
		CLIVersion:                "0.1.0",
		CommandContractRevision:   "1",
		TransportContractRevision: "2026-08-12.7",
	}
	if version.CommandContractRevision != "1" {
		t.Fatal("VersionResult does not retain contract revisions")
	}
}
