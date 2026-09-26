//go:build linux && amd64

package ansiblerunner

import (
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Every invocation owns a job directory and a scratch directory, named as
// os.MkdirTemp names them. The scratch name carries its job's random part, so
// scratch is tied to the job that made it without anything written inside it.
const (
	jobPrefix     = "bootwright-run-"
	scratchPrefix = "bootwright-run-scratch-"
	lockName      = "lock"
	recordName    = "record"
)

// Bounds on one sweep: the run directories one parent may hold at once, live
// or stale, and the depth and entry count of one stale tree.
const (
	maxRunDirectories = 256
	maxRunTreeDepth   = 64
	maxRunTreeEntries = 1 << 20
)

// jobRecord is what a job directory says about the adapter holding it. The run
// request carries neither its context nor its block, so these are the
// identities the runner has.
type jobRecord struct {
	Implementation string `json:"implementation"`
	Operation      string `json:"operation"`
	Machine        string `json:"machine"`
	RequestDigest  string `json:"requestDigest"`
}

// claim takes the job lock before anything else is written into the job, then
// records what the job is for. A sweep leaves a job without a record alone, so
// it never contends for the lock of a job still being created.
func claim(job string, request lifecycle.RunRequest) (*os.File, error) {
	lock, err := os.OpenFile(filepath.Join(job, lockName), os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, failure("lifecycle.state", "the adapter job lock could not be created", "")
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, failure("lifecycle.state", "the adapter job lock could not be taken", "")
	}
	record := jobRecord{
		Implementation: request.Implementation, Operation: request.Operation,
		Machine: request.Placement.Machine, RequestDigest: request.Digest,
	}
	if err := writeJSON(job, recordName, record); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}

// release ends this invocation's own hold on its job. The job and its scratch
// go only once no process holds the lock: an adapter descendant that still runs
// keeps them, with the material in them, the next run refuses, and the first
// run after the last holder ends removes them.
func (r Runner) release(job, scratch string, lock *os.File) {
	held, statErr := lock.Stat()
	_ = lock.Close()
	dirs, err := r.openRunDirectories()
	if statErr != nil || err != nil {
		return
	}
	defer dirs.close()
	var paired []string
	if scratch != "" {
		paired = []string{filepath.Base(scratch)}
	}
	_, _ = dirs.reap(filepath.Base(job), paired, held)
}

// sweep refuses while another job's lock is held, and removes every job whose
// lock is free, with its scratch, and every scratch whose job is gone.
func (r Runner) sweep() error {
	dirs, err := r.openRunDirectories()
	if err != nil {
		return failure("lifecycle.state", "private adapter invocation storage is unavailable", "")
	}
	defer dirs.close()
	jobs, err := matching(dirs.jobs, jobSuffix)
	if err != nil {
		return r.unswept()
	}
	scratch, err := matching(dirs.scratch, scratchJob)
	if err != nil {
		return r.unswept()
	}
	var refusal error
	for _, suffix := range slices.Sorted(maps.Keys(jobs)) {
		state, err := dirs.reap(jobPrefix+suffix, scratch[suffix], nil)
		if err != nil {
			return r.unswept()
		}
		if state == jobHeld && refusal == nil {
			lock := filepath.Join(r.jobParent, jobPrefix+suffix, lockName)
			refusal = failure("lifecycle.adapter-running", "an earlier lifecycle adapter still runs and holds its job lock",
				"wait for it to end, or end the processes that hold "+lock+", then repeat the command")
		}
		// Its scratch was settled with the job, whatever the job's state.
		delete(scratch, suffix)
	}
	for _, suffix := range slices.Sorted(maps.Keys(scratch)) {
		if err := dirs.orphaned(suffix, scratch[suffix]); err != nil {
			return r.unswept()
		}
	}
	return refusal
}

func (r Runner) unswept() error {
	return failure("lifecycle.state", "a stale adapter run directory could not be removed safely",
		"remove the bootwright-run directories in "+r.jobParent+" and "+r.scratchParent+" that no process holds, then repeat the command")
}

// runDirectories are the two parents run directories live in, held open so
// every entry is reached relative to them. owner is the only identity whose
// entries are the runner's, and entries bounds what one tree may hold.
type runDirectories struct {
	jobs, scratch *os.Root
	owner         uint32
	entries       int
}

func (r Runner) openRunDirectories() (runDirectories, error) {
	jobs, err := os.OpenRoot(r.jobParent)
	if err != nil {
		return runDirectories{}, err
	}
	scratch, err := os.OpenRoot(r.scratchParent)
	if err != nil {
		jobs.Close()
		return runDirectories{}, err
	}
	entries := r.treeEntries
	if entries <= 0 {
		entries = maxRunTreeEntries
	}
	return runDirectories{jobs: jobs, scratch: scratch, owner: r.owner, entries: entries}, nil
}

func (d runDirectories) close() {
	d.jobs.Close()
	d.scratch.Close()
}

type jobState int

const (
	jobSkipped jobState = iota
	jobHeld
	jobRemoved
)

// reap removes one job directory, with the scratch paired with it, when no
// process holds its lock. An entry that is not a private directory of the
// owner, or whose lock or record is not a private regular file, is not the
// runner's and is neither followed nor removed; neither is a job without its
// record, which is still being created. held, when set, is the lock file the
// caller created, and the job's lock must still be that file.
func (d runDirectories) reap(name string, scratch []string, held os.FileInfo) (jobState, error) {
	job, stat, ok := d.directory(d.jobs, name)
	if !ok {
		return jobSkipped, nil
	}
	defer job.Close()
	record, _, ok := d.file(job, recordName)
	if !ok {
		return jobSkipped, nil
	}
	record.Close()
	lock, opened, ok := d.file(job, lockName)
	if !ok || held != nil && !os.SameFile(held, opened) {
		return jobSkipped, nil
	}
	defer lock.Close()
	switch err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return jobHeld, nil
	case err != nil:
		return jobSkipped, err
	}
	// The lock stays held while the job goes, so no other sweep removes it too.
	for _, other := range scratch {
		if err := d.removeDirectory(d.scratch, other); err != nil {
			return jobSkipped, err
		}
	}
	// The record and lock go last, so a job that cannot be removed whole
	// still fails the next sweep closed rather than passing as one being
	// created.
	remaining := d.entries
	if err := emptyTree(job, stat.Dev, 0, &remaining, recordName, lockName); err != nil {
		return jobSkipped, err
	}
	if err := d.jobs.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return jobSkipped, err
	}
	return jobRemoved, nil
}

