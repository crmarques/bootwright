//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"encoding/json"
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
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Every invocation owns a job directory and a scratch directory, named as
// os.MkdirTemp names them. The scratch name carries its job's random part, so
// scratch is tied to the job that made it without anything written inside it.
const (
	jobPrefix     = "bootwright-run-"
	scratchPrefix = "bootwright-run-scratch-"
	lockName      = "lock"
	holderName    = "holder"
	recordName    = "record"
)

// Bounds on one sweep: the run directories one parent may hold at once, live
// or stale, the depth and entry count of one stale tree, and the bytes of one
// held job's record it reads.
const (
	maxRunDirectories = 256
	maxRunTreeDepth   = 64
	maxRunTreeEntries = 1 << 20
	maxRecordBytes    = 4096
)

// jobRecord is what a job directory says about the adapter holding it: the
// context it runs for, which bounds whom a held job refuses, and what it is
// doing, which the refusal names.
type jobRecord struct {
	Context        string `json:"context"`
	Block          string `json:"block"`
	Description    string `json:"description"`
	Implementation string `json:"implementation"`
	Operation      string `json:"operation"`
	Machine        string `json:"machine"`
	RequestDigest  string `json:"requestDigest"`
}

func recordFor(request lifecycle.RunRequest) jobRecord {
	return jobRecord{
		Context: request.Context, Block: request.Block, Description: request.Description,
		Implementation: request.Implementation, Operation: request.Operation,
		Machine: request.Placement.Machine, RequestDigest: request.Digest,
	}
}

// checkIdentity refuses, before anything is written, a run its job record
// could not attribute: one naming no context, or one a sweep could not read
// back.
func checkIdentity(request lifecycle.RunRequest) error {
	if !api.ValidLexical("name", request.Context) {
		return failure("lifecycle.state", "the adapter run names no context", "")
	}
	record := recordFor(request)
	if encoded, err := json.Marshal(record); err != nil || len(encoded) > maxRecordBytes || !recordable(record) {
		return failure("lifecycle.state", "the adapter run's identity cannot be recorded in its job", "")
	}
	return nil
}

// recordable reports whether a record attributes its job: a valid context and
// printable text a refusal can name.
func recordable(record jobRecord) bool {
	if !api.ValidLexical("name", record.Context) {
		return false
	}
	for _, field := range []string{record.Block, record.Description, record.Implementation, record.Operation, record.Machine, record.RequestDigest} {
		if !utf8.ValidString(field) || strings.ContainsFunc(field, unicode.IsControl) {
			return false
		}
	}
	return true
}

// claim takes the job lock before anything else is written into the job, then
// the holder lock, which only this invocation holds: it is opened close-on-exec
// and never handed to the adapter, so it is free once the invocation ends
// whatever its descendants still hold. It then records what the job is for,
// under another name renamed into place, so a record a sweep finds is whole
// and its holder already exists. A sweep leaves a job without a record alone,
// so it never contends for the lock of a job still being created.
func claim(job string, request lifecycle.RunRequest) (*os.File, *os.File, error) {
	lock, err := os.OpenFile(filepath.Join(job, lockName), os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, nil, failure("lifecycle.state", "the adapter job lock could not be created", "")
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, nil, failure("lifecycle.state", "the adapter job lock could not be taken", "")
	}
	holder, err := os.OpenFile(filepath.Join(job, holderName), os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		lock.Close()
		return nil, nil, failure("lifecycle.state", "the adapter job holder could not be created", "")
	}
	if syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		holder.Close()
		lock.Close()
		return nil, nil, failure("lifecycle.state", "the adapter job holder could not be taken", "")
	}
	if err := publishRecord(job, recordFor(request)); err != nil {
		holder.Close()
		lock.Close()
		return nil, nil, err
	}
	return lock, holder, nil
}

// partialRecordName is the name a job's record is written under before it is
// renamed into place.
const partialRecordName = recordName + ".partial"

func publishRecord(job string, record jobRecord) error {
	if err := writeJSON(job, partialRecordName, record); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(job, partialRecordName), filepath.Join(job, recordName)); err != nil {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	return nil
}

// release ends this invocation's own hold on its job. The job and its scratch
// go only once no process holds the lock: an adapter descendant that still runs
// keeps them, with the material in them, the next run of this context refuses,
// and the first run after the last holder ends removes them. The holder goes
// after the lock, so no sweep advises ending the processes that hold a job
// while this invocation is still one of them.
func (r Runner) release(job, scratch string, lock, holder *os.File) {
	held, statErr := lock.Stat()
	_ = lock.Close()
	_ = holder.Close()
	dirs, err := r.openRunDirectories()
	if statErr != nil || err != nil {
		return
	}
	defer dirs.close()
	var paired []string
	if scratch != "" {
		paired = []string{filepath.Base(scratch)}
	}
	_, _, _ = dirs.reap(filepath.Base(job), paired, held)
}

