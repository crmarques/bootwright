package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

type StoreAccess interface {
	Context(context.Context, string) (storage.ContextSnapshot, error)
	View(context.Context, storage.Context, bool, func(storage.StoreSession, storage.Selection) error) error
	Mutate(context.Context, storage.Context, func(storage.StoreSession, storage.Selection) error) error
}

type Materializer interface {
	Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error)
	File(context.Context, secrets.Declaration) (secrets.Material, error)
	Generate(context.Context, secrets.Declaration) (secrets.Material, error)
	Validate(context.Context, secrets.Declaration, secrets.Material) error
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type MutationResult struct {
	Context   storage.Context
	Name      string
	Changed   int
	Unchanged int
	Parts     []secrets.Part
}

type CheckRow struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Source  string         `json:"source"`
	Parts   []secrets.Part `json:"parts"`
	Status  string         `json:"status"`
	Version *string        `json:"version"`
}
type CheckResult struct {
	Context storage.Context `json:"context"`
	Secrets []CheckRow      `json:"secrets"`
}
type ListRow struct {
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Source         string         `json:"source"`
	Parts          []secrets.Part `json:"parts"`
	State          string         `json:"state"`
	CurrentVersion *string        `json:"currentVersion"`
	BoundVersions  int            `json:"boundVersions"`
}
type ListResult struct {
	Context storage.Context `json:"context"`
	Secrets []ListRow       `json:"secrets"`
}

type RevealResult struct {
	Material secrets.Material
	Part     secrets.Part
}