// orphaned removes scratch whose job is gone. A job is removed only once no
// process holds it, and its scratch is made after it, so scratch without its
// job serves no adapter, as after a reboot empties the job parent.
func (d runDirectories) orphaned(suffix string, names []string) error {
	if _, err := d.jobs.Lstat(jobPrefix + suffix); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	for _, name := range names {
		if err := d.removeDirectory(d.scratch, name); err != nil {
			return err
		}
	}
	return nil
}

func (d runDirectories) removeDirectory(parent *os.Root, name string) error {
	dir, stat, ok := d.directory(parent, name)
	if !ok {
		return nil
	}
	remaining := d.entries
	err := emptyTree(dir, stat.Dev, 0, &remaining)
	dir.Close()
	if err != nil {
		return err
	}
	if err := parent.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// directory opens one run directory through its held parent. The listing is
// read without following a link, and the decision is taken on the opened
// handle, which must be the entry listed: a private directory of the owner on
// the parent's filesystem.
func (d runDirectories) directory(parent *os.Root, name string) (*os.Root, *syscall.Stat_t, bool) {
	within, err := parent.Stat(".")
	listed, listErr := parent.Lstat(name)
	if err != nil || listErr != nil || !listed.IsDir() {
		return nil, nil, false
	}
	dir, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, false
	}
	opened, err := dir.Stat(".")
	stat, ok := d.private(opened, err)
	if !ok || !opened.IsDir() || !os.SameFile(listed, opened) || stat.Dev != within.Sys().(*syscall.Stat_t).Dev {
		dir.Close()
		return nil, nil, false
	}
	return dir, stat, true
}

