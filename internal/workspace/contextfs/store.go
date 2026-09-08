// Package contextfs persists Workspace contexts beneath a verified private root.
package contextfs

import (
	"context"
	"crypto/rand"
	"io"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	maxRegistry     = 8 << 20
	maxManifest     = 4 << 20
	maxAllManifests = 32 << 20
	maxRecord       = 64 << 10
	maxPath         = 4096
	maxIdentities   = 4096
	maxRevisions    = 4096
)

type Options struct{ Root string }

// Store has no effects until a repository method is called. A Store is safe for
// concurrent use; transaction state and held handles belong to one invocation.
type Store struct {
	options Options
	random  io.Reader
	// fail is a test-only injection boundary immediately before named effects.
	fail func(string) error
}

func New(options Options) *Store { return &Store{options: options, random: rand.Reader} }

func (s *Store) checkpoint(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.fail != nil {
		return s.fail(name)
	}
	return nil
}

func state(message string) error { return contexts.StateError(message) }

func emptyRegistry() contexts.Registry {
	return contexts.Registry{Version: 1, Identities: []contexts.Identity{}, Contexts: []contexts.Record{}}
}

func cloneRegistry(r contexts.Registry) contexts.Registry {
	r.Identities = append([]contexts.Identity{}, r.Identities...)
	r.Contexts = append([]contexts.Record{}, r.Contexts...)
	return r
}
