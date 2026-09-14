// Package contextfs persists Workspace contexts beneath a verified private root.
package contextfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	maxRegistry      = 8 << 20
	maxManifest      = 4 << 20
	maxAllManifests  = 32 << 20
	maxRecord        = 64 << 10
	maxPath          = 4096
	maxContexts      = 4096
	maxRevisions     = 4096
	maxSecretEntries = 32768
	maxSecretBytes   = 256 << 20
)

const pristineMutation = "{\"version\":1,\"operation\":\"none\",\"ownership\":\"none\"}\n"

// Ownership is an explicit filesystem test seam; production uses UID and GID zero.
type Ownership struct{ UID, GID uint32 }

type Options struct {
	Root  string
	Owner *Ownership
}

// Store has no effects until a repository method is called. A Store is safe for
// concurrent use; transaction state and held handles belong to one invocation.
type Store struct {
	options Options
	random  io.Reader
	// fail is a test-only injection boundary immediately before named effects.
	fail func(string) error
}

func New(options Options) *Store {
	if options.Owner != nil {
		owner := *options.Owner
		options.Owner = &owner
	}
	return &Store{options: options, random: rand.Reader}
}

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
	return contexts.Registry{Version: contexts.RegistryVersion, Contexts: []contexts.Record{}}
}

func cloneRegistry(r contexts.Registry) contexts.Registry {
	r.Contexts = append([]contexts.Record{}, r.Contexts...)
	return r
}

func (s *Store) candidate(prefix string) (string, error) {
	var bytes [16]byte
	if s.random == nil {
		return "", state("context identity randomness is unavailable")
	}
	if _, err := io.ReadFull(s.random, bytes[:]); err != nil {
		return "", state("context identity randomness is unavailable")
	}
	return prefix + hex.EncodeToString(bytes[:]), nil
}
