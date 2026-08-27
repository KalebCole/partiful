package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

const ProtocolVersion = "2025-06-18"

const defaultDrainTimeout = 5 * time.Second

type Invoker interface {
	Invoke(context.Context, domain.OperationID, any) (any, error)
}

type Options struct {
	Diagnostics  io.Writer
	DrainTimeout time.Duration
}

type Server struct {
	catalog      command.Catalog
	invoker      Invoker
	diagnostics  io.Writer
	drainTimeout time.Duration

	writeMu sync.Mutex
	callsMu sync.Mutex
	calls   map[string]context.CancelFunc
	callsWG sync.WaitGroup
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *protocolError  `json:"error,omitempty"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type cancelParams struct {
	RequestID json.RawMessage `json:"requestId"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema any             `json:"inputSchema"`
	Annotations toolAnnotations `json:"annotations"`
}

type toolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []content `json:"content"`
	StructuredContent any       `json:"structuredContent"`
	IsError           bool      `json:"isError,omitempty"`
}

func NewServer(catalog command.Catalog, invoker Invoker, options Options) *Server {
	diagnostics := options.Diagnostics
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	drainTimeout := options.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = defaultDrainTimeout
	}
	return &Server{
		catalog: catalog, invoker: invoker, diagnostics: diagnostics,
		drainTimeout: drainTimeout, calls: make(map[string]context.CancelFunc),
	}
}

func (server *Server) ServeSignals(ctx context.Context, input io.Reader, output io.Writer, signals <-chan os.Signal) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if signals != nil {
		go func() {
			select {
			case <-ctx.Done():
			case <-signals:
				cancel()
			}
		}()
	}
	return server.Serve(ctx, input, output)
}

func (server *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if server == nil || server.invoker == nil || input == nil || output == nil {
		return fmt.Errorf("mcp server: missing dependency")
	}
	frames := make(chan []byte)
	readErrors := make(chan error, 1)
	go readFrames(input, frames, readErrors)

	for {
		select {
		case <-ctx.Done():
			server.cancelAll()
			server.drain()
			return nil
		case err := <-readErrors:
			if err != nil {
				server.cancelAll()
				server.drain()
				return fmt.Errorf("mcp stdio read: %w", err)
			}
		case frame, ok := <-frames:
			if !ok {
				server.drain()
				return nil
			}
			server.handleFrame(ctx, output, frame)
		}
	}
}

func readFrames(input io.Reader, frames chan<- []byte, readErrors chan<- error) {
	defer close(frames)
	scanner := bufio.NewScanner(input)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		frame := append([]byte(nil), scanner.Bytes()...)
		frames <- frame
	}
	if err := scanner.Err(); err != nil {
		readErrors <- err
	}
}

func (server *Server) handleFrame(ctx context.Context, output io.Writer, frame []byte) {
	var request request
	if err := json.Unmarshal(frame, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
		fmt.Fprintln(server.diagnostics, "invalid protocol frame")
		server.write(output, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &protocolError{Code: -32700, Message: "Parse error"}})
		return
	}

	switch request.Method {
	case "initialize":
		if len(request.ID) == 0 {
			return
		}
		server.write(output, response{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "partiful", "version": "devel"},
		}})
	case "notifications/initialized":
		return
	case "tools/list":
		if len(request.ID) == 0 {
			return
		}
		server.write(output, response{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{"tools": server.tools()}})
	case "tools/call":
		if len(request.ID) == 0 {
			return
		}
		server.startCall(ctx, output, request)
	case "notifications/cancelled":
		var params cancelParams
		if json.Unmarshal(request.Params, &params) == nil {
			server.cancelCall(string(params.RequestID))
		}
	default:
		if len(request.ID) != 0 {
			server.write(output, response{JSONRPC: "2.0", ID: request.ID, Error: &protocolError{Code: -32601, Message: "Method not found"}})
		}
	}
}

func (server *Server) tools() []tool {
	definitions := server.catalog.Definitions()
	tools := make([]tool, 0, 23)
	for _, definition := range definitions {
		if definition.MCP == nil {
			continue
		}
		tools = append(tools, tool{
			Name: definition.MCP.Name, Description: definition.Help,
			InputSchema: schemaJSON(definition.InputSchema),
			Annotations: toolAnnotations{
				ReadOnlyHint: definition.MCP.Hints.ReadOnly, DestructiveHint: definition.MCP.Hints.Destructive,
				IdempotentHint: definition.MCP.Hints.Idempotent, OpenWorldHint: definition.MCP.Hints.OpenWorld,
			},
		})
	}
	return tools
}

func (server *Server) startCall(parent context.Context, output io.Writer, request request) {
	key := string(request.ID)
	ctx, cancel := context.WithCancel(parent)
	server.callsMu.Lock()
	if previous := server.calls[key]; previous != nil {
		previous()
	}
	server.calls[key] = cancel
	server.callsMu.Unlock()
	server.callsWG.Add(1)
	go func() {
		defer server.callsWG.Done()
		defer cancel()
		defer server.removeCall(key, cancel)
		server.call(ctx, output, request)
	}()
}

