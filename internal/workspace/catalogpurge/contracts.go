package catalogpurge

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

type Kind string

const (
	Artifact Kind = "artifact"
	Media    Kind = "media"
)

func (k Kind) Valid() bool { return k == Artifact || k == Media }

type Request struct {
	Kind               Kind
	Force, DryRun, Yes bool
}

type Entry struct {
	Name, Identity string
	ReferencedBy   []string
	ReservedBy     []string
}

func (e Entry) Selected(force bool) bool {
	return force || len(e.ReferencedBy) == 0 && len(e.ReservedBy) == 0
}

type Plan struct {
	Kind    Kind
	Force   bool
	Entries []Entry
}

type Result struct {
	Plan   Plan
	DryRun bool
}

type References func(context.Context, Kind, desiredstate.Sources) ([]string, error)

type Store interface {
	PlanCatalogPurge(context.Context, Kind, bool, References) (Plan, error)
	CommitCatalogPurge(context.Context, Plan, References) (int, error)
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}
