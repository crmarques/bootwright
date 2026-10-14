//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const (
	maxSetupRuns = 8
	// maxSetupRunOutput is what a setup run's writer keeps: the bounded run's
	// own bound, operationstore.MaxAdapterOutputBytes, so every run's output
	// shares one (D92).
	maxSetupRunOutput  = 4 << 20
	setupRunsName      = "runs"
	setupRunOutputName = "run.output"
	setupRunPrefix     = "setup-"
	setupRunDigits     = 6
)

func setupRunNumber(name string) (int, bool) {
	digits, found := strings.CutPrefix(name, setupRunPrefix)
	if !found || len(digits) != setupRunDigits || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	number, err := strconv.Atoi(digits)
	return number, err == nil && number > 0
}

func setupRunName(number int) string {
	digits := strconv.Itoa(number)
	return setupRunPrefix + strings.Repeat("0", max(setupRunDigits-len(digits), 0)) + digits
}

func unsafeSetupRun(path, reason string) error {
	return state("controller setup run entry is not this store's own: " + path + ": " + reason)
}

// verifySetupRuns admits a runs container only as this store creates it: at
// most maxSetupRuns run directories, each empty or holding only its own
// bounded private output file. A killed run leaves one of those shapes, so
// nothing else is ever admitted.
func verifySetupRuns(ctx context.Context, runs *directory) ([]int, error) {
	names, err := directoryNames(runs, maxSetupRuns)
	if err != nil {
		return nil, unsafeSetupRun(heldEntry(runs), "it holds more than "+strconv.Itoa(maxSetupRuns)+" runs or cannot be listed")
	}
	numbers := make([]int, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		number, valid := setupRunNumber(name)
		if !valid {
			return nil, unsafeSetupRun(storeEntry(runs, name), "its name is not one a setup run takes")
		}
		run, err := openDirectory(runs, name)
		if err != nil {
			return nil, unsafeSetupRun(storeEntry(runs, name), "it is not a private directory")
		}
		_, _, err = setupRunOutput(run)
		run.file.Close()
		if err != nil {
			return nil, err
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	return numbers, nil
}

func setupRunOutput(run *directory) (syscall.Stat_t, bool, error) {
	names, err := directoryNames(run, 1)
	if err != nil {
		return syscall.Stat_t{}, false, unsafeSetupRun(heldEntry(run), "it holds more than its own output")
	}
	if len(names) == 0 {
		return syscall.Stat_t{}, false, nil
	}
	path := storeEntry(run, names[0])
	if names[0] != setupRunOutputName {
		return syscall.Stat_t{}, false, unsafeSetupRun(path, "it is not the run's own output")
	}
	file, err := openRelative(run, setupRunOutputName, pathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, false, unsafeSetupRun(path, "it cannot be opened without following a link")
	}
	stat, err := statHandle(file)
	file.Close()
	if err != nil || !private(stat, syscall.S_IFREG, run.identity.Uid, run.identity.Gid) || stat.Dev != run.identity.Dev || stat.Size > maxSetupRunOutput {
		return syscall.Stat_t{}, false, unsafeSetupRun(path, "it is not a private regular file within its bound")
	}
	return stat, true, nil
}

func (s *Store) nextSetupRun(ctx context.Context, runs *directory) (string, prerequisites.SetupRun, error) {
	numbers, err := verifySetupRuns(ctx, runs)
	if err != nil {
		return "", nil, err
	}
	next := 1
	if len(numbers) != 0 {
		next = numbers[len(numbers)-1] + 1
	}
	if len(setupRunName(next)) != len(setupRunPrefix)+setupRunDigits {
		return "", nil, state("setup run numbers are exhausted")
	}
	for len(numbers) >= maxSetupRuns {
		if err := s.removeSetupRun(ctx, runs, setupRunName(numbers[0])); err != nil {
			return "", nil, err
		}
		numbers = numbers[1:]
	}
	run, err := s.createSetupRun(ctx, runs, setupRunName(next))
	return setupRunName(next), run, err
}

// removeSetupRun unlinks each entry only while it is still the exact one this
// call verified.
func (s *Store) removeSetupRun(ctx context.Context, runs *directory, name string) error {
	run, err := openDirectory(runs, name)
	if err != nil {
		return unsafeSetupRun(storeEntry(runs, name), "it is not a private directory")
	}
	identity := run.identity
	output, present, err := setupRunOutput(run)
	if err == nil && present {
		err = unlinkVerified(run, setupRunOutputName, output, false)
	}
	run.file.Close()
	if err != nil {
		return err
	}
	if err := unlinkVerified(runs, name, identity, true); err != nil {
		return err
	}
	return s.syncDirectory(ctx, runs)
}

// createSetupRun makes both names durable before the run is handed out, so the
// directory an operator is told about exists whatever the run does next.
func (s *Store) createSetupRun(ctx context.Context, runs *directory, name string) (prerequisites.SetupRun, error) {
	run, err := s.newDirectory(ctx, runs, name)
	if err != nil {
		return nil, unsafeSetupRun(storeEntry(runs, name), "it could not be created exclusively")
	}
	defer run.file.Close()
	file, err := openRelative(run, setupRunOutputName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return nil, unsafeSetupRun(storeEntry(run, setupRunOutputName), "it could not be created exclusively")
	}
	created, err := statHandle(file)
	if err != nil || !private(created, syscall.S_IFREG, run.identity.Uid, run.identity.Gid) || created.Mode&0777 != 0600 || s.syncDirectory(ctx, run) != nil {
		file.Close()
		discardCreated(run, setupRunOutputName, created)
		return nil, unsafeSetupRun(storeEntry(run, setupRunOutputName), "it could not be made durable as a private file")
	}
	return &setupRun{file: file, location: run.path}, nil
}

// setupRun writes straight to its file, so what the Ansible prints is readable
// while it runs. A write never reports an error, because os/exec stops copying
// from a writer that fails and the Ansible can then block on a full pipe.
type setupRun struct {
	mutex    sync.Mutex
	file     *os.File
	location string
	written  int64
	stopped  bool
}

func (r *setupRun) Write(value []byte) (int, error) {
	size := len(value)
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.stopped {
		return size, nil
	}
	value = value[:min(int64(len(value)), maxSetupRunOutput-r.written)]
	written, err := r.file.Write(value)
	r.written += int64(written)
	r.stopped = err != nil || r.written >= maxSetupRunOutput
	return size, nil
}

func (r *setupRun) Location() string { return r.location }

func (r *setupRun) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.file == nil {
		return nil
	}
	r.stopped = true
	synced := r.file.Sync()
	closed := r.file.Close()
	r.file = nil
	if synced != nil {
		return state("setup run output durability could not be established")
	}
	if closed != nil {
		return state("setup run output could not be closed")
	}
	return nil
}
