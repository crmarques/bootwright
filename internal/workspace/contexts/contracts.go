package contexts

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

type Mode string

const (
	Initializing Mode = "initializing"
	Ready        Mode = "ready"
	Deleting     Mode = "deleting"
)

type Summary struct {
	Name       string
	ID         string
	Mode       Mode
	Current    bool
	Configured bool
}

type AdmissionResult struct {
	Context      Summary
	Counts       compilation.Counts
	FilesCopied  int
	InputChanged bool
	Diagnostics  []desiredstate.Diagnostic
}

type UseResult struct{ Context Summary }

type ListResult struct{ Contexts []Summary }

type CurrentResult struct{ Context Summary }

type DeleteResult struct {
	Name           string
	ID             string
	Outcome        string
	CurrentCleared bool
}

// Registry is an atomic context snapshot. Reserved identities remain after deletion.
type Registry struct {
	Version    int        `json:"version"`
	Identities []Identity `json:"identities"`
	Contexts   []Record   `json:"contexts"`
}

type Identity struct {
	ID string `json:"id"`
}

type Record struct {
	Name                 string `json:"name"`
	ID                   string `json:"id"`
	EnvironmentDirectory string `json:"environmentDirectory"`
	Revision             string `json:"revision"`
	Mode                 Mode   `json:"mode"`
	SecretStoreType      string `json:"secretStoreType"`
	DirectoryDevice      uint64 `json:"directoryDevice"`
	DirectoryInode       uint64 `json:"directoryInode"`
}

type DirectoryReader interface {
	ReadDirectory(context.Context, string) (desiredstate.Sources, error)
}

type Compiler interface {
	Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}

type InputRepository interface {
	ReadInputs(context.Context, string, string) (desiredstate.Sources, error)
}

type Repository interface {
	InputRepository
	CheckInputDirectory(context.Context, string) error
	View(context.Context) (Registry, error)
	// Transact holds the root lock throughout the callback and commit. Roots
	// exclude the state directory from every admitted input directory.
	Transact(context.Context, bool, []string, func(Transaction) error) error
}

type Transaction interface {
	Registry() Registry
	Reserve(context.Context, string, string, []byte) (Record, error)
	Configuration(context.Context, string) ([]byte, error)
	InitializeSecrets(context.Context, string, func(storage.Area) error) error
	Publish(context.Context, string, string, desiredstate.Sources) (string, error)
	// MutationState acquires and holds the context lease until transaction
	// completion, then returns bounded Reconciliation-owned evidence bytes.
	MutationState(context.Context, string) ([]byte, error)
	Delete(context.Context, Record) error
	Commit(context.Context, Registry) error
}

type Disposition struct {
	Update  bool
	Dispose bool
}

type ContextMutationGuard interface {
	Check(context.Context, []byte) (Disposition, error)
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type Selection struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	ID      string `json:"id"`
}

type SelectionStore interface {
	Read(context.Context) (Selection, error)
	Write(context.Context, Selection) error
	Clear(context.Context, Selection) error
}

type ConfigurationReader interface {
	ReadConfiguration(context.Context, string) ([]byte, error)
}

type Options struct {
	Selection             SelectionStore
	ConfigurationReader   ConfigurationReader
	InitializeSecrets     func(context.Context, Record, storage.Area) error
	ValidateConfiguration func(context.Context, Configuration) error
}

// Inputs acquires an immutable revision for an explicit or identity-bound current context.
type Inputs struct {
	Repository InputRepository
	Selection  SelectionStore
}

func (i Inputs) ReadInputs(ctx context.Context, name string) (desiredstate.Sources, error) {
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, err
	}
	if i.Repository == nil {
		return desiredstate.Sources{}, StateError("context input repository is not configured")
	}
	id := ""
	if name == "" {
		if i.Selection == nil {
			return desiredstate.Sources{}, StateError("current context selection is not configured")
		}
		selected, err := i.Selection.Read(ctx)
		if err != nil {
			return desiredstate.Sources{}, err
		}
		if selected.Name == "" || selected.ID == "" {
			return desiredstate.Sources{}, StateError("no current context is selected; use context use --name <name>")
		}
		name, id = selected.Name, selected.ID
	}
	return i.Repository.ReadInputs(ctx, name, id)
}

func StateError(message string) error { return desiredstate.NewFailure("context.state", message, "") }

func UnsafeDelete(message string) error {
	return desiredstate.NewFailure("context.unsafe-delete", message, "")
}

// Configuration is controller configuration, separate from Environment input.
type Configuration struct {
	Name        string
	SecretStore SecretStoreConfiguration
}
type SecretStoreConfiguration struct{ Type string }
