//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	maxSetupRunOutput = 4 << 20
	installRunPrefix  = "install-"
	setupRunDigits    = 6
)

func setupRunNumber(name string) (int, bool) {
	digits, found := strings.CutPrefix(name, installRunPrefix)
	if !found || len(digits) != setupRunDigits || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	number, err := strconv.Atoi(digits)
	return number, err == nil && number > 0
}

func setupRunName(number int) string {
	digits := strconv.Itoa(number)
	return installRunPrefix + strings.Repeat("0", max(setupRunDigits-len(digits), 0)) + digits
}

func unsafeSetupRun(path, reason string) error {
	return errors.Join(prerequisites.ErrUnsafeSetupRun, state("controller setup run entry is not this store's own: "+path+": "+reason))
}

// setupLogError makes an unsafe log root entry a setup refusal, since setup
// keeps its output only where this store created the directories.
func setupLogError(err error) error {
	if errors.Is(err, errUnsafeLogEntry) {
		return errors.Join(prerequisites.ErrUnsafeSetupRun, err)
	}
	return err
}

// setupRunNumbers lists, in number order, the setup output files the host log
// directory holds. A name a setup output takes that is no private regular file
// within its bound refuses; every other name is ignored.
func setupRunNumbers(ctx context.Context, host *directory) ([]int, error) {
	names, err := directoryNames(host, maxOperationEntries)
	if err != nil {
		return nil, setupRunAccessError(host.path, err)
	}
	numbers := []int{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		number, valid := outputNumber(name, installRunPrefix)
		if !valid {
			continue
		}
		location := filepath.Join(host.path, name)
		stat, err := listedEntry(host, name)
		if err != nil {
			return nil, setupRunAccessError(location, err)
		}
		if !private(stat, syscall.S_IFREG, host.identity.Uid, host.identity.Gid) || stat.Dev != host.identity.Dev || stat.Size > maxSetupRunOutput {
			return nil, unsafeSetupRun(location, "it is not a private regular file within its bound")
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	return numbers, nil
}

// recordedSetupNumber is the highest install-NNNNNN the setup record names, in
// its pending intent or its receipt, so output retention never frees a number
// a receipt still attributes.
func recordedSetupNumber(state prerequisites.SetupState) int {
	highest := 0
	if state.Pending != nil {
		highest, _ = setupRunNumber(state.Pending.ID)
	}
	if state.Receipt != nil {
		for _, item := range state.Receipt.Packages {
			if number, found := setupRunNumber(item.InstalledBy); found {
				highest = max(highest, number)
			}
		}
	}
	return highest
}

func (s *Store) nextSetupRun(ctx context.Context, host *directory, recorded int) (string, prerequisites.SetupRun, error) {
	numbers, err := setupRunNumbers(ctx, host)
	if err != nil {
		return "", nil, err
	}
	next := recorded + 1
	if len(numbers) != 0 {
		next = max(next, numbers[len(numbers)-1]+1)
	}
	if len(setupRunName(next)) != len(installRunPrefix)+setupRunDigits {
		return "", nil, state("setup run numbers are exhausted")
	}
	for len(numbers) >= maxSetupRuns {
		if err := s.removeSetupRun(ctx, host, numbers[0]); err != nil {
			return "", nil, err
		}
		numbers = numbers[1:]
	}
	run, err := s.createSetupRun(ctx, host, setupRunName(next)+outputSuffix)
	return setupRunName(next), run, err
}

// removeSetupRun unlinks one output only while it is still the exact file this
// call verified.
func (s *Store) removeSetupRun(ctx context.Context, host *directory, number int) error {
	name := setupRunName(number) + outputSuffix
	location := filepath.Join(host.path, name)
	stat, err := listedEntry(host, name)
	if err == nil {
		err = unlinkVerified(host, name, stat, false)
	}
	if err != nil {
		return setupRunAccessError(location, err)
	}
	if err := s.syncDirectory(ctx, host); err != nil {
		return setupRunAccessError(host.path, err)
	}
	return nil
}

func setupRunAccessError(path string, err error) error {
	var errno syscall.Errno
	var ioErr *ioFailure
	var listing *listingFailure
	if errors.As(err, &listing) && listing.kind == listingUnreadable {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ioErr) {
		return err
	}
	if errors.As(err, &errno) && errno != syscall.EEXIST && errno != syscall.ELOOP && errno != syscall.ENOTDIR && errno != syscall.EXDEV {
		return err
	}
	return unsafeSetupRun(path, "private entry identity could not be proved")
}

// createSetupRun makes the file durable before the run is handed out, so the
// file an operator is told about exists whatever the run does next.
func (s *Store) createSetupRun(ctx context.Context, host *directory, name string) (prerequisites.SetupRun, error) {
	if err := s.checkpoint(ctx, checkpointCreateFile); err != nil {
		return nil, err
	}
	location := filepath.Join(host.path, name)
	file, err := openRelative(host, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return nil, setupRunAccessError(location, err)
	}
	created, err := statHandle(file)
	if err != nil {
		file.Close()
		return nil, setupRunAccessError(location, err)
	}
	if !private(created, syscall.S_IFREG, host.identity.Uid, host.identity.Gid) || created.Mode&0777 != 0600 {
		file.Close()
		discardCreated(host, name, created)
		return nil, unsafeSetupRun(location, "it could not be made durable as a private file")
	}
	if err := s.syncDirectory(ctx, host); err != nil {
		file.Close()
		discardCreated(host, name, created)
		return nil, setupRunAccessError(location, err)
	}
	return &setupRun{file: file, location: location}, nil
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
