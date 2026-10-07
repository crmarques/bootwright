//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const maximumRecord = 4096

var selectionName = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)

func validSelection(s contexts.Selection) bool {
	return s.Version == contexts.SelectionVersion && selectionName.MatchString(s.Name)
}

func (s *Store) local(ctx context.Context, action string, value contexts.Selection) (contexts.Selection, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Selection{}, err
	}
	if s.options.UID != os.Getuid() || s.options.UID != os.Geteuid() || s.options.GID != os.Getegid() {
		return contexts.Selection{}, state("selection access requires the invoking account identity")
	}
	if action != "read" && !validSelection(value) {
		return contexts.Selection{}, state("selection record is invalid")
	}
	dir, err := s.openDirectory(action == "write")
	if errors.Is(err, syscall.ENOENT) && action != "write" {
		return contexts.Selection{}, nil
	}
	if err != nil {
		return contexts.Selection{}, err
	}
	defer dir.Close()
	lock := syscall.LOCK_EX
	if action == "read" {
		lock = syscall.LOCK_SH
	}
	if err := syscall.Flock(int(dir.Fd()), lock|syscall.LOCK_NB); err != nil {
		return contexts.Selection{}, state("selection is held by another invocation")
	}
	// The opened directory is also the lock object; replacement is detected
	// again immediately before publishing or unlinking an entry.
	if err := s.verifyDirectory(dir); err != nil {
		return contexts.Selection{}, err
	}
	current, err := s.readRecord(dir)
	if err != nil {
		return contexts.Selection{}, err
	}
	if current.file != nil {
		defer current.file.Close()
	}
	if err := ctx.Err(); err != nil {
		return contexts.Selection{}, err
	}
	if action == "read" {
		if err := s.verifyRecord(dir, current); err != nil {
			return contexts.Selection{}, err
		}
		return current.value, nil
	}
	if action == "clear" {
		if err := s.verifyRecord(dir, current); err != nil {
			return contexts.Selection{}, err
		}
		if current.value != value {
			return contexts.Selection{}, nil
		}
		if err := syscall.Unlinkat(int(dir.Fd()), "context"); err != nil {
			return contexts.Selection{}, state("selection could not be cleared")
		}
		return contexts.Selection{}, dir.Sync()
	}
	return s.publishRecord(ctx, dir, current, value)
}

func (s *Store) publishRecord(ctx context.Context, dir *os.File, current selectionRecord, value contexts.Selection) (contexts.Selection, error) {
	data, _ := json.Marshal(value)
	data = append(data, '\n')
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return contexts.Selection{}, state("selection staging identity is unavailable")
	}
	name := ".context-" + hex.EncodeToString(token[:])
	fd, err := syscall.Openat(int(dir.Fd()), name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return contexts.Selection{}, state("selection staging file could not be created")
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	defer syscall.Unlinkat(int(dir.Fd()), name) // Only our exclusive staging name.
	if s.check(file, syscall.S_IFREG, 0600) != nil {
		return contexts.Selection{}, state("selection staging file owner, type, links or permissions are unsafe")
	}
	if _, err := file.Write(data); err != nil {
		return contexts.Selection{}, state("selection staging file could not be written")
	}
	if err := file.Sync(); err != nil {
		return contexts.Selection{}, state("selection staging file could not be synchronized")
	}
	if err := ctx.Err(); err != nil {
		return contexts.Selection{}, err
	}
	if err := s.verifyDirectory(dir); err != nil {
		return contexts.Selection{}, err
	}
	staged, err := syscall.Openat(int(dir.Fd()), name, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return contexts.Selection{}, state("selection staging file changed")
	}
	var held, named syscall.Stat_t
	statErr := syscall.Fstat(int(file.Fd()), &held)
	namedErr := syscall.Fstat(staged, &named)
	syscall.Close(staged)
	if statErr != nil || namedErr != nil || held.Dev != named.Dev || held.Ino != named.Ino || held.Size != int64(len(data)) || s.check(file, syscall.S_IFREG, 0600) != nil {
		return contexts.Selection{}, state("selection staging file changed")
	}
	if err := s.verifyRecord(dir, current); err != nil {
		return contexts.Selection{}, err
	}
	if err := syscall.Renameat(int(dir.Fd()), name, int(dir.Fd()), "context"); err != nil {
		return contexts.Selection{}, state("selection could not be published")
	}
	if err := dir.Sync(); err != nil {
		return contexts.Selection{}, state("selection publication durability is unknown")
	}
	return value, nil
}

func (s *Store) check(file *os.File, kind, mode uint32) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil || !s.private(stat, kind, mode) {
		if kind == syscall.S_IFDIR {
			return unsafeSelectionDirectory()
		}
		return unsafeSelectionFile()
	}
	return nil
}

func unsafeSelectionFile() error {
	return contexts.StateErrorWithRemediation("selection file ~/.bootwright/context has an unsafe owner, type, link count or mode",
		"remove it and select again with bootwright context use --name <context>")
}

func unsafeSelectionDirectory() error {
	return contexts.StateErrorWithRemediation("~/.bootwright has an unsafe owner, type or mode",
		"make ~/.bootwright a directory you own with mode 0700")
}

func (s *Store) private(stat syscall.Stat_t, kind, mode uint32) bool {
	return stat.Mode&syscall.S_IFMT == kind && stat.Mode&07777 == mode && stat.Uid == uint32(s.options.UID) && stat.Gid == uint32(s.options.GID) && (kind != syscall.S_IFREG || stat.Nlink == 1)
}

// O_PATH permits metadata inspection without opening a substituted device for
// I/O. O_NOFOLLOW keeps a substituted symlink as the inspected object.
const pathHandle = 0x200000

