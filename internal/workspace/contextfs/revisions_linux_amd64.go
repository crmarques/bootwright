//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Only pristine contexts have a complete proof that no operation needs an old
// input revision. Future operation formats must provide their own retention
// proof before this collector can remove their inputs.
func (t *transaction) collectRevisions(ctx context.Context, id string) error {
	dir, held := t.leases[id]
	if !held || !bytes.Equal(t.evidence[id], []byte(pristineMutation)) {
		return nil
	}
	if err := t.checkControllerRecovery(ctx, id); err != nil {
		return err
	}
	record, err := t.record(id)
	if err != nil {
		return err
	}
	if record.Mode == contexts.Deleting {
		return nil
	}
	input, err := openDirectory(dir, "desired-state")
	if err != nil {
		return err
	}
	defer input.file.Close()
	revisions, err := openDirectory(input, "revisions")
	if err != nil {
		return err
	}
	defer revisions.file.Close()
	names, err := directoryNames(revisions, maxRevisions)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !identifier(name, "rev-") {
			return state("retained input contains an unknown entry")
		}
	}
	for _, name := range names {
		if name == record.Revision {
			continue
		}
		if err := t.store.checkpoint(ctx, "before-revision-cleanup"); err != nil {
			return err
		}
		if err := t.collectRevision(ctx, dir, revisions, name, id); err != nil {
			return err
		}
	}
	return nil
}

func (t *transaction) collectRevision(ctx context.Context, owner, revisions *directory, name, id string) error {
	dir, err := openDirectory(revisions, name)
	if err != nil {
		return err
	}
	defer dir.file.Close()
	path := "desired-state/revisions/" + name
	remaining := desiredstate.MaxFiles + desiredstate.MaxMarkers + 1
	if err := t.store.walkContextTree(ctx, dir, path, inspectContextTree, &remaining); err != nil {
		return err
	}
	if err := t.store.checkpoint(ctx, "before-revision-remove"); err != nil {
		return err
	}
	guard := func(ctx context.Context) error { return t.verifyRevisionCollection(ctx, owner, id) }
	if err := guard(ctx); err != nil {
		return err
	}
	remaining = desiredstate.MaxFiles + desiredstate.MaxMarkers + 1
	if err := t.store.walkContextTreeWithRemovalGuard(ctx, dir, path, removeContextTree, &remaining, guard); err != nil {
		return err
	}
	if err := t.store.checkpoint(ctx, "before-revision-rmdir"); err != nil {
		return err
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if err := unlinkVerified(revisions, name, dir.identity, true); err != nil {
		return err
	}
	return t.store.syncDirectory(ctx, revisions)
}

func (t *transaction) verifyRevisionCollection(ctx context.Context, owner *directory, id string) error {
	if err := t.checkControllerRecovery(ctx, id); err != nil {
		return err
	}
	file, err := openRelative(t.root, "registry.json", pathHandle, 0)
	if err != nil {
		return state("context selection cannot be verified before input cleanup")
	}
	actual, err := statHandle(file)
	file.Close()
	if err != nil || t.expected == nil || !sameFile(actual, t.expected.identity) {
		return state("context selection changed before input cleanup")
	}
	evidence, err := readMutation(ctx, owner)
	if err != nil || !bytes.Equal(evidence, t.evidence[id]) {
		return state("context mutation evidence changed before input cleanup")
	}
	return t.root.verify()
}
