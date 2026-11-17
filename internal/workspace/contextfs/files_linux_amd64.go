//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativefs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	storeCapacityRemediation = "free space, or raise the quota, on the filesystem that holds /var/lib/bootwright, then repeat the command"
	storeDeviceRemediation   = "inspect the filesystem that holds /var/lib/bootwright (its kernel log names the device error), then repeat the command"
	rootLockPoll             = 10 * time.Millisecond
	maxRootLockPoll          = 250 * time.Millisecond
	rootLockWait             = time.Minute
)

// storeErrno finds the kernel's answer inside a store failure. Only the errno
// is ever rendered: the text of an *os.PathError carries a private path.
func storeErrno(err error) (syscall.Errno, bool) {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno, true
	}
	return 0, false
}

// capacityErrno reports the answers that mean the filesystem could not hold a
// write: no space, no quota left, or a file past its size limit.
func capacityErrno(errno syscall.Errno) bool {
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT || errno == syscall.EFBIG
}

// errnoText is the kernel's own name for an answer, with the symbol an
// operator searches for when it is a capacity answer.
func errnoText(errno syscall.Errno) string {
	switch errno {
	case syscall.ENOSPC:
		return errno.Error() + " (ENOSPC)"
	case syscall.EDQUOT:
		return errno.Error() + " (EDQUOT)"
	case syscall.EFBIG:
		return errno.Error() + " (EFBIG)"
	}
	return errno.Error()
}

// storeCauseText renders why the store refused an entry: the errno it
// answered, or the message of a refusal the store raised itself, and never the
// text of any other error.
func storeCauseText(cause error) string {
	if errno, found := storeErrno(cause); found {
		return errnoText(errno)
	}
	messages := []string{}
	for _, diagnostic := range diagnostics.Of(cause) {
		messages = append(messages, diagnostic.Message)
	}
	return strings.Join(messages, "; ")
}

// ioDiagnosis names a write the kernel refused: a capacity answer as the room
// the state root's filesystem lacks, any other answer as a device error.
func ioDiagnosis(message string, errno syscall.Errno) (string, string) {
	if capacityErrno(errno) {
		return message + ": the filesystem holding the state root has no room: " + errnoText(errno), storeCapacityRemediation
	}
	return message + ": " + errnoText(errno), storeDeviceRemediation
}

// storeFailure names the errno a failed write or sync returned. A capacity
// answer is never read as damage: it names the room the filesystem lacks and
// the free-space remedy. A failure without an errno keeps its message alone.
type directory = kernel.Directory

func storeFailure(message string, cause error) error {
	errno, found := storeErrno(cause)
	if !found {
		return state(message)
	}
	text, remediation := ioDiagnosis(message, errno)
	return &kernel.IOFailure{Failure: contexts.StateErrorWithRemediation(text, remediation)}
}

// refusedRoot is unsafeRoot with an ancestor link's exit for a root that is a link.
func refusedRoot(path string, stat syscall.Stat_t, uid, gid uint32) error {
	err := unsafeRoot(stat, uid, gid)
	var failure *diagnostics.Failure
	if stat.Mode&syscall.S_IFMT != syscall.S_IFLNK || !errors.As(err, &failure) || len(failure.Diagnostics) != 1 {
		return err
	}
	return contexts.StateErrorWithRemediation(failure.Diagnostics[0].Message,
		"replace "+path+" with a directory, such as the directory the link names or a mount of that storage, then repeat the command")
}

func (s *Store) rootPath() (string, error) {
	path := s.options.Root
	if path == "" {
		path = "/var/lib/bootwright"
	}
	if !filepath.IsAbs(path) || len(path) > maxPath || strings.ContainsRune(path, 0) {
		return "", state("state root must be an absolute bounded path")
	}
	path = filepath.Clean(path)
	if !canonicalPath(path) {
		return "", state("state root is not canonical")
	}
	return path, nil
}

