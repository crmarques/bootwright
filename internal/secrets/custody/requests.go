package custody

import (
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type SetRequest struct {
	ContextName      string
	Name             string
	Input            secrets.Input
	SkipConfirmation bool
}

type GenerateRequest struct {
	ContextName string
	Name        string
	Renew       bool
}

type CheckRequest struct{ ContextName string }

type ListRequest struct{ ContextName string }

type ShowRequest struct {
	ContextName string
	Name        string
	Part        secrets.Part
}

type DeleteRequest struct {
	ContextName      string
	Name             string
	SkipConfirmation bool
}

type BindRequest struct {
	ContextName string
	Names       []string
}

type BindingRequest struct {
	ContextName string
	BindingID   string
}

type MutationResult struct {
	Context   secretstore.Context
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
	// Sequence is the version's per-secret ordinal, zero when no version
	// applies. The identifier stays the durable reference.
	Sequence int `json:"sequence"`
}

type CheckResult struct {
	Context secretstore.Context `json:"context"`
	Secrets []CheckRow          `json:"secrets"`
}

type ListRow struct {
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Source         string         `json:"source"`
	Parts          []secrets.Part `json:"parts"`
	State          string         `json:"state"`
	CurrentVersion *string        `json:"currentVersion"`
	// CurrentSequence is the current version's per-secret ordinal, zero when no
	// current version applies.
	CurrentSequence int `json:"currentSequence"`
	BoundVersions   int `json:"boundVersions"`
}

type ListResult struct {
	Context secretstore.Context `json:"context"`
	Secrets []ListRow           `json:"secrets"`
}

type RevealResult struct {
	Material secrets.Material
	Part     secrets.Part
}
