package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/mcp"
)

type invokeFunc func(context.Context, domain.OperationID, any) (any, error)

func (fn invokeFunc) Invoke(ctx context.Context, operation domain.OperationID, input any) (any, error) {
	return fn(ctx, operation, input)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func TestServerInitializesAndDerivesExactToolInventory(t *testing.T) {
	catalog := testCatalog(t)
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
	)
	var stdout, stderr bytes.Buffer
	server := mcp.NewServer(catalog, invokeFunc(unexpectedInvoke(t)), mcp.Options{
		Diagnostics:   &stderr,
		ServerVersion: "1.2.3-test",
	})

	if err := server.Serve(context.Background(), input, &stdout); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, stdout.Bytes())
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2", len(responses))
	}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities struct {
			Tools map[string]any `json:"tools"`
		} `json:"capabilities"`
	}
	mustUnmarshal(t, responses[0].Result, &initialized)
	if initialized.ProtocolVersion != mcp.ProtocolVersion || initialized.ServerInfo.Name != "partiful" || initialized.ServerInfo.Version != "1.2.3-test" || initialized.Capabilities.Tools == nil {
		t.Fatalf("initialize result = %#v", initialized)
	}
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint    bool `json:"readOnlyHint"`
				DestructiveHint bool `json:"destructiveHint"`
				IdempotentHint  bool `json:"idempotentHint"`
				OpenWorldHint   bool `json:"openWorldHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	mustUnmarshal(t, responses[1].Result, &listed)
	if len(listed.Tools) != 23 {
		t.Fatalf("tool count = %d, want 23", len(listed.Tools))
	}
	definitions := catalog.Definitions()
	var want []string
	for _, definition := range definitions {
		if definition.MCP != nil {
			want = append(want, definition.MCP.Name)
		}
	}
	got := make([]string, len(listed.Tools))
	for index, tool := range listed.Tools {
		got[index] = tool.Name
		if tool.Description == "" {
			t.Errorf("tool %q lacks its derived description", tool.Name)
		}
		if branches, hasBranches := tool.InputSchema["oneOf"]; hasBranches {
			if _, hasRootClosure := tool.InputSchema["additionalProperties"]; hasRootClosure {
				t.Errorf("tool %q closes the oneOf root and rejects every branch field", tool.Name)
			}
			if tool.Name == "rsvp_set" && len(branches.([]any)) != 4 {
				t.Errorf("rsvp_set branch count = %d, want 4", len(branches.([]any)))
			}
		} else if tool.InputSchema["additionalProperties"] != false {
			t.Errorf("tool %q input schema is not closed", tool.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestServerReturnsStructuredPublicResultAndProtectedAuthError(t *testing.T) {
	catalog := testCatalog(t)
	invoker := invokeFunc(func(_ context.Context, operation domain.OperationID, _ any) (any, error) {
		switch operation {
		case domain.OperationGetVersion:
			return domain.VersionResult{CLIVersion: "devel", CommandContractRevision: "1", TransportContractRevision: "fixture"}, nil
		case domain.OperationGetEvent:
			return nil, &domain.Error{Type: domain.ErrorAuthRequired, Code: "AUTH_REQUIRED", Message: "authentication is required", Details: domain.ErrorDetails{Remediation: "Run partiful auth login."}}
		default:
			return nil, errors.New("unexpected operation")
		}
	})
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":"public","method":"tools/call","params":{"name":"version","arguments":{}}}` + "\n" +
			`{"jsonrpc":"2.0","id":"protected","method":"tools/call","params":{"name":"events_get","arguments":{"event_id":"event_redacted"}}}` + "\n",
	)
	var stdout bytes.Buffer
	if err := mcp.NewServer(catalog, invoker, mcp.Options{}).Serve(context.Background(), input, &stdout); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, stdout.Bytes())
	byID := responseMap(responses)
	var public struct {
		StructuredContent struct {
			CLIVersion string `json:"cli_version"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	mustUnmarshal(t, byID[`"public"`].Result, &public)
	if public.IsError || public.StructuredContent.CLIVersion != "devel" {
		t.Fatalf("public result = %#v", public)
	}
	var protected struct {
		StructuredContent struct {
			Error struct {
				Type    domain.ErrorType `json:"type"`
				Code    string           `json:"code"`
				Details struct {
					Remediation string `json:"remediation"`
				} `json:"details"`
			} `json:"error"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	mustUnmarshal(t, byID[`"protected"`].Result, &protected)
	if !protected.IsError || protected.StructuredContent.Error.Type != domain.ErrorAuthRequired || protected.StructuredContent.Error.Code != "AUTH_REQUIRED" || protected.StructuredContent.Error.Details.Remediation != "Run partiful auth login." {
		t.Fatalf("protected result = %#v", protected)
	}
}

func TestServerPreservesSafeClassifiedErrorDetails(t *testing.T) {
	catalog := testCatalog(t)
	invoker := invokeFunc(func(context.Context, domain.OperationID, any) (any, error) {
		return nil, &domain.Error{
			Type: domain.ErrorMatchAmbiguous, Code: "MATCH_AMBIGUOUS", Message: "the contact selector is ambiguous",
			Details: domain.ErrorDetails{Candidates: []domain.Contact{{ContactRef: "contact_redacted", DisplayName: "name_redacted", SharedEventCount: 2}}},
		}
	})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"guests_invite","arguments":{"event_id":"event_redacted","contact":"name_redacted"}}}` + "\n")
	var stdout bytes.Buffer
	if err := mcp.NewServer(catalog, invoker, mcp.Options{}).Serve(context.Background(), input, &stdout); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, stdout.Bytes())
	var result struct {
		StructuredContent struct {
			Error struct {
				Details struct {
					Candidates []struct {
						ContactRef       string `json:"contact_ref"`
						DisplayName      string `json:"display_name"`
						SharedEventCount int    `json:"shared_event_count"`
					} `json:"candidates"`
				} `json:"details"`
			} `json:"error"`
		} `json:"structuredContent"`
	}
	mustUnmarshal(t, responses[0].Result, &result)
	candidates := result.StructuredContent.Error.Details.Candidates
	if len(candidates) != 1 || candidates[0].ContactRef != "contact_redacted" || candidates[0].DisplayName != "name_redacted" || candidates[0].SharedEventCount != 2 {
		t.Fatalf("safe error candidates = %#v", candidates)
	}
}

func TestCancellationByRequestIDLeavesServerHealthy(t *testing.T) {
	catalog := testCatalog(t)
	started := make(chan struct{})
	var once sync.Once
	invoker := invokeFunc(func(ctx context.Context, operation domain.OperationID, _ any) (any, error) {
		if operation == domain.OperationListPosters {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if operation == domain.OperationGetVersion {
			return domain.VersionResult{CLIVersion: "healthy"}, nil
		}
		return nil, errors.New("unexpected operation")
	})
	reader, writer := io.Pipe()
	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- mcp.NewServer(catalog, invoker, mcp.Options{DrainTimeout: 100 * time.Millisecond}).Serve(context.Background(), reader, &stdout)
	}()
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"posters_list","arguments":{}}}`)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow call did not start")
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"fixture"}}`)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"version","arguments":{}}}`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	byID := responseMap(decodeResponses(t, stdout.Bytes()))
	var cancelled struct {
		IsError bool `json:"isError"`
	}
	mustUnmarshal(t, byID["7"].Result, &cancelled)
	if !cancelled.IsError {
		t.Fatal("cancelled call was not a tool error")
	}
	var healthy struct {
		StructuredContent struct {
			CLIVersion string `json:"cli_version"`
		} `json:"structuredContent"`
	}
	mustUnmarshal(t, byID["8"].Result, &healthy)
	if healthy.StructuredContent.CLIVersion != "healthy" {
		t.Fatalf("healthy result = %#v", healthy)
	}
}