func (s *Store) openRoot(ctx context.Context, create bool, inputs []string) (*directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.rootPath()
	if err != nil {
		return nil, err
	}
	uid, gid := uint32(0), uint32(0)
	if s.options.Owner != nil {
		if s.options.Root == "" {
			return nil, state("test ownership requires an isolated explicit root")
		}
		uid, gid = s.options.Owner.UID, s.options.Owner.GID
	}
	if uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		return nil, state("context storage requires root privileges")
	}
	for _, input := range inputs {
		if !canonicalPath(input) || beneath(input, path) {
			return nil, state("state root overlaps admitted input")
		}
	}
	var admit func(*os.File) error
	if create {
		admit = s.admitCreation(path)
	}
	file, err := kernel.WalkAbsolute(path, admit)
	if err != nil {
		// A root that is itself a link or not a directory is a root of
		// another type, which setup reports the same way.
		var unreachable *kernel.UnreachableRoot
		if errors.As(err, &unreachable) && unreachable.Component == path {
			return nil, refusedRoot(path, unreachable.Stat, uid, gid)
		}
		return nil, err
	}
	stat, err := kernel.StatHandle(file)
	if err != nil {
		file.Close()
		return nil, state("state root type, owner or permissions is unsafe")
	}
	if !kernel.Private(stat, syscall.S_IFDIR, uid, gid) {
		file.Close()
		return nil, refusedRoot(path, stat, uid, gid)
	}
	var fs syscall.Statfs_t
	if err = syscall.Fstatfs(int(file.Fd()), &fs); err != nil || !kernel.LocalFilesystem(fs.Type) {
		file.Close()
		return nil, state("state filesystem is not qualified for private durable storage")
	}
	if _, err := s.persistentRoot(path, fs.Type); err != nil {
		file.Close()
		return nil, err
	}
	root := &directory{File: file, Path: path, Identity: stat}
	// Probe the required kernel primitive without creating anything.
	probe, err := kernel.OpenRelative(root, "registry.json", kernel.PathHandle, 0)
	if probe != nil {
		probe.Close()
	}
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		file.Close()
		return nil, state("state containment primitives are unavailable or the registry path is unsafe")
	}
	return root, nil
}

// transientFilesystems are the qualified filesystems that may not keep a
// production root's contents across a reboot: tmpfs, which a reboot empties,
// and overlayfs, whose upper layer the kernel does not name. Only an isolated
// test root injected at composition lies on one.
var transientFilesystems = map[int64]string{0x01021994: "tmpfs", 0x794c7630: "overlayfs"}

// persistentRoot refuses the production root on a transient filesystem, before
// anything is published beneath it, and names that filesystem.
func (s *Store) persistentRoot(path string, kind int64) (string, error) {
	production := s.options.Root == ""
	if s.rootFilesystem != nil {
		kind, production = s.rootFilesystem(), true
	}
	name, transient := transientFilesystems[kind]
	if !production || !transient {
		return "", nil
	}
	return name, contexts.StateErrorWithRemediation("the state root "+path+" is on "+name+", which may not keep the ownership evidence it holds across a reboot",
		"place "+path+" on a persistent local filesystem, such as ext4, XFS or Btrfs, then repeat the command")
}

// admitCreation refuses to create a missing component of path beneath a
// directory whose filesystem persistentRoot refuses, so no empty root remains.
func (s *Store) admitCreation(path string) func(*os.File) error {
	return func(parent *os.File) error {
		var fs syscall.Statfs_t
		if err := syscall.Fstatfs(int(parent.Fd()), &fs); err != nil {
			return state("state filesystem is not qualified for private durable storage")
		}
		_, err := s.persistentRoot(path, fs.Type)
		return err
	}
}

func readBounded(ctx context.Context, parent *directory, name string, maximum int, immutable bool) ([]byte, error) {
	data, _, err := readBoundedIdentity(ctx, parent, name, maximum, immutable)
	return data, err
}

func readBoundedIdentity(ctx context.Context, parent *directory, name string, maximum int, immutable bool) ([]byte, syscall.Stat_t, error) {
	if err := ctx.Err(); err != nil {
		return nil, syscall.Stat_t{}, err
	}
	file, err := kernel.OpenRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, syscall.Stat_t{}, err
	}
	defer file.Close()
	before, err := kernel.StatHandle(file)
	if err != nil || !kernel.ReadableFile(before, immutable, parent) || before.Size < 0 || before.Size > int64(maximum) {
		return nil, syscall.Stat_t{}, state("state file type, owner, permissions, links or size is unsafe")
	}
	data := make([]byte, 0, int(before.Size))
	buffer := make([]byte, min(32768, maximum+1))
	for len(data) <= maximum {
		if err := ctx.Err(); err != nil {
			return nil, syscall.Stat_t{}, err
		}
		n, readErr := file.Read(buffer[:min(len(buffer), maximum+1-len(data))])
		data = append(data, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, syscall.Stat_t{}, state("state file could not be read")
		}
		if len(data) > maximum {
			return nil, syscall.Stat_t{}, state("state file exceeds its byte limit")
		}
	}
	after, err := kernel.StatHandle(file)
	stable := kernel.SameFile(before, after)
	if !immutable {
		// A registry replacement may unlink this complete, already-open old
		// snapshot. Its contents remain immutable even though ctime/nlink change.
		stable = kernel.SameIdentity(before, after) && before.Size == after.Size && before.Mtim == after.Mtim && kernel.ReadableFile(after, false, parent)
	}
	if err != nil || !stable || int64(len(data)) != after.Size {
		return nil, syscall.Stat_t{}, state("state file changed during reading")
	}
	if immutable {
		current, err := kernel.OpenRelative(parent, name, kernel.PathHandle, 0)
		if err != nil {
			return nil, syscall.Stat_t{}, state("immutable state file was replaced")
		}
		stat, statErr := kernel.StatHandle(current)
		current.Close()
		if statErr != nil || !kernel.SameFile(after, stat) {
			return nil, syscall.Stat_t{}, state("immutable state file was replaced")
		}
	}
	if err := parent.Verify(); err != nil {
		return nil, syscall.Stat_t{}, err
	}
	return data, after, nil
}