// unsafeEntry reports an entry that exists but is not the store's private
// object, such as a symlink, socket or unreadable file an open refused.
func (s *Store) unsafeEntry(dir int, name string, kind, mode uint32) bool {
	fd, err := syscall.Openat(dir, name, pathHandle|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	return syscall.Fstat(fd, &stat) == nil && !s.private(stat, kind, mode)
}

func (s *Store) openDirectory(create bool) (*os.File, error) {
	home := s.options.Home
	if len(home) > 4096 || !filepath.IsAbs(home) || filepath.Clean(home) != home || home == "/" || strings.ContainsRune(home, 0) {
		return nil, state("selection account home is invalid")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, state("selection account home cannot be opened")
	}
	for _, part := range strings.Split(strings.TrimPrefix(home, "/"), "/") {
		next, nextErr := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if nextErr != nil {
			return nil, state("selection account home cannot be opened safely")
		}
		fd = next
	}
	defer syscall.Close(fd)
	var homeStat syscall.Stat_t
	if err := syscall.Fstat(fd, &homeStat); err != nil || homeStat.Uid != uint32(s.options.UID) || homeStat.Mode&0022 != 0 {
		return nil, state("selection account home ownership or permissions are unsafe")
	}
	if create {
		err := syscall.Mkdirat(fd, ".bootwright", 0700)
		if err != nil && !errors.Is(err, syscall.EEXIST) {
			return nil, state("selection directory could not be created")
		}
		if err == nil {
			if err := syscall.Fsync(fd); err != nil {
				return nil, state("selection directory durability is unknown")
			}
		}
	}
	child, err := syscall.Openat(fd, ".bootwright", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, err
		}
		if s.unsafeEntry(fd, ".bootwright", syscall.S_IFDIR, 0700) {
			return nil, unsafeSelectionDirectory()
		}
		return nil, state("selection directory cannot be opened safely")
	}
	file := os.NewFile(uintptr(child), ".bootwright")
	if err := s.check(file, syscall.S_IFDIR, 0700); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (s *Store) verifyDirectory(held *os.File) error {
	named, err := s.openDirectory(false)
	if err != nil {
		return state("selection directory changed during access")
	}
	defer named.Close()
	var a, b syscall.Stat_t
	if syscall.Fstat(int(held.Fd()), &a) != nil || syscall.Fstat(int(named.Fd()), &b) != nil || a.Dev != b.Dev || a.Ino != b.Ino {
		return state("selection directory changed during access")
	}
	return s.check(held, syscall.S_IFDIR, 0700)
}

// A held handle prevents inode reuse until the observation has been checked at
// the return or mutation boundary. A nil handle records an observed absence.
type selectionRecord struct {
	value    contexts.Selection
	file     *os.File
	identity syscall.Stat_t
}

func sameRecord(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func (s *Store) verifyRecord(dir *os.File, observed selectionRecord) error {
	if err := s.verifyDirectory(dir); err != nil {
		return err
	}
	fd, err := syscall.Openat(int(dir.Fd()), "context", pathHandle|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) && observed.file == nil {
		return nil
	}
	if err != nil {
		return state("selection file changed during access")
	}
	defer syscall.Close(fd)
	var held, named syscall.Stat_t
	if observed.file == nil || syscall.Fstat(fd, &named) != nil || syscall.Fstat(int(observed.file.Fd()), &held) != nil || !sameRecord(observed.identity, held) || !sameRecord(observed.identity, named) {
		return state("selection file changed during access")
	}
	return nil
}

func (s *Store) readRecord(dir *os.File) (selectionRecord, error) {
	var result selectionRecord
	fd, err := syscall.Openat(int(dir.Fd()), "context", syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return result, nil
	}
	if err != nil {
		if s.unsafeEntry(int(dir.Fd()), "context", syscall.S_IFREG, 0600) {
			return result, unsafeSelectionFile()
		}
		return result, state("selection file cannot be opened safely")
	}
	file := os.NewFile(uintptr(fd), "context")
	keep := false
	defer func() {
		if !keep {
			file.Close()
		}
	}()
	if err := s.check(file, syscall.S_IFREG, 0600); err != nil {
		return result, err
	}
	var before, after syscall.Stat_t
	if syscall.Fstat(fd, &before) != nil || before.Size < 0 {
		return result, state("selection file cannot be inspected safely")
	}
	if !s.private(before, syscall.S_IFREG, 0600) {
		return result, unsafeSelectionFile()
	}
	// Content this store cannot read is the account's own superseded record,
	// not an unsafe object: hold it for identity, report no selection, and let
	// the next write replace it.
	if before.Size > maximumRecord {
		result.file, result.identity = file, before
		keep = true
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumRecord+1))
	if err != nil || len(data) > maximumRecord || syscall.Fstat(fd, &after) != nil || !sameRecord(before, after) || int64(len(data)) != after.Size {
		return result, state("selection file changed while reading")
	}
	if value, err := decodeSelection(data); err == nil {
		result.value = value
	}
	result.file, result.identity = file, after
	keep = true
	return result, nil
}

func decodeSelection(data []byte) (contexts.Selection, error) {
	var result contexts.Selection
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return result, state("selection record is invalid")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return result, state("selection record is invalid")
		}
		seen[key] = true
		switch key {
		case "version":
			err = decoder.Decode(&result.Version)
		case "name":
			err = decoder.Decode(&result.Name)
		default:
			return result, state("selection record is invalid")
		}
		if err != nil {
			return result, state("selection record is invalid")
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || !validSelection(result) {
		return result, state("selection record is invalid")
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return result, state("selection record is not canonical")
	}
	return result, nil
}