func TestStdoutContainsOnlyProtocolFramesAndDiagnosticsUseStderr(t *testing.T) {
	catalog := testCatalog(t)
	var stdout, stderr bytes.Buffer
	input := strings.NewReader("not-json\n" + `{"jsonrpc":"2.0","id":1,"method":"unknown"}` + "\n")
	server := mcp.NewServer(catalog, invokeFunc(unexpectedInvoke(t)), mcp.Options{Diagnostics: &stderr})
	if err := server.Serve(context.Background(), input, &stdout); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, stdout.Bytes())
	if len(responses) != 2 || len(responses[0].Error) == 0 || len(responses[1].Error) == 0 {
		t.Fatalf("protocol errors = %#v", responses)
	}
	if !strings.Contains(stderr.String(), "invalid protocol frame") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestEOFStopsCleanly(t *testing.T) {
	server := mcp.NewServer(testCatalog(t), invokeFunc(unexpectedInvoke(t)), mcp.Options{})
	if err := server.Serve(context.Background(), strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("Serve(EOF) error = %v", err)
	}
}

func TestSignalCancellationUsesBoundedDrain(t *testing.T) {
	catalog := testCatalog(t)
	started := make(chan struct{})
	release := make(chan struct{})
	invoker := invokeFunc(func(context.Context, domain.OperationID, any) (any, error) {
		close(started)
		<-release
		return domain.VersionResult{}, nil
	})
	reader, writer := io.Pipe()
	signals := make(chan os.Signal, 1)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- mcp.NewServer(catalog, invoker, mcp.Options{DrainTimeout: 30 * time.Millisecond}).ServeSignals(context.Background(), reader, io.Discard, signals)
	}()
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"version","arguments":{}}}`)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("call did not start")
	}
	signals <- os.Interrupt
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeSignals() error = %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("signal shutdown exceeded bounded drain")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("signal shutdown took %v", elapsed)
	}
	close(release)
	_ = writer.Close()
}

func testCatalog(t *testing.T) command.Catalog {
	t.Helper()
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func unexpectedInvoke(t *testing.T) func(context.Context, domain.OperationID, any) (any, error) {
	t.Helper()
	return func(context.Context, domain.OperationID, any) (any, error) {
		t.Error("unexpected application invocation")
		return nil, errors.New("unexpected invocation")
	}
}

func writeFrame(t *testing.T, writer io.Writer, frame string) {
	t.Helper()
	if _, err := io.WriteString(writer, frame+"\n"); err != nil {
		t.Fatal(err)
	}
}

func decodeResponses(t *testing.T, data []byte) []rpcResponse {
	t.Helper()
	var responses []rpcResponse
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("stdout contains non-protocol data %q: %v", scanner.Text(), err)
		}
		if response.JSONRPC != "2.0" {
			t.Fatalf("stdout frame has jsonrpc = %q", response.JSONRPC)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return responses
}

func responseMap(responses []rpcResponse) map[string]rpcResponse {
	result := make(map[string]rpcResponse, len(responses))
	for _, response := range responses {
		result[string(response.ID)] = response
	}
	return result
}

func mustUnmarshal(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", data, err)
	}
}
