package contexts

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type Mode string

const (
	Initializing Mode = "initializing"
	Ready        Mode = "ready"
	Deleting     Mode = "deleting"
)

// RegistryVersion is the durable context-registry format. Earlier formats named
// each context by an allocated identity that no longer exists, so a store
// written by them is refused rather than converted.
const RegistryVersion = 5

// Registry is an atomic context snapshot. A context's name is its identity, so
// the registry holds no separate identifier and needs no allocation state.
type Registry struct {
	Version    int                  `json:"version"`
	Contexts   []Record             `json:"contexts"`
	Controller ControllerDescriptor `json:"-"`
}

// ControllerDescriptor attributes the independently versioned shared subtree.
// Only confirmed setup may introduce it; ordinary context writes preserve it.
type ControllerDescriptor struct {
	Version         int    `json:"version"`
	Mode            string `json:"mode"`
	DirectoryDevice uint64 `json:"directoryDevice"`
	DirectoryInode  uint64 `json:"directoryInode"`
}

type Record struct {
	Name                 string `json:"name"`
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
	ReadInputs(context.Context, string) (desiredstate.Sources, error)
}

type Repository interface {
	InputRepository
	CheckInputDirectory(context.Context, string) error
	View(context.Context) (Registry, error)
	// Transact holds the root lock throughout the callback and commit. Roots
	// exclude the state directory from every admitted input directory. Create is
	// reserved for context init and permits exact initial-registry recovery.
	Transact(context.Context, bool, []string, func(Transaction) error) error
	// TransactDeletion is the registry transaction of deleting one named
	// context. It verifies every other context as Transact does and leaves
	// the named one to its deletion, so a damaged context can be purged; its
	// transaction serves only the registry, that context's mutation state and
	// host reservations, and the deletion of its record.
	TransactDeletion(context.Context, string, func(Transaction) error) error
}

type Transaction interface {
	Registry() Registry
	Reserve(context.Context, string, string, []byte) (Record, error)
	Configuration(context.Context, string) ([]byte, error)
	InitializeSecrets(context.Context, string, func(secretstore.Area) error) error
	Publish(context.Context, string, string, desiredstate.Sources) (string, error)
	// CheckControllerInput preserves an existing controller Machine binding
	// when replacing admitted input: it refuses input whose controller Machine
	// is not the one the context's binding records.
	CheckControllerInput(context.Context, string, string) error
	// Unchanged reports whether admitted input, with its Environment
	// directory, freezes exactly the context's selected revision, so an
	// update keeps that revision rather than publishing another.
	Unchanged(context.Context, string, string, desiredstate.Sources) (bool, error)
	// MutationState acquires and holds the context lease until transaction
	// completion, then returns bounded Reconciliation-owned evidence bytes.
	MutationState(context.Context, string) ([]byte, error)
	// HostReservations names the host resource keys the controller record
	// reserves for one context. Delete releases them with the context.
	HostReservations(context.Context, string) ([]string, error)
	Delete(context.Context, Record) error
	Commit(context.Context, Registry) error
}

// Disposition is what the guard's reading of the mutation evidence permits:
// an input update, the context's disposal, and whether that evidence records a
// completed apply, which refuses changed input until what it owns is taken
// back.
type Disposition struct {
	Update  bool
	Dispose bool
	Applied bool
}

type ContextMutationGuard interface {
	Check(context.Context, []byte) (Disposition, error)
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

// Presenter shows what an input update or a deletion changes immediately
// before its ordinary confirmation, so the operator confirms what they read.
type Presenter interface {
	PresentUpdate(context.Context, UpdatePlan) error
	PresentDeletion(context.Context, DeletionPlan) error
}

// SelectionVersion is the per-user current-context record format. The record
// holds only the selected name, which is the context's identity.
const SelectionVersion = 2

type Selection struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

type SelectionStore interface {
	Read(context.Context) (Selection, error)
	Write(context.Context, Selection) error
	Clear(context.Context, Selection) error
}

type ConfigurationReader interface {
	ReadFile(context.Context, string, int) ([]byte, error)
}

type Options struct {
	Selection             SelectionStore
	ConfigurationReader   ConfigurationReader
	InitializeSecrets     func(context.Context, Record, secretstore.Area) error
	ValidateConfiguration func(context.Context, Configuration) error
	Presenter             Presenter
}

// Inputs acquires an immutable revision for an explicit or current context.
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
	if name == "" {
		if i.Selection == nil {
			return desiredstate.Sources{}, StateError("current context selection is not configured")
		}
		selected, err := i.Selection.Read(ctx)
		if err != nil {
			return desiredstate.Sources{}, err
		}
		if selected.Name == "" {
			return desiredstate.Sources{}, NoSelection()
		}
		name = selected.Name
	}
	return i.Repository.ReadInputs(ctx, name)
}

func StateError(message string) error { return diagnostics.NewFailure("context.state", message, "") }

func StateErrorWithRemediation(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("context.state", message, "", remediation)
}

func UnsafeDelete(message string) error {
	return diagnostics.NewFailure("context.unsafe-delete", message, "")
}

func UnsafeDeleteWithRemediation(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("context.unsafe-delete", message, "", remediation)
}

// ErrLostContext marks a deletion's refusal of a ready context whose directory
// is gone: what it owned cannot be listed, so only the explicit orphan
// acknowledgement abandons it. A repository's refusal matches it with
// errors.Is and reports the LostContext diagnostic.
var ErrLostContext = errors.New("the context's directory is gone")

// LostContext is the diagnostic of that refusal; entry is the directory
// relative to the state root, and cause the kernel's answer.
func LostContext(name, entry, cause string) error {
	return UnsafeDeleteWithRemediation(
		"context "+name+" has lost its directory ("+entry+": "+cause+"), so what it owned cannot be listed",
		"abandon whatever it owned with bootwright context delete --name "+name+" --purge --allow-orphans, or restore the whole store from a matching backup")
}

// Configuration is controller configuration, separate from Environment input.
type Configuration struct {
	Name        string
	SecretStore SecretStoreConfiguration
}
type SecretStoreConfiguration struct{ Type string }
