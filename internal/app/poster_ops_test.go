package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

type fakePosterTransport struct {
	posters []transport.Poster
	err     error
	calls   int
}

func (remote *fakePosterTransport) GetCatalog(context.Context, transport.GetPosterCatalogRequest) (transport.GetPosterCatalogResult, error) {
	remote.calls++
	return transport.GetPosterCatalogResult{Posters: append([]transport.Poster(nil), remote.posters...)}, remote.err
}

func posterRecord(id, name string) transport.Poster {
	return transport.Poster{PosterID: transport.PosterID(id), Name: name, URL: "https://example.invalid/poster.png", ContentType: "image/png", Width: 10, Height: 20, Tags: []string{}, Categories: []string{}}
}

func TestPosterListPreservesObservedOrderAndDuplicatesThroughSharedCollection(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	remote := &fakePosterTransport{posters: []transport.Poster{posterRecord("same", "First"), posterRecord("same", "Second"), posterRecord("third", "Third")}}
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(service, remote, codec); err != nil {
		t.Fatal(err)
	}

	firstValue, err := service.Invoke(context.Background(), domain.OperationListPosters, domain.ListPostersInput{CollectionInput: domain.CollectionInput{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	first := firstValue.(domain.PostersResult)
	if len(first.Posters) != 2 || first.Posters[0].Name != "First" || first.Posters[1].Name != "Second" || first.Posters[0].PosterID != first.Posters[1].PosterID || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first page = %#v", first)
	}
	secondValue, err := service.Invoke(context.Background(), domain.OperationListPosters, domain.ListPostersInput{CollectionInput: domain.CollectionInput{Limit: 2, Cursor: first.NextCursor}})
	if err != nil {
		t.Fatal(err)
	}
	second := secondValue.(domain.PostersResult)
	if !reflect.DeepEqual(second.Posters, []domain.Poster{{PosterID: "third", Name: "Third", URL: "https://example.invalid/poster.png", ContentType: "image/png", Width: 10, Height: 20, Tags: []string{}, Categories: []string{}}}) || second.HasMore || second.NextCursor != nil {
		t.Fatalf("second page = %#v", second)
	}
	if remote.calls != 2 {
		t.Fatalf("catalog calls = %d, want one per invocation", remote.calls)
	}
}

func TestPosterSearchFiltersCaseInsensitivelyBeforePagingAndBindsCursorToQuery(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	remote := &fakePosterTransport{posters: []transport.Poster{posterRecord("1", "Blue Sky"), posterRecord("2", "Red"), posterRecord("3", "sky line")}}
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(service, remote, codec); err != nil {
		t.Fatal(err)
	}

	value, err := service.Invoke(context.Background(), domain.OperationSearchPosters, domain.SearchPostersInput{Query: " SKY ", CollectionInput: domain.CollectionInput{Limit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	first := value.(domain.PostersResult)
	if len(first.Posters) != 1 || first.Posters[0].Name != "Blue Sky" || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("search page = %#v", first)
	}
	_, err = service.Invoke(context.Background(), domain.OperationSearchPosters, domain.SearchPostersInput{Query: "red", CollectionInput: domain.CollectionInput{Limit: 1, Cursor: first.NextCursor}})
	var public *domain.Error
	if !errors.As(err, &public) || public.Code != "INVALID_CURSOR" {
		t.Fatalf("wrong-query cursor error = %#v", err)
	}
}

func TestPosterSearchRejectsBlankQueryBeforeTransport(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	remote := &fakePosterTransport{}
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(service, remote, codec); err != nil {
		t.Fatal(err)
	}
	_, err := service.Invoke(context.Background(), domain.OperationSearchPosters, domain.SearchPostersInput{Query: " \t "})
	var public *domain.Error
	if !errors.As(err, &public) || public.Type != domain.ErrorInputInvalid || public.Code != "INVALID_POSTER_QUERY" {
		t.Fatalf("blank query error = %#v", err)
	}
	if remote.calls != 0 {
		t.Fatalf("catalog calls = %d, want 0", remote.calls)
	}
}

func TestPosterUnknownFailureFailsClosedWithoutLeakingTransportDetails(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	remote := &fakePosterTransport{err: &transport.ProtocolFailure{Operation: "getPosterCatalog", Class: "future.unknown", CorrelationID: "private-correlation"}}
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(service, remote, codec); err != nil {
		t.Fatal(err)
	}
	_, err := service.Invoke(context.Background(), domain.OperationListPosters, domain.ListPostersInput{})
	var public *domain.Error
	if !errors.As(err, &public) || public.Code != "EVIDENCE_CLAIM_OPEN" || public.Message == "" {
		t.Fatalf("unknown failure = %#v", err)
	}
	if public.Message == "private-correlation" {
		t.Fatal("transport detail leaked")
	}
}

func TestPosterKnownProtocolFailureIsSanitized(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	remote := &fakePosterTransport{err: &transport.ProtocolFailure{Operation: "getPosterCatalog", Class: string(domain.ErrorRemoteUnavailable), Retryable: true, CorrelationID: "private-correlation"}}
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(service, remote, codec); err != nil {
		t.Fatal(err)
	}
	_, err := service.Invoke(context.Background(), domain.OperationListPosters, domain.ListPostersInput{})
	var public *domain.Error
	if !errors.As(err, &public) || public.Type != domain.ErrorRemoteUnavailable || public.Code != "REMOTE_UNAVAILABLE" || !public.Retryable {
		t.Fatalf("known failure = %#v", err)
	}
}

func TestBindPosterOperationsRejectsMissingDependencies(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	codec, _ := NewCursorCodec([]byte("poster-test-installation"))
	if err := BindPosterOperations(testService(t, manifest), nil, codec); err == nil {
		t.Fatal("BindPosterOperations accepted nil transport")
	}
	if err := BindPosterOperations(testService(t, manifest), &fakePosterTransport{}, nil); err == nil {
		t.Fatal("BindPosterOperations accepted nil cursor codec")
	}
}
