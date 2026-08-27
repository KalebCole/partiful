package app

import (
	"context"
	"strings"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

type CollectionPage[T any] struct {
	Items      []T
	NextCursor *domain.Cursor
	HasMore    bool
}

type RemotePage[T any] struct {
	Items      []T
	NextCursor *transport.RemoteCursor
}

type GuestRecord struct {
	Guest         transport.Guest
	AnchorGuestID *string
}

func CanonicalQuery(query *string) string {
	if query == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*query))
}

func NormalizeCollectionInput(input domain.CollectionInput) (domain.CollectionInput, error) {
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 {
		return domain.CollectionInput{}, invalidCollectionInput("limit must be between 1 and 100")
	}
	if input.All {
		if input.MaxItems == nil || *input.MaxItems < 1 || *input.MaxItems > 1000 {
			return domain.CollectionInput{}, invalidCollectionInput("all requires max_items between 1 and 1000")
		}
	} else if input.MaxItems != nil {
		return domain.CollectionInput{}, invalidCollectionInput("max_items requires all")
	}
	return input, nil
}

func SliceCollection[T any](codec *CursorCodec, scope CursorScope, input domain.CollectionInput, items []T) (CollectionPage[T], error) {
	normalized, err := NormalizeCollectionInput(input)
	if err != nil {
		return CollectionPage[T]{}, err
	}
	offset := 0
	if normalized.Cursor != nil {
		offset, err = codec.Decode(*normalized.Cursor, scope, items)
		if err != nil {
			return CollectionPage[T]{}, err
		}
	}
	count := normalized.Limit
	if normalized.All {
		count = *normalized.MaxItems
	}
	end := offset + count
	if end > len(items) {
		end = len(items)
	}
	page := CollectionPage[T]{Items: append([]T(nil), items[offset:end]...), HasMore: end < len(items)}
	if page.HasMore {
		next, encodeErr := codec.Encode(scope, end, items)
		if encodeErr != nil {
			return CollectionPage[T]{}, encodeErr
		}
		page.NextCursor = &next
	}
	return page, nil
}

func MaterializeGuestPages[T any](ctx context.Context, fetch func(context.Context, *transport.RemoteCursor) (RemotePage[T], error)) ([]T, error) {
	var items []T
	var cursor *transport.RemoteCursor
	seen := make(map[transport.RemoteCursor]struct{})
	for page := 1; page <= 20; page++ {
		result, err := fetch(ctx, cursor)
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if result.NextCursor == nil {
			return items, nil
		}
		if _, exists := seen[*result.NextCursor]; exists {
			return nil, collectionProtocolChanged("COLLECTION_CURSOR_CYCLE")
		}
		seen[*result.NextCursor] = struct{}{}
		if page == 20 {
			return nil, collectionProtocolChanged("COLLECTION_GUEST_PAGE_21")
		}
		value := *result.NextCursor
		cursor = &value
	}
	return nil, collectionProtocolChanged("COLLECTION_GUEST_PAGE_21")
}

func MaterializeContactPages[T any](ctx context.Context, fetch func(context.Context, *transport.RemoteCursor) (RemotePage[T], error)) ([]T, error) {
	var items []T
	var cursor *transport.RemoteCursor
	seen := make(map[transport.RemoteCursor]struct{})
	for {
		result, err := fetch(ctx, cursor)
		if err != nil {
			return nil, err
		}
		if result.NextCursor == nil {
			return items, nil
		}
		if _, exists := seen[*result.NextCursor]; exists {
			return nil, collectionProtocolChanged("COLLECTION_CURSOR_CYCLE")
		}
		seen[*result.NextCursor] = struct{}{}
		items = append(items, result.Items...)
		value := *result.NextCursor
		cursor = &value
	}
}

func DedupeContacts(contacts []transport.Contact) []transport.Contact {
	seen := make(map[transport.ContactID]struct{}, len(contacts))
	result := make([]transport.Contact, 0, len(contacts))
	for _, contact := range contacts {
		if _, exists := seen[contact.ContactID]; exists {
			continue
		}
		seen[contact.ContactID] = struct{}{}
		result = append(result, contact)
	}
	return result
}

func FilterContactsByName(contacts []transport.Contact, canonicalQuery string) []transport.Contact {
	query := strings.ToLower(strings.TrimSpace(canonicalQuery))
	result := make([]transport.Contact, 0, len(contacts))
	for _, contact := range contacts {
		if strings.Contains(strings.ToLower(contact.DisplayName), query) {
			result = append(result, contact)
		}
	}
	return result
}

func TopLevelGuests(records []GuestRecord) []transport.Guest {
	result := make([]transport.Guest, 0, len(records))
	for _, record := range records {
		if record.AnchorGuestID == nil {
			result = append(result, record.Guest)
		}
	}
	return result
}

func FilterPostersByQuery(posters []transport.Poster, canonicalQuery string) []transport.Poster {
	query := strings.ToLower(strings.TrimSpace(canonicalQuery))
	result := make([]transport.Poster, 0, len(posters))
	for _, poster := range posters {
		if strings.Contains(strings.ToLower(poster.Name), query) {
			result = append(result, poster)
		}
	}
	return result
}

func invalidCollectionInput(message string) error {
	return &domain.Error{Type: domain.ErrorUsageInvalid, Code: "INVALID_COLLECTION_INPUT", Message: message}
}

func collectionProtocolChanged(code string) error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: code, Message: "collection traversal exceeded the accepted protocol"}
}
