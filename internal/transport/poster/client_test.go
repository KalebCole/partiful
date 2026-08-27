package poster

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KalebCole/partiful/internal/transport"
)

func TestClientImplementsPosterTransport(t *testing.T) {
	var _ transport.PosterTransport = (*Client)(nil)
}

func TestGetCatalogPreservesOrderDuplicatesAndTypedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/posters.json" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"same","name":"One","url":"https://example.invalid/1.png","contentType":"image/png","width":10,"height":20,"tags":["a"],"categories":[]},{"id":"same","name":"Two","url":"https://example.invalid/2.gif","contentType":"image/gif","width":30,"height":40,"tags":[],"categories":["b"]}]`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	result, err := client.GetCatalog(context.Background(), transport.GetPosterCatalogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Posters) != 2 || result.Posters[0].Name != "One" || result.Posters[1].Name != "Two" || result.Posters[0].PosterID != result.Posters[1].PosterID {
		t.Fatalf("result = %#v", result)
	}
}

func TestMalformedOrUnknownCatalogFailsClosed(t *testing.T) {
	for _, body := range []string{
		`[{"id":"p","name":"Poster","url":"http://not-https.invalid/x","contentType":"image/png","width":1,"height":1,"tags":[],"categories":[]}]`,
		`[{"id":"p","name":"Poster","url":"https://example.invalid/x","contentType":"image/future","width":1,"height":1,"tags":[],"categories":[]}]`,
		`[{"id":"p","name":"Poster","url":"https://example.invalid/x","contentType":"image/png","width":null,"height":null,"tags":[],"categories":[]}]`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		client := New(Config{BaseURL: server.URL})
		_, err := client.GetCatalog(context.Background(), transport.GetPosterCatalogRequest{})
		server.Close()
		var failure *transport.ProtocolFailure
		if !errors.As(err, &failure) || failure.Operation != "getPosterCatalog" || failure.Class != "contract.protocol_changed" || failure.DispatchState != transport.DispatchStarted {
			t.Fatalf("body %s: error = %#v", body, err)
		}
	}
}
