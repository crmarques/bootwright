//go:build linux && amd64

package contextfs

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// maxContextStateStages is how many abandoned publication stages a context's
// state directory may hold beside its own entries and still be collected.
const maxContextStateStages = 16

// stageName reports whether name is a publication stage: pending- and 32
// lowercase hexadecimal digits, with the .json suffix unless bare admits the
// stage form without one.
func stageName(name string, bare bool) bool {
	if base, found := strings.CutSuffix(name, ".json"); found {
		return identifier(base, "pending-")
	}
	return bare && identifier(name, "pending-")
}

// collectStages removes the publication stages a killed command left in dir.
// It runs only under the exclusive root lock, and every writer of such a stage
// holds the root lock for its whole command, so no stage found here has a live
// writer. It removes only a private regular file named as a stage, on the
// directory's device and within bound, and leaves everything else to the
// verification that refuses it; it descends into subdirectories only while
// depth admits one.
func (s *Store) collectStages(ctx context.Context, dir *directory, maximum int, bound int64, bare bool, depth int) error {
	names, err := directoryNames(dir, maximum)
	if err != nil {
		return nil
	}
	removed := false
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := openRelative(dir, name, pathHandle, 0)
		if err != nil {
			continue
		}
		stat, err := statHandle(entry)
		entry.Close()
		if err != nil {
			continue
		}
		if stat.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			if depth < 0 || depth >= maxOperationSegments {
				continue
			}
			nested, err := openDirectory(dir, name)
			if err != nil {
				continue
			}
			err = s.collectStages(ctx, nested, maximum, bound, bare, depth+1)
			nested.file.Close()
			if err != nil {
				return err
			}
			continue
		}
		if !stageName(name, bare) || stat.Dev != dir.identity.Dev || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) || stat.Size > bound {
			continue
		}
		if err := s.checkpoint(ctx, checkpointBeforeStageCollection); err != nil {
			return err
		}
		if err := unlinkVerified(dir, name, stat, false); err != nil {
			return state("abandoned publication stage could not be removed: " + filepath.Join(dir.path, name))
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(ctx, dir)
}

// collectContextStages removes the stages in a leased context's state
// directory and, with subtrees, in its operation, run and trust areas.
func (s *Store) collectContextStages(ctx context.Context, dir *directory, subtrees bool) error {
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return nil
	}
	defer runtime.file.Close()
	if err := s.collectStages(ctx, runtime, maxContextStateEntries+maxContextStateStages, maxRecord, false, -1); err != nil {
		return err
	}
	if !subtrees {
		return nil
	}
	for _, name := range []string{"operations", "runs", trustSubtree} {
		area, err := openDirectory(runtime, name)
		if err != nil {
			continue
		}
		err = s.collectStages(ctx, area, maxOperationEntries, maxOperationRecord, true, 0)
		area.file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// collectControllerStages removes the stages in the attributed controller
// directory. A store with no attributed controller directory, or one that does
// not verify, has nothing to collect.
func (s *Store) collectControllerStages(ctx context.Context, root *directory, registry contexts.Registry) error {
	dir, err := openControllerDirectory(root, registry)
	if err != nil {
		return nil
	}
	defer dir.file.Close()
	return s.collectStages(ctx, dir, maxControllerStages+2, maxControllerState, false, -1)
}
