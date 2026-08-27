package domain_test

import (
	"reflect"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
)

func TestOperationsAreClosedAndOrdered(t *testing.T) {
	t.Parallel()

	want := []domain.OperationID{
		domain.OperationAuthLoginInteractive,
		domain.OperationGetAuthStatus,
		domain.OperationLogout,
		domain.OperationListEvents,
		domain.OperationGetEvent,
		domain.OperationCreateEvent,
		domain.OperationUpdateEvent,
		domain.OperationCancelEvent,
		domain.OperationListGuests,
		domain.OperationInviteGuest,
		domain.OperationGetRSVP,
		domain.OperationSetRSVP,
		domain.OperationListContacts,
		domain.OperationInviteCohost,
		domain.OperationRevokeCohostInvite,
		domain.OperationRemoveCohost,
		domain.OperationCreateCohostLink,
		domain.OperationRevokeCohostLink,
		domain.OperationSendBlast,
		domain.OperationListPosters,
		domain.OperationSearchPosters,
		domain.OperationGetCommandSchema,
		domain.OperationRunDoctor,
		domain.OperationGetVersion,
	}

	if got := domain.Operations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Operations() = %#v, want %#v", got, want)
	}
	got := domain.Operations()
	got[0] = "modified"
	if domain.Operations()[0] != domain.OperationAuthLoginInteractive {
		t.Fatal("Operations returned mutable shared state")
	}
}

func TestEventReadRSVPValuesAreClosed(t *testing.T) {
	t.Parallel()

	values := domain.EventReadRSVPValues()
	if len(values) != 16 {
		t.Fatalf("EventReadRSVPValues count = %d, want 16", len(values))
	}
	for _, value := range values {
		if !value.Valid() {
			t.Fatalf("settled RSVP value %q is invalid", value)
		}
	}
	if domain.EventReadRSVP("unknown").Valid() {
		t.Fatal("unknown RSVP value is valid")
	}
}
