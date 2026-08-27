package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

var blastRequiredGates = []string{
	"OP11-GET-EVENT-REQUEST",
	"OP11-BLAST-FIRESTORE-READS",
	"OP11-BLAST-GROUPS",
}

var blastReadClaimGates = []string{
	"OP11-ENDPOINT-ERRORS:getEventInfo",
	"OP11-ENDPOINT-ERRORS:firestoreGetEvent",
	"OP11-ENDPOINT-ERRORS:firestoreListDocuments",
}

// BlastCredentialProvider acquires the private credential used by blast transports.
type BlastCredentialProvider interface {
	Acquire(context.Context) (auth.Authorization, error)
}

// BindBlastOperation installs the gated, one-attempt all-guests blast workflow.
func BindBlastOperation(service *Service, credentials BlastCredentialProvider, callable transport.CallableTransport, firestore transport.FirestoreTransport) error {
	if service == nil || credentials == nil || callable == nil || firestore == nil {
		return fmt.Errorf("bind blast operation: missing dependency")
	}
	for _, identity := range blastReadClaimGates {
		if _, found := service.gates.Lookup(identity); !found {
			return fmt.Errorf("bind blast operation: missing gate %q", identity)
		}
	}
	workflow := blastWorkflow{service: service, credentials: credentials, callable: callable, firestore: firestore}
	return BindOperation(service, OperationSpec[domain.SendBlastInput, domain.BlastResult]{
		Operation:     domain.OperationSendBlast,
		RequiredGates: blastRequiredGates,
		ErrorGate:     "OP11-ENDPOINT-ERRORS:createTextBlast",
		OutcomeGate:   "OP11-MUTATION-OUTCOME:createTextBlast",
		Execute:       workflow.send,
	})
}

type blastWorkflow struct {
	service     *Service
	credentials BlastCredentialProvider
	callable    transport.CallableTransport
	firestore   transport.FirestoreTransport
}

func (workflow blastWorkflow) send(ctx context.Context, invocation *Invocation, input domain.SendBlastInput) (domain.BlastResult, error) {
	result := domain.BlastResult{
		EventID:         input.EventID,
		Audience:        input.Audience,
		ShowOnEventPage: input.ShowOnEventPage,
		RecipientStatus: "not-reported",
	}
	if err := validateBlastInput(input); err != nil {
		return result, err
	}
	if input.DryRun {
		return result, nil
	}
	authorization, err := workflow.credentials.Acquire(ctx)
	if err != nil {
		return domain.BlastResult{}, err
	}
	credential := transport.Credential(authorization.AccessToken.Reveal())
	if credential == "" {
		return domain.BlastResult{}, &domain.Error{Type: domain.ErrorAuthRequired, Code: "AUTH_REQUIRED", Message: "authentication is required"}
	}
	eventID := transport.EventID(input.EventID)
	event, err := workflow.callable.GetEvent(ctx, transport.GetEventRequest{Credential: credential, EventID: eventID})
	if err != nil {
		return domain.BlastResult{}, workflow.guardReadClaim("OP11-ENDPOINT-ERRORS:getEventInfo", err)
	}
	if event.Event.EventID != eventID {
		return domain.BlastResult{}, protocolChangedBlast()
	}
	eventDocument, err := workflow.firestore.GetEvent(ctx, transport.GetEventDocumentRequest{Credential: credential, EventID: eventID})
	if err != nil {
		return domain.BlastResult{}, workflow.guardReadClaim("OP11-ENDPOINT-ERRORS:firestoreGetEvent", err)
	}
	if eventDocument.EventID != eventID {
		return domain.BlastResult{}, protocolChangedBlast()
	}
	guests, err := workflow.listGuests(ctx, credential, eventID)
	if err != nil {
		return domain.BlastResult{}, err
	}
	if _, err := workflow.listTextBlasts(ctx, credential, eventID); err != nil {
		return domain.BlastResult{}, err
	}
	if event.Event.Start != nil && !event.Event.Start.After(time.Now()) {
		return domain.BlastResult{}, &domain.Error{Type: domain.ErrorStateConflict, Code: "EVENT_EXPIRED", Message: "the event has expired"}
	}
	groups := ordinaryBlastGroups(guests)
	if len(groups) == 0 {
		return domain.BlastResult{}, &domain.Error{Type: domain.ErrorStateConflict, Code: "NO_BLAST_RECIPIENTS", Message: "the event has no eligible blast recipients"}
	}
	_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return workflow.callable.CreateTextBlast(ctx, transport.CreateTextBlastRequest{
			Credential: credential, EventID: eventID, Message: input.Message,
			ShowOnEventPage: input.ShowOnEventPage, Groups: groups,
		})
	})
	if err != nil {
		return domain.BlastResult{}, err
	}
	result.Submitted = true
	return result, nil
}

