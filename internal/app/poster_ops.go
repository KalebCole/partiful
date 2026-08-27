package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

const posterErrorGate = "OP11-ENDPOINT-ERRORS:getPosterCatalog"

// BindPosterOperations registers public poster collection operations over the
// strict unauthenticated poster transport.
func BindPosterOperations(service *Service, remote transport.PosterTransport, cursors *CursorCodec) error {
	if service == nil || remote == nil || cursors == nil {
		return fmt.Errorf("bind poster operations: missing dependency")
	}
	if err := BindOperation(service, OperationSpec[domain.ListPostersInput, domain.PostersResult]{
		Operation: domain.OperationListPosters,
		ErrorGate: posterErrorGate,
		Execute: func(ctx context.Context, _ *Invocation, input domain.ListPostersInput) (domain.PostersResult, error) {
			return collectPosters(ctx, remote, cursors, domain.OperationListPosters, "", input.CollectionInput)
		},
	}); err != nil {
		return err
	}
	return BindOperation(service, OperationSpec[domain.SearchPostersInput, domain.PostersResult]{
		Operation: domain.OperationSearchPosters,
		ErrorGate: posterErrorGate,
		Execute: func(ctx context.Context, _ *Invocation, input domain.SearchPostersInput) (domain.PostersResult, error) {
			query := CanonicalQuery(&input.Query)
			if query == "" {
				return domain.PostersResult{}, &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_POSTER_QUERY", Message: "poster query must not be blank"}
			}
			return collectPosters(ctx, remote, cursors, domain.OperationSearchPosters, query, input.CollectionInput)
		},
	})
}

func collectPosters(ctx context.Context, remote transport.PosterTransport, cursors *CursorCodec, operation domain.OperationID, query string, input domain.CollectionInput) (domain.PostersResult, error) {
	normalized, err := NormalizeCollectionInput(input)
	if err != nil {
		return domain.PostersResult{}, err
	}
	catalog, err := remote.GetCatalog(ctx, transport.GetPosterCatalogRequest{})
	if err != nil {
		return domain.PostersResult{}, err
	}
	posters := catalog.Posters
	if operation == domain.OperationSearchPosters {
		posters = FilterPostersByQuery(posters, query)
	}
	projected := ProjectPosters(posters)
	for index := range projected {
		if projected[index].Tags == nil {
			projected[index].Tags = []string{}
		}
		if projected[index].Categories == nil {
			projected[index].Categories = []string{}
		}
	}
	page, err := SliceCollection(cursors, CursorScope{Operation: operation, CanonicalFilter: strings.TrimSpace(strings.ToLower(query))}, normalized, projected)
	if err != nil {
		return domain.PostersResult{}, err
	}
	return domain.PostersResult{Posters: page.Items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}