// sweep refuses while a job of contextName, or a job attributed to no context,
// still holds its lock, and removes every job on the host whose lock is free,
// with its scratch, and every scratch whose job is gone. A held job of another
// context neither refuses nor goes.
func (r Runner) sweep(contextName string) error {
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
		state, held, err := dirs.reap(jobPrefix+suffix, scratch[suffix], nil)
		if err != nil {
			return r.unswept()
		}
		if state == jobHeld && refusal == nil {
			refusal = r.heldRefusal(contextName, filepath.Join(r.jobParent, jobPrefix+suffix, lockName), held)
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

// heldRefusal is what one held job says to a run of contextName. A job of
// another context says nothing. A job of this context names what it is doing,
// and advises ending processes only once the invocation that started it no
// longer holds its holder lock, when what remains are descendants it left. A
// job attributed to no context, as one an earlier build started, refuses every
// context.
func (r Runner) heldRefusal(contextName, lock string, held holding) error {
	record := held.record
	switch {
	case !held.attributed:
		return failure("lifecycle.adapter-running", "an earlier lifecycle adapter still runs and holds its job lock",
			"wait for it to end, or end the processes that hold "+lock+", then repeat the command")
	case record.Context != contextName:
		return nil
	case held.alive:
		return failure("lifecycle.adapter-running", "context "+record.Context+" is still running "+record.doing()+
			" (adapter operation "+record.Operation+" on Machine "+record.Machine+")",
			"wait for it to end, then repeat the command")
	default:
		return failure("lifecycle.adapter-running", "context "+record.Context+"'s "+record.doing()+
			" ended, but processes it started still hold its adapter job lock",
			"end the processes that hold "+lock+", then repeat the command")
	}
}

// doing names what a job is for: its block's or step's description, or its
// implementation for a request that states none.
func (record jobRecord) doing() string {
	if record.Description != "" {
		return record.Description
	}
	return record.Implementation
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

// holding is what a held job's record and holder lock say about it. A job is
// attributed only when its record is readable, names a valid context and the
// job has its holder; alive is set while the invocation that started it still
// holds that holder.
type holding struct {
	record     jobRecord
	attributed bool
	alive      bool
}

// reap removes one job directory, with the scratch paired with it, once no
// process of the job holds its lock, and otherwise reports what holds it. A
// job another sweep is removing is skipped.
func (d runDirectories) reap(name string, scratch []string, held os.FileInfo) (jobState, holding, error) {
	seized, state, found, err := d.seize(name, held)
	if seized == nil {
		return state, found, err
	}
	defer seized.close()
	if err := seized.remove(scratch); err != nil {
		return jobSkipped, holding{}, err
	}
	return jobRemoved, holding{}, nil
}

// seizedJob is a job one sweep holds for removal. Only the job's own
// processes ever hold its lock exclusively: the invocation takes it before
// the job records anything, and the adapter and every process it starts
// inherit it. The sweep holds that lock shared, which it can only once none
// of them runs and which keeps it so, and it holds the job directory's own
// lock exclusively, which keeps every other sweep from removing the job too.
// A sweep that finds a job's lock held exclusively has found the job's own
// processes, never another sweep removing it.
type seizedJob struct {
	dirs    runDirectories
	name    string
	job     *os.Root
	device  uint64
	handles []*os.File
}

func (s *seizedJob) close() {
	for _, handle := range s.handles {
		handle.Close()
	}
	s.job.Close()
}

// open opens one of the job's files as runDirectories.file does, keeping its
// handle until the job is closed.
func (s *seizedJob) open(name string) (*os.File, os.FileInfo, bool) {
	file, opened, ok := s.dirs.file(s.job, name)
	if ok {
		s.handles = append(s.handles, file)
	}
	return file, opened, ok
}

// seize takes one job for removal, or reports why it cannot: the job is not
// the runner's, is still being created, is held by its own processes, or is
// being removed by another sweep. An entry that is not a private directory of
// the owner, or whose lock or record is not a private regular file, is not the
// runner's and is neither followed nor removed; neither is a job without its
// record, which is still being created or already going. held, when set, is
// the lock file the caller created, and the job's lock must still be that
// file. A held job let go while its record and holder were read is taken like
// any free job.
func (d runDirectories) seize(name string, held os.FileInfo) (seized *seizedJob, state jobState, found holding, err error) {
	job, stat, ok := d.directory(d.jobs, name)
	if !ok {
		return nil, jobSkipped, holding{}, nil
	}
	candidate := &seizedJob{dirs: d, name: name, job: job, device: stat.Dev}
	defer func() {
		if seized == nil {
			candidate.close()
		}
	}()
	record, recorded, ok := candidate.open(recordName)
	if !ok {
		return nil, jobSkipped, holding{}, nil
	}
	lock, opened, ok := candidate.open(lockName)
	if !ok || held != nil && !os.SameFile(held, opened) {
		return nil, jobSkipped, holding{}, nil
	}
	free, err := lockShared(lock)
	if err != nil {
		return nil, jobSkipped, holding{}, err
	}
	if !free {
		var stillHeld bool
		if found, stillHeld, err = d.holding(job, record, lock); err != nil {
			return nil, jobSkipped, holding{}, err
		}
		if stillHeld {
			return nil, jobHeld, found, nil
		}
	}
	directory, locked, err := lockDirectory(job)
	if !locked {
		return nil, jobSkipped, holding{}, err
	}
	candidate.handles = append(candidate.handles, directory)
	// A sweep that held the directory before this one may have removed the
	// job meanwhile; its record goes first of the three.
	if listed, err := job.Lstat(recordName); err != nil || !os.SameFile(listed, recorded) {
		return nil, jobSkipped, holding{}, nil
	}
	return candidate, jobSkipped, holding{}, nil
}

// lockDirectory takes a run directory's own lock exclusively, which only a
// sweep removing it holds, so no other sweep removes it too. It holds nothing
// when another sweep holds that lock or the directory is already gone.
func lockDirectory(dir *os.Root) (*os.File, bool, error) {
	handle, err := dir.Open(".")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	switch err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		handle.Close()
		return nil, false, nil
	case err != nil:
		handle.Close()
		return nil, false, err
	}
	return handle, true, nil
}

// lockShared takes a job lock shared, which succeeds once no process of the
// job holds it.
func lockShared(lock *os.File) (bool, error) {
	switch err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// remove removes a seized job with the scratch paired with it. The record,
// holder and lock go last, so a job that cannot be emptied still fails the
// next sweep closed rather than passing as one being created. The record goes
// before the holder, so a held job with its record in place and no holder is
// one an earlier build started.
func (s *seizedJob) remove(scratch []string) error {
	for _, other := range scratch {
		if err := s.dirs.removeDirectory(s.dirs.scratch, other); err != nil {
			return err
		}
	}
	remaining := s.dirs.entries
	if err := emptyTree(s.job, s.device, 0, &remaining, recordName, holderName, lockName); err != nil {
		return err
	}
	if err := s.dirs.jobs.Remove(s.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// holding is what a job found held says about itself, and whether its lock is
// still held once its record and holder are read. Only then does what they say
// stand: a job whose processes let it go meanwhile may already have lost its
// holder to a sweep removing it, or freed its holder only because its
// invocation ended.
func (d runDirectories) holding(job *os.Root, record, lock *os.File) (holding, bool, error) {
	found := d.attribute(job, record)
	free, err := lockShared(lock)
	if err != nil || free {
		return holding{}, false, err
	}
	return found, true, nil
}

// attribute reads a held job's record, bounded and strictly, and probes its
// holder through the same checks as its lock. Taking a free holder only probes
// it: closing the handle gives it back.
func (d runDirectories) attribute(job *os.Root, file *os.File) holding {
	data, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil || len(data) > maxRecordBytes {
		return holding{}
	}
	var record jobRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || !recordable(record) {
		return holding{}
	}
	holder, _, ok := d.file(job, holderName)
	if !ok {
		return holding{}
	}
	defer holder.Close()
	alive := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil
	return holding{record: record, attributed: true, alive: alive}
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

// removeDirectory removes one scratch directory while it holds the
// directory's own lock, so two sweeps never empty one scratch together: one
// that cannot take the lock leaves the scratch to the sweep removing it, and
// one that takes it after the scratch went leaves it.
func (d runDirectories) removeDirectory(parent *os.Root, name string) error {
	dir, stat, ok := d.directory(parent, name)
	if !ok {
		return nil
	}
	defer dir.Close()
	lock, locked, err := lockDirectory(dir)
	if !locked {
		return err
	}
	defer lock.Close()
	listed, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if now, ok := listed.Sys().(*syscall.Stat_t); !ok || now.Dev != stat.Dev || now.Ino != stat.Ino {
		return nil
	}
	remaining := d.entries
	if err := emptyTree(dir, stat.Dev, 0, &remaining); err != nil {
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

// file opens a job's lock, holder or record the same way: a private regular
// file of the owner with one link, never through a link.
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
