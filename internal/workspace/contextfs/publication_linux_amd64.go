//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"syscall"
)

type publicationOutcome int

const (
	publicationNotCommitted publicationOutcome = iota
	publicationCommitted
	publicationUnknown
)

// stagedPublication is how one record publication stages its bytes beside its
// target and proves its destination right before the one rename.
type stagedPublication struct {
	// subject names the record in the refusals the publication raises itself.
	subject string
	// suffix follows the stage's pending-<32 hex> name.
	suffix string
	// replace renames over an existing target; otherwise the target must be
	// absent and the rename never replaces anything.
	replace bool
	// prove refuses a destination that no longer holds what the caller
	// observed. It runs after the before checkpoint and before the recheck.
	prove func(context.Context) error
	// renameFailure replaces a failed rename's error; empty keeps it.
	renameFailure string
	// immutable is the read flag of the published file's read-backs.
	immutable bool
	bound     int
	// retain keeps what a failed write or an unrenamed stage leaves, for a
	// caller whose retry attributes it.
	retain bool
}

// publishStage stages data beside name and renames it there, so name appears
// only with complete, synchronized content. A failure before the rename is
// uncommitted and removes the stage unless retain is set; the removal takes no
// context, so a cancelled command still cleans up. Any failure after the
// rename is an unknown outcome.
func (s *Store) publishStage(ctx context.Context, parent *directory, name string, data []byte, p stagedPublication, before, after checkpoint) (publicationOutcome, error) {
	pending, created, err := s.createStage(ctx, parent, data, p)
	if err != nil {
		return publicationNotCommitted, err
	}
	renamed := false
	defer func() {
		if !renamed && !p.retain {
			discardCreated(parent, pending, created)
		}
	}()
	staged, matches := readsBack(ctx, parent, pending, data, p.bound, true)
	if !matches {
		return publicationNotCommitted, state("staged " + p.subject + " changed before publication")
	}
	if err := s.checkpoint(ctx, before); err != nil {
		return publicationNotCommitted, err
	}
	if p.prove != nil {
		if err := p.prove(ctx); err != nil {
			return publicationNotCommitted, err
		}
	}
	current, matches := readsBack(ctx, parent, pending, data, p.bound, true)
	if !matches || !sameFile(staged, current) {
		return publicationNotCommitted, state("staged " + p.subject + " was substituted")
	}
	if err := renameStage(parent, pending, name, p.replace); err != nil {
		if p.renameFailure != "" {
			return publicationNotCommitted, state(p.renameFailure)
		}
		return publicationNotCommitted, err
	}
	renamed = true
	if err := s.checkpoint(ctx, after); err != nil {
		return publicationUnknown, err
	}
	published, matches := readsBack(ctx, parent, name, data, p.bound, p.immutable)
	if !matches || !sameIdentity(staged, published) {
		return publicationUnknown, state("published " + p.subject + " is unsafe")
	}
	if err := s.syncDirectory(ctx, parent); err != nil {
		return publicationUnknown, err
	}
	final, matches := readsBack(ctx, parent, name, data, p.bound, p.immutable)
	if !matches || !sameFile(published, final) {
		return publicationUnknown, state("published " + p.subject + " is unsafe")
	}
	return publicationCommitted, nil
}

// createStage writes the stage under a fresh pending name, retrying a name
// another stage already holds.
func (s *Store) createStage(ctx context.Context, parent *directory, data []byte, p stagedPublication) (string, syscall.Stat_t, error) {
	for range 16 {
		candidate, err := s.candidate("pending-")
		if err != nil {
			return "", syscall.Stat_t{}, err
		}
		pending := candidate + p.suffix
		created, err := s.writeExclusiveIdentity(ctx, parent, pending, data, p.retain)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		return pending, created, err
	}
	return "", syscall.Stat_t{}, state(p.subject + " publication exhausted its collision limit")
}

// readsBack reports the identity of the named file and whether it holds
// exactly data. It clears what it read, because a secret area publishes
// through it.
func readsBack(ctx context.Context, parent *directory, name string, data []byte, bound int, immutable bool) (syscall.Stat_t, bool) {
	actual, identity, err := readBoundedIdentity(ctx, parent, name, bound, immutable)
	matches := err == nil && bytes.Equal(actual, data)
	clear(actual)
	return identity, matches
}

func renameStage(parent *directory, pending, name string, replace bool) error {
	if !replace {
		return renameNoReplaceAt(parent, pending, name)
	}
	if err := parent.verify(); err != nil {
		return err
	}
	return syscall.Renameat(int(parent.file.Fd()), pending, int(parent.file.Fd()), name)
}
