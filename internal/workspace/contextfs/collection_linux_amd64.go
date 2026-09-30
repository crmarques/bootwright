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
		stat, err := listedEntry(dir, name)
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
		collected, err := s.collectStage(ctx, dir, name, stat, bound, bare)
		if err != nil {
			return err
		}
		removed = removed || collected
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(ctx, dir)
}

// listedEntry reads the identity of one listed entry without following it.
func listedEntry(dir *directory, name string) (syscall.Stat_t, error) {
	entry, err := openRelative(dir, name, pathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, err
	}
	defer entry.Close()
	return statHandle(entry)
}

// collectStage removes one listed entry only when it is proved a publication
// stage of dir: a private regular file named as one, on dir's device and
// within bound. It reports whether it removed the entry.
func (s *Store) collectStage(ctx context.Context, dir *directory, name string, stat syscall.Stat_t, bound int64, bare bool) (bool, error) {
	if !stageName(name, bare) || stat.Dev != dir.identity.Dev || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) || stat.Size > bound {
		return false, nil
	}
	if err := s.checkpoint(ctx, checkpointBeforeStageCollection); err != nil {
		return false, err
	}
	if err := unlinkVerified(dir, name, stat, false); err != nil {
		return false, state("abandoned publication stage could not be removed: " + filepath.Join(dir.path, name))
	}
	return true, nil
}

// collectRegistryStages removes the stages a killed registry replacement left
// in the root. It runs only in a registry transaction, under the exclusive
// root lock and after the root's entries were admitted, so no stage it finds
// has a live writer; init's recovery artifact never meets it, because that
// stage exists only while registry.json does not.
func (s *Store) collectRegistryStages(ctx context.Context, root *directory) error {
	names, err := rootEntryNames(root, maxContexts+1)
	if err != nil {
		return nil
	}
	removed := false
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !stageName(name, false) {
			continue
		}
		stat, err := listedEntry(root, name)
		if err != nil {
			continue
		}
		collected, err := s.collectStage(ctx, root, name, stat, maxRegistry, false)
		if err != nil {
			return err
		}
		removed = removed || collected
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(ctx, root)
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
	return s.collectStages(ctx, dir, maxControllerEntries, maxControllerState, false, -1)
}
