package domain

// OperationID identifies one shared application operation.
type OperationID string

const (
	OperationAuthLoginInteractive OperationID = "AuthLoginInteractive"
	OperationGetAuthStatus        OperationID = "GetAuthStatus"
	OperationLogout               OperationID = "Logout"
	OperationListEvents           OperationID = "ListEvents"
	OperationGetEvent             OperationID = "GetEvent"
	OperationCreateEvent          OperationID = "CreateEvent"
	OperationUpdateEvent          OperationID = "UpdateEvent"
	OperationCancelEvent          OperationID = "CancelEvent"
	OperationListGuests           OperationID = "ListGuests"
	OperationInviteGuest          OperationID = "InviteGuest"
	OperationGetRSVP              OperationID = "GetRsvp"
	OperationSetRSVP              OperationID = "SetRsvp"
	OperationListContacts         OperationID = "ListContacts"
	OperationInviteCohost         OperationID = "InviteCohost"
	OperationRevokeCohostInvite   OperationID = "RevokeCohostInvite"
	OperationRemoveCohost         OperationID = "RemoveCohost"
	OperationCreateCohostLink     OperationID = "CreateCohostLink"
	OperationRevokeCohostLink     OperationID = "RevokeCohostLink"
	OperationSendBlast            OperationID = "SendBlast"
	OperationListPosters          OperationID = "ListPosters"
	OperationSearchPosters        OperationID = "SearchPosters"
	OperationGetCommandSchema     OperationID = "GetCommandSchema"
	OperationRunDoctor            OperationID = "RunDoctor"
	OperationGetVersion           OperationID = "GetVersion"
)

var operations = [...]OperationID{
	OperationAuthLoginInteractive,
	OperationGetAuthStatus,
	OperationLogout,
	OperationListEvents,
	OperationGetEvent,
	OperationCreateEvent,
	OperationUpdateEvent,
	OperationCancelEvent,
	OperationListGuests,
	OperationInviteGuest,
	OperationGetRSVP,
	OperationSetRSVP,
	OperationListContacts,
	OperationInviteCohost,
	OperationRevokeCohostInvite,
	OperationRemoveCohost,
	OperationCreateCohostLink,
	OperationRevokeCohostLink,
	OperationSendBlast,
	OperationListPosters,
	OperationSearchPosters,
	OperationGetCommandSchema,
	OperationRunDoctor,
	OperationGetVersion,
}

// Operations returns the closed operation inventory in command-model order.
func Operations() []OperationID {
	result := make([]OperationID, len(operations))
	copy(result, operations[:])
	return result
}
