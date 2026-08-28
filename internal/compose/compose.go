package compose

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/app"
	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/cli"
	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/credentialstore"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/mcp"
	"github.com/KalebCole/partiful/internal/transport"
	"github.com/KalebCole/partiful/internal/transport/callable"
	"github.com/KalebCole/partiful/internal/transport/firebaseauth"
	"github.com/KalebCole/partiful/internal/transport/firestore"
	"github.com/KalebCole/partiful/internal/transport/poster"
	"github.com/KalebCole/partiful/internal/version"
)

const (
	lockTimeout            = 10 * time.Second
	credentialProbeTimeout = 2 * time.Second
)

type cliConfig struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	isTerminal bool
	store      auth.CredentialStore
	root       string
}

type graph struct {
	catalog command.Catalog
	service *app.Service
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type diagnosticWriter struct{ output io.Writer }

func (writer diagnosticWriter) Warn(_ context.Context, diagnostic auth.Diagnostic) {
	if writer.output != nil {
		fmt.Fprintf(writer.output, "warning: %s (%s)\n", diagnostic.Kind, diagnostic.State)
	}
}

type terminalPrompter struct {
	input  *bufio.Reader
	output io.Writer
}

func (prompter *terminalPrompter) read(ctx context.Context, label string) (auth.Secret, error) {
	if err := ctx.Err(); err != nil {
		return auth.Secret{}, err
	}
	if _, err := fmt.Fprint(prompter.output, label); err != nil {
		return auth.Secret{}, err
	}
	value, err := prompter.input.ReadString('\n')
	if err != nil && err != io.EOF {
		return auth.Secret{}, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return auth.Secret{}, fmt.Errorf("authentication input is required")
	}
	return auth.NewSecret(value), nil
}

func (prompter *terminalPrompter) PhoneNumber(ctx context.Context) (auth.Secret, error) {
	return prompter.read(ctx, "Phone number: ")
}

func (prompter *terminalPrompter) VerificationCode(ctx context.Context) (auth.Secret, error) {
	return prompter.read(ctx, "Verification code: ")
}

type credentials struct {
	provider auth.Provider
	store    auth.CredentialStore
	clock    auth.Clock
}

func (credentials credentials) Acquire(ctx context.Context) (app.PeopleAuthorization, error) {
	authorization, err := credentials.provider.Acquire(ctx)
	if err != nil {
		return app.PeopleAuthorization{}, err
	}
	credential, _, err := auth.LoadActive(ctx, credentials.store)
	if err != nil {
		return app.PeopleAuthorization{}, err
	}
	return app.PeopleAuthorization{
		Credential:         transport.Credential(authorization.AccessToken.Reveal()),
		AccountIdentity:    authorization.AccountIdentity.Reveal(),
		InstallationSecret: []byte(credential.InstallationSecret.Reveal()),
	}, nil
}

func (credentials credentials) InspectCredentials(ctx context.Context) (domain.AuthState, error) {
	credential, _, err := auth.LoadActive(ctx, credentials.store)
	if auth.IsErrorType(err, domain.ErrorAuthRequired) {
		return domain.AuthState{TokenState: domain.TokenStateMissing}, nil
	}
	if err != nil {
		return domain.AuthState{}, err
	}
	state := domain.TokenStateUnknown
	if credential.AccessTokenExpires != nil {
		now := credentials.clock.Now()
		switch {
		case !credential.AccessTokenExpires.After(now):
			state = domain.TokenStateExpired
		case !credential.AccessTokenExpires.After(now.Add(5 * time.Minute)):
			state = domain.TokenStateExpiring
		default:
			state = domain.TokenStateHealthy
		}
	}
	return domain.AuthState{Authenticated: true, TokenState: state, ExpiresAt: credential.AccessTokenExpires}, nil
}

func NewCLI(stdin io.Reader, stdout, stderr io.Writer, isTerminal bool) (*cli.CLI, error) {
	root, err := dataRoot(runtime.GOOS, os.UserHomeDir, os.Getenv)
	if err != nil {
		return nil, err
	}
	return newCLI(context.Background(), cliConfig{stdin: stdin, stdout: stdout, stderr: stderr, isTerminal: isTerminal, root: root})
}

func newCLI(ctx context.Context, config cliConfig) (*cli.CLI, error) {
	built, err := build(ctx, config.root, config.store, config.stderr, &terminalPrompter{input: bufio.NewReader(config.stdin), output: config.stderr})
	if err != nil {
		return nil, err
	}
	return cli.New(cli.Config{Catalog: built.catalog, Application: built.service, Stdin: config.stdin, Stdout: config.stdout, Stderr: config.stderr, IsTerminal: config.isTerminal})
}

func NewMCP(ctx context.Context, diagnostics io.Writer) (*mcp.Server, error) {
	root, err := dataRoot(runtime.GOOS, os.UserHomeDir, os.Getenv)
	if err != nil {
		return nil, err
	}
	built, err := build(ctx, root, nil, diagnostics, unavailablePrompter{})
	if err != nil {
		return nil, err
	}
	return mcp.NewServer(built.catalog, built.service, mcp.Options{
		Diagnostics:   diagnostics,
		ServerVersion: version.Current().CLIVersion,
	}), nil
}

type unavailablePrompter struct{}

func (unavailablePrompter) PhoneNumber(context.Context) (auth.Secret, error) {
	return auth.Secret{}, fmt.Errorf("interactive authentication is unavailable")
}
func (unavailablePrompter) VerificationCode(context.Context) (auth.Secret, error) {
	return auth.Secret{}, fmt.Errorf("interactive authentication is unavailable")
}

func build(ctx context.Context, root string, selected auth.CredentialStore, diagnostics io.Writer, prompter auth.LoginPrompter) (graph, error) {
	catalog, err := command.DefaultCatalog()
	if err != nil {
		return graph{}, fmt.Errorf("compose catalog: %w", err)
	}
	current := version.Current()
	if current.CLIVersion == "" || current.CommandContractRevision == "" || current.TransportContractRevision == "" {
		return graph{}, fmt.Errorf("compose version: empty contract revision")
	}
	gates, err := app.DefaultGateManifest()
	if err != nil {
		return graph{}, fmt.Errorf("compose gates: %w", err)
	}
	if root == "" {
		return graph{}, fmt.Errorf("compose credentials: empty data root")
	}

	fileStore := credentialstore.NewFileStore(root)
	osStore, osErr := credentialstore.DefaultOSStore()
	var platformStore auth.CredentialStore
	if osErr == nil {
		platformStore = osStore
	}
	if selected == nil {
		selected, err = selectCredentialStore(ctx, root, platformStore, diagnostics)
		if err != nil {
			return graph{}, fmt.Errorf("compose credentials: %w", err)
		}
	}
	cleanup := []auth.CredentialStore{fileStore}
	if osStore != nil {
		cleanup = append(cleanup, osStore)
	}
	clock := systemClock{}
	provider := auth.NewProvider(auth.ProviderConfig{
		Store: selected, CleanupStores: cleanup, Transport: firebaseauth.BlockedTransport{},
		Coordinator: credentialstore.NewFileCoordinator(root, lockTimeout), Clock: clock,
		Diagnostics: diagnosticWriter{diagnostics}, Gates: gates, ClearBackendMarker: (credentialstore.Selector{Root: root}).ClearMarker,
	})
	credentials := credentials{provider: provider, store: selected, clock: clock}
	service := app.NewService(catalog, gates)
	callableClient := callable.New(callable.Config{})
	firestoreClient := firestore.New(firestore.Config{})
	posterClient := poster.New(poster.Config{})
	cursorKey := make([]byte, 32)
	if _, err := rand.Read(cursorKey); err != nil {
		return graph{}, fmt.Errorf("compose cursor key: %w", err)
	}
	cursors, err := app.NewCursorCodec(cursorKey)
	if err != nil {
		return graph{}, fmt.Errorf("compose cursor codec: %w", err)
	}
	if err := app.BindAuthOperations(service, provider, prompter); err != nil {
		return graph{}, err
	}
	if err := app.BindEventOperations(service, provider, callableClient, firestoreClient, posterClient, cursors); err != nil {
		return graph{}, err
	}
	if err := app.BindPeopleOperations(service, credentials, callableClient); err != nil {
		return graph{}, err
	}
	if err := app.BindBlastOperation(service, provider, callableClient, firestoreClient); err != nil {
		return graph{}, err
	}
	if err := app.BindPosterOperations(service, posterClient, cursors); err != nil {
		return graph{}, err
	}
	if err := app.BindUtilityOperations(service, app.UtilityOperationsConfig{
		Credentials: credentials,
		Version:     domain.VersionResult{CLIVersion: current.CLIVersion, CommandContractRevision: current.CommandContractRevision, TransportContractRevision: current.TransportContractRevision},
	}); err != nil {
		return graph{}, err
	}
	return graph{catalog: catalog, service: service}, nil
}

func selectCredentialStore(ctx context.Context, root string, osStore auth.CredentialStore, diagnostics io.Writer) (auth.CredentialStore, error) {
	selector := credentialstore.Selector{
		Root:        root,
		OS:          osStore,
		File:        credentialstore.NewFileStore(root),
		Diagnostics: diagnosticWriter{diagnostics},
	}
	if osStore != nil {
		selector.Probe = func(ctx context.Context) error {
			bounded, cancel := context.WithTimeout(ctx, credentialProbeTimeout)
			defer cancel()
			_, err := osStore.Load(bounded, auth.SlotA)
			return err
		}
	}
	return selector.Select(ctx)
}

func dataRoot(goos string, userHomeDir func() (string, error), getenv func(string) string) (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve data root: %w", err)
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "partiful"), nil
	case "linux":
		if root := getenv("XDG_DATA_HOME"); root != "" {
			return filepath.Join(root, "partiful"), nil
		}
		return filepath.Join(home, ".local", "share", "partiful"), nil
	case "windows":
		if root := getenv("LOCALAPPDATA"); root != "" {
			return filepath.Join(root, "Partiful"), nil
		}
		return "", fmt.Errorf("resolve data root: LOCALAPPDATA is unavailable")
	default:
		return "", fmt.Errorf("resolve data root: unsupported platform")
	}
}