// file opens a job's lock or record the same way: a private regular file of
// the owner with one link, never through a link.
func (d runDirectories) file(dir *os.Root, name string) (*os.File, os.FileInfo, bool) {
	listed, err := dir.Lstat(name)
	if err != nil || !listed.Mode().IsRegular() {
		return nil, nil, false
	}
	file, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, false
	}
	opened, err := file.Stat()
	stat, ok := d.private(opened, err)
	if !ok || !opened.Mode().IsRegular() || stat.Nlink != 1 || !os.SameFile(listed, opened) {
		file.Close()
		return nil, nil, false
	}
	return file, opened, true
}

func (d runDirectories) private(info os.FileInfo, err error) (*syscall.Stat_t, bool) {
	if err != nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok && stat.Uid == d.owner && stat.Mode&0077 == 0
}

// emptyTree removes everything beneath one verified run directory. A link is
// removed as itself and never followed, a directory is entered only through a
// handle that is the entry listed, and an entry on another filesystem, such as
// a mount left behind, refuses rather than being emptied. The last names go
// after every other entry, in their order.
func emptyTree(dir *os.Root, device uint64, depth int, remaining *int, last ...string) error {
	if depth > maxRunTreeDepth {
		return errors.New("run directory depth bound")
	}
	names, err := entries(dir, remaining)
	if err != nil {
		return err
	}
	slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })
	for _, name := range names {
		listed, err := dir.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat, ok := listed.Sys().(*syscall.Stat_t); !ok || stat.Dev != device {
			return errors.New("run directory crosses a filesystem")
		}
		if listed.IsDir() {
			if err := emptyChild(dir, name, listed, device, depth, remaining); err != nil {
				return err
			}
		}
		if err := dir.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func emptyChild(parent *os.Root, name string, listed os.FileInfo, device uint64, depth int, remaining *int) error {
	child, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(listed, opened) {
		return errors.New("run directory entry changed")
	}
	return emptyTree(child, device, depth+1, remaining)
}

func entries(dir *os.Root, remaining *int) ([]string, error) {
	listing, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	var names []string
	for {
		part, err := listing.Readdirnames(1024)
		if *remaining -= len(part); *remaining < 0 {
			return nil, errors.New("run directory entry bound")
		}
		names = append(names, part...)
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// matching lists the entries of one parent that carry the runner's exact
// names, keyed by the job they belong to. Other entries are only read past, so
// a busy /var/tmp costs nothing but the reading.
func matching(parent *os.Root, match func(string) (string, bool)) (map[string][]string, error) {
	listing, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	found, count := map[string][]string{}, 0
	for {
		names, err := listing.Readdirnames(1024)
		for _, name := range names {
			if suffix, ok := match(name); ok {
				if count++; count > maxRunDirectories {
					return nil, errors.New("run directory bound")
				}
				found[suffix] = append(found[suffix], name)
			}
		}
		if errors.Is(err, io.EOF) {
			return found, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// runSuffix reports whether a name part is what os.MkdirTemp puts there: the
// decimal form of a 32-bit value and nothing else.
func runSuffix(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return err == nil && strconv.FormatUint(parsed, 10) == value
}

func jobSuffix(name string) (string, bool) {
	suffix, ok := strings.CutPrefix(name, jobPrefix)
	return suffix, ok && runSuffix(suffix)
}

func scratchJob(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, scratchPrefix)
	job, random, paired := strings.Cut(rest, "-")
	return job, ok && paired && runSuffix(job) && runSuffix(random)
}