func (s *Store) newDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	if err := s.checkpoint(ctx, checkpointMkdir); err != nil {
		return nil, err
	}
	if err := parent.Verify(); err != nil {
		return nil, err
	}
	if err := syscall.Mkdirat(int(parent.File.Fd()), name, 0700); err != nil {
		return nil, err
	}
	child, err := kernel.OpenDirectory(parent, name)
	if err != nil {
		return nil, err
	}
	if child.Identity.Mode&0777 != 0700 {
		child.File.Close()
		return nil, state("new state directory does not have private creation permissions")
	}
	if err = s.syncDirectory(ctx, parent); err != nil {
		child.File.Close()
		return nil, err
	}
	return child, nil
}

func (s *Store) ensureDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	child, err := kernel.OpenDirectory(parent, name)
	if !errors.Is(err, syscall.ENOENT) {
		return child, err
	}
	child, err = s.newDirectory(ctx, parent, name)
	if !errors.Is(err, syscall.EEXIST) {
		return child, err
	}
	return s.concurrentDirectory(ctx, parent, name)
}

func (s *Store) concurrentDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	child, err := kernel.OpenDirectory(parent, name)
	if err != nil {
		return nil, err
	}
	if err := s.syncDirectory(ctx, parent); err != nil {
		child.File.Close()
		return nil, err
	}
	return child, nil
}

func (s *Store) syncDirectory(ctx context.Context, dir *directory) error {
	if err := s.checkpoint(ctx, checkpointSyncDirectory); err != nil {
		return err
	}
	if err := dir.Verify(); err != nil {
		return err
	}
	if err := dir.File.Sync(); err != nil {
		return storeFailure("state directory durability could not be established", err)
	}
	return nil
}

func (s *Store) writeExclusive(ctx context.Context, parent *directory, name string, data []byte) error {
	_, err := s.writeExclusiveIdentity(ctx, parent, name, data, false)
	return err
}

// writeExclusiveIdentity reports the identity of the complete file it created,
// so a caller staging it can later remove exactly that file. A failure after
// creation removes the file it created: a partial or unpublished entry would
// otherwise outlive the refusal and fail every later layout verification. A
// caller sets retain when a retry relies on what an interrupted write leaves:
// the secret area, whose backend attributes and recovers it, and a context
// reservation, which alone attributes a directory the registry does not yet
// record.
func (s *Store) writeExclusiveIdentity(ctx context.Context, parent *directory, name string, data []byte, retain bool) (syscall.Stat_t, error) {
	if err := s.checkpoint(ctx, checkpointCreateFile); err != nil {
		return syscall.Stat_t{}, err
	}
	if err := parent.Verify(); err != nil {
		return syscall.Stat_t{}, err
	}
	file, err := kernel.OpenRelative(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return syscall.Stat_t{}, err
	}
	defer file.Close()
	complete := false
	defer func() {
		if complete || retain {
			return
		}
		if created, err := kernel.StatHandle(file); err == nil {
			discardCreated(parent, name, created)
		}
	}()
	before, err := kernel.StatHandle(file)
	if err != nil || !kernel.Private(before, syscall.S_IFREG, parent.Identity.Uid, parent.Identity.Gid) || before.Mode&0777 != 0600 {
		return syscall.Stat_t{}, state("new state file is unsafe")
	}
	size := len(data)
	for len(data) > 0 {
		if err := s.checkpoint(ctx, checkpointWriteFile); err != nil {
			return syscall.Stat_t{}, err
		}
		n, err := file.Write(data[:min(len(data), 32768)])
		if err != nil || n == 0 {
			return syscall.Stat_t{}, storeFailure("state file could not be written", err)
		}
		data = data[n:]
	}
	if err := s.checkpoint(ctx, checkpointSyncFile); err != nil {
		return syscall.Stat_t{}, err
	}
	if err := file.Sync(); err != nil {
		return syscall.Stat_t{}, storeFailure("state file durability could not be established", err)
	}
	held, err := kernel.StatHandle(file)
	if err != nil || !kernel.SameIdentity(before, held) || !kernel.Private(held, syscall.S_IFREG, parent.Identity.Uid, parent.Identity.Gid) || held.Size != int64(size) {
		return syscall.Stat_t{}, state("new state file changed during publication")
	}
	current, err := kernel.OpenRelative(parent, name, kernel.PathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, state("new state file was replaced")
	}
	after, statErr := kernel.StatHandle(current)
	current.Close()
	if statErr != nil || !kernel.SameFile(held, after) {
		return syscall.Stat_t{}, state("new state file was replaced")
	}
	if err := s.syncDirectory(ctx, parent); err != nil {
		return syscall.Stat_t{}, err
	}
	complete = true
	return after, nil
}

