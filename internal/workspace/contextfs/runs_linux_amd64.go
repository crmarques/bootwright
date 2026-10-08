//go:build linux && amd64

package contextfs

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// maxBoundedRuns is how many bounded runs one context's runs area keeps. Each
// run's retained output is bounded at operationstore.MaxAdapterOutputBytes, so
// the output of the runs it keeps never reaches the area's byte bound.
const maxBoundedRuns = 16

const runsSubtree = "runs"

// OpenRun makes room among the newest runs this context's runs area keeps,
// then creates identity's directory exclusively and holds it until the
// callback that received this view returns, so no other run's retention
// removes a run still in progress. A failure leaves no directory this call
// created.
func (r *lifecycleRun) OpenRun(ctx context.Context, identity string) error {
	if err := r.runs.available(ctx, true); err != nil {
		return err
	}
	if !reconciliation.ValidRunID(identity) {
		return state("a bounded run's directory requires a run identity")
	}
	runs, release, err := r.runs.root(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	if err := r.runs.store.retireRuns(ctx, runs); err != nil {
		return err
	}
	run, err := r.runs.store.createRun(ctx, runs, identity)
	if err != nil {
		return err
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.released {
		run.file.Close()
		discardRun(runs, identity)
		return state("bounded lifecycle run capability has closed")
	}
	r.held = append(r.held, run)
	return nil
}

// release ends this view's holds on the runs it opened, so a later run's
// retention may remove them once they are among the oldest.
func (r *lifecycleRun) release() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.released = true
	for _, run := range r.held {
		run.file.Close()
	}
	r.held = nil
}

// boundedRun is one run directory as retention first read it.
type boundedRun struct {
	name     string
	identity syscall.Stat_t
}

// retireRuns removes the oldest runs no live run holds until fewer than
// maxBoundedRuns remain, so the run about to open is among the newest the area
// keeps. It syncs the area once, after every removal, because the first run
// over an area an earlier build filled may remove thousands.
func (s *Store) retireRuns(ctx context.Context, runs *directory) error {
	candidates, err := listRuns(ctx, runs)
	if err != nil {
		return err
	}
	removed, remaining := false, len(candidates)
	for _, candidate := range candidates {
		if remaining < maxBoundedRuns {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if retireRun(runs, candidate) {
			removed, remaining = true, remaining-1
		}
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(ctx, runs)
}

// listRuns lists, oldest first, the private directories named as runs. A run
// is ordered by its directory's modification time and then by name, never by
// its random identity alone. Every other entry is left out, and so is never
// removed or counted.
func listRuns(ctx context.Context, runs *directory) ([]boundedRun, error) {
	names, err := directoryNames(runs, maxOperationEntries)
	if err != nil {
		return nil, state("bounded run storage cannot be listed: " + heldEntry(runs))
	}
	candidates := make([]boundedRun, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !reconciliation.ValidRunID(name) {
			continue
		}
		run, err := openDirectory(runs, name)
		if err != nil {
			continue
		}
		candidates = append(candidates, boundedRun{name: name, identity: run.identity})
		run.file.Close()
	}
	slices.SortFunc(candidates, func(x, y boundedRun) int {
		return cmp.Or(
			cmp.Compare(x.identity.Mtim.Sec, y.identity.Mtim.Sec),
			cmp.Compare(x.identity.Mtim.Nsec, y.identity.Mtim.Nsec),
			strings.Compare(x.name, y.name),
		)
	})
	return candidates, nil
}

// retireRun removes one run only while this call holds it exclusively and it
// is still the directory listed, empty or holding only its own private output.
// A run another live run holds, one holding anything else and one whose output
// is a link, is linked elsewhere or lies on another device are left as they
// are. It reports whether the run is gone.
func retireRun(runs *directory, candidate boundedRun) bool {
	run, err := openDirectory(runs, candidate.name)
	if err != nil {
		return false
	}
	defer run.file.Close()
	if !sameIdentity(run.identity, candidate.identity) {
		return false
	}
	if syscall.Flock(int(run.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false
	}
	names, err := directoryNames(run, 1)
	if err != nil {
		return false
	}
	if len(names) == 1 {
		output, kept := runOutput(run, names[0])
		if !kept || unlinkVerified(run, lifecycle.RunOutputName, output, false) != nil {
			return false
		}
	}
	return unlinkVerified(runs, candidate.name, run.identity, true) == nil
}

// runOutput reports the identity of a run's one entry while it is the run's
// own output: a private, singly linked regular file on the run's device.
func runOutput(run *directory, name string) (syscall.Stat_t, bool) {
	if name != lifecycle.RunOutputName {
		return syscall.Stat_t{}, false
	}
	file, err := openRelative(run, name, pathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, false
	}
	stat, err := statHandle(file)
	file.Close()
	if err != nil || !private(stat, syscall.S_IFREG, run.identity.Uid, run.identity.Gid) || stat.Dev != run.identity.Dev {
		return syscall.Stat_t{}, false
	}
	return stat, true
}

// createRun creates one run's directory exclusively and holds it shared, so a
// retention running beside it never removes it. A directory another retention
// removed before the hold was taken refuses, and any failure once the
// directory exists removes it.
func (s *Store) createRun(ctx context.Context, runs *directory, identity string) (*directory, error) {
	if err := s.checkpoint(ctx, checkpointMkdir); err != nil {
		return nil, err
	}
	if err := runs.verify(); err != nil {
		return nil, err
	}
	if err := syscall.Mkdirat(int(runs.file.Fd()), identity, 0700); err != nil {
		return nil, state("bounded run directory could not be created exclusively: " + storeEntry(runs, identity))
	}
	run, err := s.holdRun(ctx, runs, identity)
	if err != nil {
		discardRun(runs, identity)
		return nil, err
	}
	return run, nil
}

func (s *Store) holdRun(ctx context.Context, runs *directory, identity string) (*directory, error) {
	location := storeEntry(runs, identity)
	run, err := openDirectory(runs, identity)
	if err != nil {
		return nil, state("bounded run directory is not a private directory: " + location)
	}
	if run.identity.Mode&0777 != 0700 {
		run.file.Close()
		return nil, state("new state directory does not have private creation permissions")
	}
	if syscall.Flock(int(run.file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil {
		run.file.Close()
		return nil, state("bounded run directory could not be held: " + location)
	}
	if run.verify() != nil {
		run.file.Close()
		return nil, state("bounded run directory was removed before it was held: " + location)
	}
	if err := s.syncDirectory(ctx, runs); err != nil {
		run.file.Close()
		return nil, err
	}
	return run, nil
}

// discardRun removes a run directory this call created and could not hand
// out, only while it is still the empty directory it was. It takes no context,
// so an interrupted run still cleans up, and its own failure is ignored: the
// refusal that caused it is the one to report.
func discardRun(runs *directory, identity string) {
	file, err := openRelative(runs, identity, pathHandle, 0)
	if err != nil {
		return
	}
	stat, err := statHandle(file)
	file.Close()
	if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return
	}
	if unlinkVerified(runs, identity, stat, true) == nil {
		_ = runs.file.Sync()
	}
}