func (server *Server) call(ctx context.Context, output io.Writer, request request) {
	var params callParams
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
		server.write(output, response{JSONRPC: "2.0", ID: request.ID, Error: &protocolError{Code: -32602, Message: "Invalid params"}})
		return
	}
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage("{}")
	}
	definition, found := server.catalog.LookupMCP(params.Name)
	if !found {
		server.write(output, response{JSONRPC: "2.0", ID: request.ID, Error: &protocolError{Code: -32602, Message: "Unknown tool"}})
		return
	}
	input, err := server.catalog.DecodeMCP(params.Name, params.Arguments)
	if err != nil {
		server.writeToolError(output, request.ID, err)
		return
	}
	result, err := server.invoker.Invoke(ctx, definition.Operation, input)
	if err != nil {
		server.writeToolError(output, request.ID, err)
		return
	}
	structured := logicalJSON(result)
	server.write(output, response{JSONRPC: "2.0", ID: request.ID, Result: newToolResult(structured, false)})
}

func (server *Server) writeToolError(output io.Writer, id json.RawMessage, err error) {
	publicError := sanitizeError(err)
	structured := map[string]any{"error": publicError}
	server.write(output, response{JSONRPC: "2.0", ID: id, Result: newToolResult(structured, true)})
}

func newToolResult(structured any, isError bool) toolResult {
	encoded, _ := json.Marshal(structured)
	return toolResult{Content: []content{{Type: "text", Text: string(encoded)}}, StructuredContent: structured, IsError: isError}
}

func sanitizeError(err error) map[string]any {
	var applicationError *domain.Error
	if errors.As(err, &applicationError) {
		details := map[string]any{}
		if applicationError.Details.Remediation != "" {
			details["remediation"] = applicationError.Details.Remediation
		}
		if applicationError.Details.State != "" {
			details["state"] = applicationError.Details.State
		}
		if applicationError.Details.BackendKind != "" {
			details["backend_kind"] = applicationError.Details.BackendKind
		}
		if len(applicationError.Details.Candidates) > 0 {
			details["candidates"] = logicalJSON(applicationError.Details.Candidates)
		}
		result := map[string]any{
			"type": string(applicationError.Type), "code": applicationError.Code,
			"message": applicationError.Message, "retryable": applicationError.Retryable,
			"details": details,
		}
		return result
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return map[string]any{"type": string(domain.ErrorInternalFailure), "code": "REQUEST_CANCELLED", "message": "request was cancelled", "retryable": false, "details": map[string]any{}}
	}
	return map[string]any{"type": string(domain.ErrorInternalFailure), "code": "INTERNAL_FAILURE", "message": "the operation failed", "retryable": false, "details": map[string]any{}}
}

func (server *Server) write(output io.Writer, value response) {
	server.writeMu.Lock()
	defer server.writeMu.Unlock()
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(server.diagnostics, "protocol write failed")
	}
}

func (server *Server) cancelCall(key string) {
	server.callsMu.Lock()
	cancel := server.calls[key]
	server.callsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (server *Server) cancelAll() {
	server.callsMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(server.calls))
	for _, cancel := range server.calls {
		cancels = append(cancels, cancel)
	}
	server.callsMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (server *Server) removeCall(key string, own context.CancelFunc) {
	server.callsMu.Lock()
	defer server.callsMu.Unlock()
	if _, exists := server.calls[key]; exists {
		delete(server.calls, key)
	}
}

func (server *Server) drain() {
	done := make(chan struct{})
	go func() {
		server.callsWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(server.drainTimeout):
		fmt.Fprintln(server.diagnostics, "bounded drain expired")
	}
}

func logicalJSON(value any) any {
	return logicalValue(reflect.ValueOf(value))
}

func logicalValue(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return logicalValue(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return value.Interface()
		}
		result := make(map[string]any)
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			result[snakeCase(field.Name)] = logicalValue(value.Field(index))
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for index := range result {
			result[index] = logicalValue(value.Index(index))
		}
		return result
	case reflect.Map:
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result[fmt.Sprint(iterator.Key().Interface())] = logicalValue(iterator.Value())
		}
		return result
	default:
		return value.Interface()
	}
}

func snakeCase(name string) string {
	var result strings.Builder
	for index, current := range name {
		if index > 0 && unicode.IsUpper(current) {
			previous := rune(name[index-1])
			nextLower := index+1 < len(name) && unicode.IsLower(rune(name[index+1]))
			if unicode.IsLower(previous) || nextLower {
				result.WriteByte('_')
			}
		}
		result.WriteRune(unicode.ToLower(current))
	}
	return result.String()
}
