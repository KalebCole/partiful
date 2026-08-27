package app

import (
	"fmt"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

// ProjectSequence applies a privacy boundary to a complete sequence. It returns
// no partial result when any item cannot be projected safely.
func ProjectSequence[Private, Public any](private []Private, projector func(Private) (Public, error)) ([]Public, error) {
	if projector == nil {
		return nil, fmt.Errorf("project sequence: missing projector")
	}
	projected := make([]Public, 0, len(private))
	for _, item := range private {
		value, err := projector(item)
		if err != nil {
			return nil, err
		}
		projected = append(projected, value)
	}
	return projected, nil
}

func ProjectContacts(contacts []transport.Contact, reference func(transport.ContactID) (domain.ContactRef, error)) ([]domain.Contact, error) {
	if reference == nil {
		return nil, fmt.Errorf("project contacts: missing reference derivation")
	}
	return ProjectSequence(contacts, func(contact transport.Contact) (domain.Contact, error) {
		contactRef, err := reference(contact.ContactID)
		if err != nil {
			return domain.Contact{}, err
		}
		return domain.Contact{ContactRef: contactRef, DisplayName: contact.DisplayName, SharedEventCount: contact.SharedEventCount}, nil
	})
}

func ProjectPosters(posters []transport.Poster) []domain.Poster {
	result, _ := ProjectSequence(posters, func(poster transport.Poster) (domain.Poster, error) {
		return domain.Poster{
			PosterID: domain.PosterID(poster.PosterID), Name: poster.Name, URL: poster.URL,
			ContentType: poster.ContentType, Width: poster.Width, Height: poster.Height,
			Tags: append([]string(nil), poster.Tags...), Categories: append([]string(nil), poster.Categories...),
		}, nil
	})
	return result
}
