//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// maxBoundedRuns is how many bounded runs one context's runs area keeps. Each
// run's retained output is bounded at operationstore.MaxAdapterOutputBytes.
const maxBoundedRuns = 16

const (
	runsSubtree       = "runs"
	runPrefix         = "run-"
	outputSuffix      = ".output"
	maxRunAllocations = 16
)

func outputNumber(name, prefix string) (int, bool) {
	digits, found := strings.CutPrefix(name, prefix)
	if digits, found = strings.CutSuffix(digits, outputSuffix); !found {
		return 0, false
	}
	number, err := reconciliation.ParseNumber(digits)
	return number, err == nil
}

func outputName(prefix string, number int) (string, error) {
	digits, err := reconciliation.FormatNumber(number)
	return prefix + digits + outputSuffix, err
}

// OpenRun makes room among the newest runs this context's runs area keeps,
// then creates the next run's file exclusively and holds it until the callback
// that received this view returns, so no other run's retention removes a run
// still in progress. A failure leaves no file this call created.
func (r *lifecycleRun) OpenRun(ctx context.Context) (string, error) {
	if err := r.runs.available(ctx, true); err != nil {
		return "", err
	}
	runs, release, err := r.runs.root(ctx, true)
	if err != nil {
		return "", err
	}
	defer release()
	name, run, err := r.runs.store.openNextRun(ctx, runs)
	if err != nil {
		return "", err
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.released {
		discardRun(runs, name, run)
		return "", state("bounded lifecycle run capability has closed")
	}
	r.held = append(r.held, run)
	return name, nil
}

func (r *lifecycleRun) release() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.released = true
	for _, run := range r.held {
		run.Close()
	}
	r.held = nil
}

// openNextRun numbers the next run one past the highest the area holds or
// just retired, so a number is never reused while the area lives.
func (s *Store) openNextRun(ctx context.Context, runs *directory) (string, *os.File, error) {
	highest, err := s.retireRuns(ctx, runs)
	if err != nil {
		return "", nil, err
	}
	for attempt := 1; attempt <= maxRunAllocations; attempt++ {
		name, err := outputName(runPrefix, highest+attempt)
		if err != nil {
			return "", nil, diagnostics.NewFailureWithRemediation("lifecycle.state", "bounded run numbers are exhausted in "+runs.path, "",
				"remove "+runs.path+", then repeat the command")
		}
		run, err := s.createRun(ctx, runs, name)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		return name, run, err
	}
	return "", nil, state("bounded run output could not be created exclusively in " + runs.path)
}

// retireRuns removes the lowest-numbered runs no live run holds until fewer
// than maxBoundedRuns remain, and reports the highest number it listed. Every
// entry not named as a run is neither counted nor removed.
func (s *Store) retireRuns(ctx context.Context, runs *directory) (int, error) {
	names, err := directoryNames(runs, maxOperationEntries)
	if err != nil {
		return 0, state("bounded run storage cannot be listed: " + runs.path)
	}
	numbers := []int{}
	for _, name := range names {
		if number, valid := outputNumber(name, runPrefix); valid {
			numbers = append(numbers, number)
		}
	}
	slices.Sort(numbers)
	highest, remaining, removed := 0, len(numbers), false
	if len(numbers) != 0 {
		highest = numbers[len(numbers)-1]
	}
	for _, number := range numbers {
		if remaining < maxBoundedRuns {
			break
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		name, _ := outputName(runPrefix, number)
		if retireRun(runs, name) {
			removed, remaining = true, remaining-1
		}
	}
	if removed {
		if err := s.syncDirectory(ctx, runs); err != nil {
			return 0, err
		}
	}
	return highest, nil
}

// retireRun removes one run only while this call holds it exclusively and it
// is the area's own private, singly linked regular file. It reports whether
// the run is gone.
func retireRun(runs *directory, name string) bool {
	file, err := openRelative(runs, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFREG, runs.identity.Uid, runs.identity.Gid) || stat.Dev != runs.identity.Dev {
		return false
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false
	}
	return unlinkVerified(runs, name, stat, false) == nil
}

// createRun creates one run's file exclusively and holds it shared, so a
// retention running beside it never removes it. A file another retention
// removed before the hold was taken refuses, and any failure once the file
// exists removes it.
func (s *Store) createRun(ctx context.Context, runs *directory, name string) (*os.File, error) {
	if err := s.checkpoint(ctx, checkpointCreateFile); err != nil {
		return nil, err
	}
	if err := runs.verify(); err != nil {
		return nil, err
	}
	location := filepath.Join(runs.path, name)
	run, err := openRelative(runs, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if errors.Is(err, syscall.EEXIST) {
		return nil, err
	}
	if err != nil {
		return nil, state("bounded run output could not be created exclusively: " + location)
	}
	created, err := statHandle(run)
	if err != nil || !private(created, syscall.S_IFREG, runs.identity.Uid, runs.identity.Gid) || created.Dev != runs.identity.Dev {
		run.Close()
		discardCreated(runs, name, created)
		return nil, state("bounded run output is not a private file: " + location)
	}
	if syscall.Flock(int(run.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil {
		discardRun(runs, name, run)
		return nil, state("bounded run output could not be held: " + location)
	}
	if current, err := listedEntry(runs, name); err != nil || !sameIdentity(current, created) {
		run.Close()
		return nil, state("bounded run output was removed before it was held: " + location)
	}
	if err := s.syncDirectory(ctx, runs); err != nil {
		discardRun(runs, name, run)
		return nil, err
	}
	return run, nil
}

// discardRun takes no context, so an interrupted run still cleans up.
func discardRun(runs *directory, name string, run *os.File) {
	stat, err := statHandle(run)
	run.Close()
	if err == nil {
		discardCreated(runs, name, stat)
	}
}
