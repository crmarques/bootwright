package contexts

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type Mode string

const (
	Active       Mode = "active"
	RecoveryOnly Mode = "recoveryOnly"
)

type Summary struct {
	Name    string
	ID      string
	Mode    Mode
	Current bool
}

type AdmissionResult struct {
	Context     Summary
	Counts      compilation.Counts
	FilesCopied int
	Diagnostics []desiredstate.Diagnostic
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

// Registry is an atomic selection snapshot. Identities remain after deletion.
type Registry struct {
	Version    int        `json:"version"`
	Current    string     `json:"current"`
	Identities []Identity `json:"identities"`
	Contexts   []Record   `json:"contexts"`
}

type Identity struct {
	EnvironmentDirectory string `json:"environmentDirectory"`
	ID                   string `json:"id"`
}

type Record struct {
	Name                 string `json:"name"`
	ID                   string `json:"id"`
	EnvironmentDirectory string `json:"environmentDirectory"`
	Revision             string `json:"revision"`
	Mode                 Mode   `json:"mode"`
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
	// exclude the state directory from every admitted input directory.
	Transact(context.Context, bool, []string, func(Transaction) error) error
}

type Transaction interface {
	Registry() Registry
	Reserve(context.Context, string) (string, error)
	Publish(context.Context, string, string, desiredstate.Sources) (string, error)
	// MutationState acquires and holds the context lease until transaction
	// completion, then returns bounded Reconciliation-owned evidence bytes.
	MutationState(context.Context, string) ([]byte, error)
	Archive(context.Context, Record, string) error
	Commit(context.Context, Registry) error
}

type Disposition struct {
	Update   bool
	Dispose  bool
	Recovery bool
}

type ContextMutationGuard interface {
	Check(context.Context, []byte) (Disposition, error)
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

// Inputs only acquires existing immutable input; it never compiles or writes.
type Inputs struct{ Repository InputRepository }

func (i Inputs) ReadInputs(ctx context.Context, name string) (desiredstate.Sources, error) {
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, err
	}
	if i.Repository == nil {
		return desiredstate.Sources{}, StateError("context input repository is not configured")
	}
	return i.Repository.ReadInputs(ctx, name)
}

func StateError(message string) error { return desiredstate.NewFailure("context.state", message, "") }

func UnsafeDelete(message string) error {
	return desiredstate.NewFailure("context.unsafe-delete", message, "")
}
