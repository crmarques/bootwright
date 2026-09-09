// Package selectionfs stores the invoking account's context selection.
package selectionfs

import (
	"context"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Options must come from the verified local account, never HOME or XDG variables.
type Options struct {
	UID, GID   int
	Home       string
	Groups     []uint32
	Executable string
}

type Store struct{ options Options }

func New(options Options) *Store {
	options.Groups = append([]uint32(nil), options.Groups...)
	return &Store{options: options}
}

func (s *Store) Read(ctx context.Context) (contexts.Selection, error) {
	return s.perform(ctx, "read", contexts.Selection{})
}

func (s *Store) Write(ctx context.Context, selection contexts.Selection) error {
	_, err := s.perform(ctx, "write", selection)
	return err
}

// Clear removes only the exact selection observed by the caller.
func (s *Store) Clear(ctx context.Context, expected contexts.Selection) error {
	_, err := s.perform(ctx, "clear", expected)
	return err
}

func state(message string) error { return contexts.StateError(message) }
