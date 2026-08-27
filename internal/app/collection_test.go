package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

func TestSliceCollectionHonorsRetrievalControlsAndCursorPosition(t *testing.T) {
	t.Parallel()

	codec, _ := NewCursorCodec([]byte("installation-secret"))
	scope := CursorScope{Operation: domain.OperationListPosters}
	items := []string{"one", "two", "three", "four"}
	first, err := SliceCollection(codec, scope, domain.CollectionInput{Limit: 2}, items)
	if err != nil || !first.HasMore || first.NextCursor == nil || !reflect.DeepEqual(first.Items, []string{"one", "two"}) {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	one := 1
	resumed, err := SliceCollection(codec, scope, domain.CollectionInput{Limit: 1, Cursor: first.NextCursor}, items)
	if err != nil || !resumed.HasMore || !reflect.DeepEqual(resumed.Items, []string{"three"}) {
		t.Fatalf("resumed page = %#v, %v", resumed, err)
	}
	all, err := SliceCollection(codec, scope, domain.CollectionInput{Limit: 1, Cursor: first.NextCursor, All: true, MaxItems: &one}, items)
	if err != nil || !all.HasMore || !reflect.DeepEqual(all.Items, []string{"three"}) {
		t.Fatalf("all page = %#v, %v", all, err)
	}
	terminal, err := SliceCollection(codec, scope, domain.CollectionInput{Limit: 100, Cursor: all.NextCursor}, items)
	if err != nil || terminal.HasMore || terminal.NextCursor != nil || !reflect.DeepEqual(terminal.Items, []string{"four"}) {
		t.Fatalf("terminal page = %#v, %v", terminal, err)
	}
}

func TestSliceCollectionEmptyAndExactLimitAreTerminal(t *testing.T) {
	t.Parallel()
	codec, _ := NewCursorCodec([]byte("installation-secret"))
	scope := CursorScope{Operation: domain.OperationListPosters}
	for _, items := range [][]string{nil, {"one", "two"}} {
		page, err := SliceCollection(codec, scope, domain.CollectionInput{Limit: 2}, items)
		if err != nil || page.HasMore || page.NextCursor != nil || !reflect.DeepEqual(page.Items, items) {
			t.Fatalf("SliceCollection(%v) = %#v, %v", items, page, err)
		}
	}
}

func TestValidateCollectionInputRejectsInvalidControlCombinations(t *testing.T) {
	t.Parallel()

	cases := []domain.CollectionInput{
		{Limit: -1}, {Limit: 101}, {Limit: 25, All: true}, {Limit: 25, MaxItems: intPointer(1)},
		{Limit: 25, All: true, MaxItems: intPointer(0)}, {Limit: 25, All: true, MaxItems: intPointer(1001)},
	}
	for _, input := range cases {
		if _, err := NormalizeCollectionInput(input); err == nil {
			t.Fatalf("NormalizeCollectionInput(%#v) succeeded", input)
		}
	}
	normalized, err := NormalizeCollectionInput(domain.CollectionInput{})
	if err != nil || normalized.Limit != 25 {
		t.Fatalf("NormalizeCollectionInput(default) = %#v, %v", normalized, err)
	}
}

func TestCanonicalQueryTreatsOmissionAndNormalizedEmptyAsEqual(t *testing.T) {
	t.Parallel()
	empty := "  "
	query := "  ALpHa  "
	if CanonicalQuery(nil) != CanonicalQuery(&empty) || CanonicalQuery(&query) != "alpha" {
		t.Fatalf("canonical queries = nil:%q empty:%q query:%q", CanonicalQuery(nil), CanonicalQuery(&empty), CanonicalQuery(&query))
	}
}

func TestMaterializeGuestPagesConsumesFinalDataAndFailsClosedAtPageTwenty(t *testing.T) {
	t.Parallel()

	calls := 0
	fetch := func(_ context.Context, cursor *transport.RemoteCursor) (RemotePage[string], error) {
		calls++
		if calls == 1 && cursor != nil {
			t.Fatalf("first cursor = %v, want nil", cursor)
		}
		if calls == 1 {
			next := transport.RemoteCursor("next")
			return RemotePage[string]{Items: []string{"one"}, NextCursor: &next}, nil
		}
		return RemotePage[string]{Items: []string{"two"}}, nil
	}
	items, err := MaterializeGuestPages(context.Background(), fetch)
	if err != nil || calls != 2 || !reflect.DeepEqual(items, []string{"one", "two"}) {
		t.Fatalf("MaterializeGuestPages() = %v, %v; calls %d", items, err, calls)
	}

	page := 0
	items, err = MaterializeGuestPages(context.Background(), func(context.Context, *transport.RemoteCursor) (RemotePage[string], error) {
		page++
		next := transport.RemoteCursor(pageString(page))
		return RemotePage[string]{Items: []string{pageString(page)}, NextCursor: &next}, nil
	})
	var applicationError *domain.Error
	if items != nil || !errors.As(err, &applicationError) || applicationError.Type != domain.ErrorContractProtocolChanged || applicationError.Code != "COLLECTION_GUEST_PAGE_21" || page != 20 {
		t.Fatalf("page-20 result = %v, %#v; calls %d", items, err, page)
	}
}

func TestMaterializeContactPagesStopsBeforeSentinelDataAndRejectsCycles(t *testing.T) {
	t.Parallel()

	calls := 0
	items, err := MaterializeContactPages(context.Background(), func(context.Context, *transport.RemoteCursor) (RemotePage[string], error) {
		calls++
		if calls == 1 {
			next := transport.RemoteCursor("next")
			return RemotePage[string]{Items: []string{"data"}, NextCursor: &next}, nil
		}
		return RemotePage[string]{Items: []string{"sentinel-must-not-appear"}}, nil
	})
	if err != nil || calls != 2 || !reflect.DeepEqual(items, []string{"data"}) {
		t.Fatalf("MaterializeContactPages() = %v, %v; calls %d", items, err, calls)
	}

	cycle := transport.RemoteCursor("same")
	items, err = MaterializeContactPages(context.Background(), func(context.Context, *transport.RemoteCursor) (RemotePage[string], error) {
		return RemotePage[string]{Items: []string{"private"}, NextCursor: &cycle}, nil
	})
	if items != nil || err == nil {
		t.Fatalf("cycle result = %v, %v; want no partial result", items, err)
	}
}

func TestSettledLocalCollectionRulesPreserveOrder(t *testing.T) {
	t.Parallel()

	contacts := []transport.Contact{
		{ContactID: "first", DisplayName: "Alpha"},
		{ContactID: "second", DisplayName: "Beta"},
		{ContactID: "first", DisplayName: "Changed duplicate"},
		{ContactID: "third", DisplayName: "alphabet"},
	}
	got := FilterContactsByName(DedupeContacts(contacts), "ALP")
	if !reflect.DeepEqual(got, []transport.Contact{contacts[0], contacts[3]}) {
		t.Fatalf("contact rules = %#v", got)
	}

	anchor := "parent"
	guests := []GuestRecord{
		{Guest: transport.Guest{GuestID: "top-one"}},
		{Guest: transport.Guest{GuestID: "plus-one"}, AnchorGuestID: &anchor},
		{Guest: transport.Guest{GuestID: "top-two"}},
	}
	if gotGuests := TopLevelGuests(guests); !reflect.DeepEqual(gotGuests, []transport.Guest{guests[0].Guest, guests[2].Guest}) {
		t.Fatalf("top-level guests = %#v", gotGuests)
	}

	posters := []transport.Poster{{PosterID: "one", Name: "Summer Night"}, {PosterID: "two", Name: "Winter"}, {PosterID: "one", Name: "summer duplicate"}}
	if gotPosters := FilterPostersByQuery(posters, "SUMMER"); !reflect.DeepEqual(gotPosters, []transport.Poster{posters[0], posters[2]}) {
		t.Fatalf("poster rules = %#v", gotPosters)
	}
}

func TestPrivacyProjectorsRemovePrivateIdentifiersAndReturnNoPartialResult(t *testing.T) {
	t.Parallel()

	contacts := []transport.Contact{{ContactID: "private-one", DisplayName: "Alpha", SharedEventCount: 2}, {ContactID: "private-two", DisplayName: "Beta", SharedEventCount: 1}}
	projected, err := ProjectContacts(contacts, func(id transport.ContactID) (domain.ContactRef, error) {
		if id == "private-two" {
			return "", errors.New("cannot derive")
		}
		return domain.ContactRef("public-ref"), nil
	})
	if projected != nil || err == nil {
		t.Fatalf("ProjectContacts(failure) = %#v, %v; want no partial result", projected, err)
	}
	projected, err = ProjectContacts(contacts[:1], func(transport.ContactID) (domain.ContactRef, error) {
		return "public-ref", nil
	})
	if err != nil || len(projected) != 1 || projected[0].ContactRef != "public-ref" || projected[0].DisplayName != "Alpha" {
		t.Fatalf("ProjectContacts(success) = %#v, %v", projected, err)
	}

	privatePosters := []transport.Poster{{PosterID: "poster", Name: "Name", Tags: []string{"tag"}, Categories: []string{"category"}}}
	publicPosters := ProjectPosters(privatePosters)
	privatePosters[0].Tags[0] = "changed"
	privatePosters[0].Categories[0] = "changed"
	if len(publicPosters) != 1 || publicPosters[0].PosterID != "poster" || !reflect.DeepEqual(publicPosters[0].Tags, []string{"tag"}) || !reflect.DeepEqual(publicPosters[0].Categories, []string{"category"}) {
		t.Fatalf("ProjectPosters() = %#v", publicPosters)
	}
}

func intPointer(value int) *int { return &value }

func pageString(page int) string {
	const digits = "0123456789"
	if page < 10 {
		return "page-0" + string(digits[page])
	}
	return "page-" + string(digits[page/10]) + string(digits[page%10])
}