func (workflow blastWorkflow) listGuests(ctx context.Context, credential transport.Credential, eventID transport.EventID) ([]transport.GuestDocument, error) {
	var documents []transport.GuestDocument
	var cursor *transport.RemoteCursor
	seen := map[transport.RemoteCursor]struct{}{}
	for {
		page, err := workflow.firestore.ListEventGuests(ctx, transport.ListEventDocumentsRequest{Credential: credential, EventID: eventID, Cursor: cursor})
		if err != nil {
			return nil, workflow.guardReadClaim("OP11-ENDPOINT-ERRORS:firestoreListDocuments", err)
		}
		documents = append(documents, page.Documents...)
		if page.Cursor == nil {
			return documents, nil
		}
		if *page.Cursor == "" {
			return nil, protocolChangedBlast()
		}
		if _, duplicate := seen[*page.Cursor]; duplicate {
			return nil, protocolChangedBlast()
		}
		seen[*page.Cursor] = struct{}{}
		cursor = page.Cursor
	}
}

func (workflow blastWorkflow) listTextBlasts(ctx context.Context, credential transport.Credential, eventID transport.EventID) (int, error) {
	count := 0
	var cursor *transport.RemoteCursor
	seen := map[transport.RemoteCursor]struct{}{}
	for {
		page, err := workflow.firestore.ListEventHostMessages(ctx, transport.ListEventDocumentsRequest{Credential: credential, EventID: eventID, Cursor: cursor})
		if err != nil {
			return 0, workflow.guardReadClaim("OP11-ENDPOINT-ERRORS:firestoreListDocuments", err)
		}
		for _, document := range page.Documents {
			if document.Kind == nil || *document.Kind == "TEXT_BLAST" {
				count++
			}
		}
		if page.Cursor == nil {
			return count, nil
		}
		if *page.Cursor == "" {
			return 0, protocolChangedBlast()
		}
		if _, duplicate := seen[*page.Cursor]; duplicate {
			return 0, protocolChangedBlast()
		}
		seen[*page.Cursor] = struct{}{}
		cursor = page.Cursor
	}
}

func ordinaryBlastGroups(guests []transport.GuestDocument) []transport.TextBlastGroup {
	orderedNames := []string{"invited", "checkedIn", "GOING", "MAYBE", "DECLINED", "WAITLIST"}
	byName := make(map[string][]transport.GuestID, len(orderedNames))
	for _, guest := range guests {
		if guest.GuestID == "" {
			continue
		}
		if guest.Status != nil {
			switch *guest.Status {
			case "READY_TO_SEND", "SENDING", "SENT", "SEND_ERROR", "DELIVERY_ERROR":
				byName["invited"] = append(byName["invited"], guest.GuestID)
			case "GOING", "MAYBE", "DECLINED", "WAITLIST":
				byName[*guest.Status] = append(byName[*guest.Status], guest.GuestID)
			}
		}
		if guest.CheckIn != nil {
			byName["checkedIn"] = append(byName["checkedIn"], guest.GuestID)
		}
	}
	groups := make([]transport.TextBlastGroup, 0, len(orderedNames))
	for _, name := range orderedNames {
		guestIDs := byName[name]
		if len(guestIDs) == 0 || (name == "invited" && len(guestIDs) > 100) {
			continue
		}
		groups = append(groups, transport.TextBlastGroup{Name: name, GuestIDs: guestIDs})
	}
	return groups
}

func validateBlastInput(input domain.SendBlastInput) error {
	if strings.TrimSpace(string(input.EventID)) == "" || input.Audience != "all-guests" || strings.TrimSpace(input.Message) == "" || len([]rune(input.Message)) > 480 {
		return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_BLAST_INPUT", Message: "blast operation input is invalid"}
	}
	return nil
}

func protocolChangedBlast() error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "PROTOCOL_CHANGED", Message: "the remote blast inputs cannot be interpreted safely"}
}

func (workflow blastWorkflow) guardReadClaim(identity string, err error) error {
	if err == nil || workflow.service.gates.Allows(identity) || hasEvidencedClassification(err) {
		return err
	}
	return evidenceClaimOpen()
}