// discardCreated removes a file this call created and then failed to publish.
// It takes no context, so a cancelled caller still cleans up, and it unlinks
// only the exact identity it was given, never an entry that has since been
// replaced or a directory that no longer verifies. Its own failure is ignored:
// the refusal that caused it is the one to report.
func discardCreated(parent *directory, name string, identity syscall.Stat_t) {
	if unlinkVerified(parent, name, identity, false) == nil {
		_ = parent.File.Sync()
	}
}

// writeExclusiveAtomic stages the bytes and renames them to name without
// replacing it, so name appears only with complete, synchronized content. A
// failure before the rename removes the stage.
func (s *Store) writeExclusiveAtomic(ctx context.Context, parent *directory, name string, data []byte) error {
	_, err := s.publishStage(ctx, parent, name, data, stagedPublication{
		subject: "immutable state", immutable: true, bound: len(data),
	}, checkpointBeforeSecretImmutableRename, checkpointAfterSecretImmutableRename)
	return err
}

func renameNoReplaceAt(parent *directory, oldName, newName string) error {
	if err := parent.Verify(); err != nil {
		return err
	}
	if len(oldName) == 0 || len(oldName) > 255 || oldName == "." || oldName == ".." || strings.ContainsAny(oldName, "/\x00") {
		return state("staged immutable state name is invalid")
	}
	if len(newName) == 0 || len(newName) > 255 || newName == "." || newName == ".." || strings.ContainsAny(newName, "/\x00") {
		return state("immutable state name is invalid")
	}
	err := nativefs.RenameNoReplace(int(parent.File.Fd()), oldName, newName)
	runtime.KeepAlive(parent.File)
	if err != nil {
		return err
	}
	// Publication is complete once renameat2 succeeds. Callers verify the
	// published entry and parent before reporting success.
	return nil
}

func errnoOf(err error) syscall.Errno {
	var errno syscall.Errno
	if err == nil {
		return 0
	}
	if errors.As(err, &errno) {
		return errno
	}
	return syscall.EINVAL
}

func syncFilesystem(file *os.File) error {
	if err := nativefs.SyncFilesystem(int(file.Fd())); err != nil {
		return storeFailure("state filesystem durability could not be established", err)
	}
	return nil
}

func rootWait(s *Store) time.Duration {
	if s != nil && s.rootWait > 0 {
		return s.rootWait
	}
	return rootLockWait
}

func awaitRoot(ctx context.Context, s *Store, dir *directory, how int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := dir.Verify(); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, rootWait(s))
	defer cancel()
	for delay := rootLockPoll; ; delay = min(2*delay, maxRootLockPoll) {
		err := syscall.Flock(int(dir.File.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return state("context state is held by another mutator")
		}
		timer := time.NewTimer(delay)
		select {
		case <-bounded.Done():
			timer.Stop()
			if err := ctx.Err(); err != nil {
				return err
			}
			return kernel.Busy("context state stayed held by another command for the whole root lock wait")
		case <-timer.C:
		}
	}
	if err := dir.Verify(); err != nil {
		syscall.Flock(int(dir.File.Fd()), syscall.LOCK_UN)
		return err
	}
	return nil
}
