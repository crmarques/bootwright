package localkeyring

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// limitArea is an in-memory secret area that areacontract.Verify holds to the
// same clauses as contextfs's area (TestLimitAreaHonoursTheSecretAreaContract),
// so what a limit test proves against it holds against the store. One value is
// one Workspace callback: what it read through ReadMutable, observed and
// published ends with it, and next opens the following callback over the same
// files. It counts every mutating call, admitted or not, so a limit test can
// prove that a refusal attempted none.
type limitArea struct {
	files       map[string][]byte
	directories map[string]bool
	readOnly    bool
	mutations   int
	expected    map[string]limitExpectation
	observed    map[string]bool
	phase       limitPhase
}

type limitExpectation struct {
	exists bool
	data   []byte
}

type limitPhase int

const (
	limitBeforePublication limitPhase = iota
	limitPublishing
	limitCommitted
)

func newLimitArea(files map[string][]byte) *limitArea {
	return &limitArea{files: files, directories: map[string]bool{}, expected: map[string]limitExpectation{}, observed: map[string]bool{}}
}

// initializedLimitArea holds the files given beside the directories an
// initialized keyring keeps.
func initializedLimitArea(files map[string][]byte) *limitArea {
	area := newLimitArea(files)
	for _, name := range []string{"identities", "keys", "parts"} {
		area.directories[name] = true
	}
	return area
}

func (a *limitArea) next(readOnly bool) *limitArea {
	next := newLimitArea(a.files)
	next.directories, next.readOnly = a.directories, readOnly
	return next
}

func (a *limitArea) available(ctx context.Context, mutation bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mutation && a.readOnly {
		return errors.New("read-only secret storage refuses mutation")
	}
	if mutation && a.phase == limitCommitted {
		return errors.New("secret storage callback has already published its store")
	}
	return nil
}

func limitPath(path string, minimum, maximum int) error {
	if path == "" && minimum == 0 {
		return nil
	}
	parts := strings.Split(path, "/")
	if len(parts) < minimum || len(parts) > maximum {
		return errors.New("secret storage path depth is invalid")
	}
	for _, part := range parts {
		if part == "" || len(part) > 255 || part == "." || part == ".." {
			return errors.New("secret storage path component is invalid")
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
				return errors.New("secret storage path component is invalid")
			}
		}
	}
	return nil
}

func (a *limitArea) isFile(path string) bool {
	_, exists := a.files[path]
	return exists
}

func (a *limitArea) isDirectory(path string) bool {
	return path == "" || a.directories[path] || a.holdsFiles(path)
}

func (a *limitArea) holdsFiles(path string) bool {
	for name := range a.files {
		if strings.HasPrefix(name, path+"/") {
			return true
		}
	}
	return false
}

// parentHolds reports whether a file path's parent is a directory this area
// holds, as the store's must be before anything is written beneath it.
func (a *limitArea) parentHolds(path string) bool {
	parent, _, nested := strings.Cut(path, "/")
	return !nested || !a.isFile(parent) && a.isDirectory(parent)
}

func (a *limitArea) file(path string) ([]byte, bool, error) {
	if parent, _, nested := strings.Cut(path, "/"); nested && a.isFile(parent) || a.isDirectory(path) {
		return nil, false, errors.New("secret storage file is unsafe")
	}
	data, exists := a.files[path]
	return data, exists, nil
}

func (a *limitArea) Read(ctx context.Context, path string, maximum int) ([]byte, bool, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, false, err
	}
	if err := limitPath(path, 1, 2); err != nil {
		return nil, false, err
	}
	data, exists, err := a.file(path)
	if err != nil || !exists {
		return nil, false, err
	}
	if len(data) > maximum {
		return nil, false, errors.New("record exceeds read limit")
	}
	return bytes.Clone(data), true, nil
}

func (a *limitArea) ReadMutable(ctx context.Context, path string, maximum int) ([]byte, bool, error) {
	data, exists, err := a.Read(ctx, path, maximum)
	if err != nil {
		return nil, false, err
	}
	next := limitExpectation{exists: exists, data: bytes.Clone(data)}
	if prior, known := a.expected[path]; known && (prior.exists != next.exists || !bytes.Equal(prior.data, next.data)) {
		return nil, false, errors.New("secret storage observation changed during the callback")
	}
	a.expected[path] = next
	if exists {
		a.observed[path] = true
	}
	return data, exists, nil
}

func (a *limitArea) Entries(ctx context.Context, path string) ([]secretstore.Entry, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	if err := limitPath(path, 0, 1); err != nil {
		return nil, err
	}
	if a.isFile(path) || !a.isDirectory(path) {
		return nil, errors.New("secret storage directory does not exist")
	}
	prefix := path
	if prefix != "" {
		prefix += "/"
	}
	seen := map[string]secretstore.Entry{}
	for name, data := range a.files {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			if head, _, nested := strings.Cut(rest, "/"); nested {
				seen[head] = secretstore.Entry{Name: head, Directory: true}
			} else {
				seen[rest] = secretstore.Entry{Name: rest, Size: int64(len(data))}
			}
		}
	}
	for name := range a.directories {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" && !strings.Contains(rest, "/") {
			seen[rest] = secretstore.Entry{Name: rest, Directory: true}
		}
	}
	entries := slices.SortedFunc(maps.Values(seen), func(x, y secretstore.Entry) int { return strings.Compare(x.Name, y.Name) })
	for _, entry := range entries {
		a.observed[prefix+entry.Name] = true
	}
	return entries, nil
}

func (a *limitArea) EnsureDirectory(ctx context.Context, path string) error {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if err := limitPath(path, 1, 1); err != nil {
		return err
	}
	if a.isFile(path) {
		return errors.New("secret storage directory is unsafe")
	}
	if !a.isDirectory(path) {
		a.phase = limitPublishing
		a.directories[path] = true
	}
	return nil
}

func (a *limitArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if err := limitPath(path, 1, 2); err != nil {
		return err
	}
	a.phase = limitPublishing
	if !a.parentHolds(path) {
		return errors.New("secret storage parent directory is unsafe")
	}
	if a.isFile(path) || a.isDirectory(path) {
		return errors.New("entry already exists")
	}
	a.publish(path, data)
	return nil
}

func (a *limitArea) PublishExclusive(ctx context.Context, path string, data []byte) error {
	return a.WriteExclusive(ctx, path, data)
}

func (a *limitArea) publish(path string, data []byte) {
	a.files[path] = bytes.Clone(data)
	a.expected[path] = limitExpectation{exists: true, data: bytes.Clone(data)}
	a.observed[path] = true
}

func (a *limitArea) Replace(ctx context.Context, path string, replacement, expected []byte) (secretstore.Outcome, error) {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return secretstore.NotCommitted, err
	}
	if err := limitPath(path, 1, 2); err != nil {
		return secretstore.NotCommitted, err
	}
	expectation, known := a.expected[path]
	if !known || !bytes.Equal(expectation.data, expected) {
		return secretstore.NotCommitted, errors.New("secret storage replacement lacks its exact read expectation")
	}
	a.phase = limitPublishing
	current, exists, err := a.file(path)
	if err != nil || exists != expectation.exists || !bytes.Equal(current, expectation.data) || !a.parentHolds(path) {
		return secretstore.NotCommitted, errors.New("secret state was replaced or modified before publication")
	}
	a.publish(path, replacement)
	if path == secretstore.RecordPath {
		a.phase = limitCommitted
	}
	return secretstore.Committed, nil
}

func (a *limitArea) Sync(ctx context.Context, path string) error {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if err := limitPath(path, 0, 1); err != nil {
		return err
	}
	if a.isFile(path) || !a.isDirectory(path) {
		return errors.New("secret storage directory does not exist")
	}
	return nil
}

func (a *limitArea) SyncFile(ctx context.Context, path string) error {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if err := limitPath(path, 1, 2); err != nil {
		return err
	}
	if !a.isFile(path) || !a.observed[path] {
		return errors.New("secret file synchronization lacks an exact prior observation")
	}
	return nil
}

func (a *limitArea) Prune(ctx context.Context, expectedStore []byte, paths []string) error {
	a.mutations++
	if err := a.available(ctx, false); err != nil {
		return err
	}
	if a.readOnly || a.phase == limitPublishing {
		return errors.New("secret cleanup is unavailable in the current publication phase")
	}
	if len(paths) == 0 {
		return nil
	}
	store, known := a.expected[secretstore.RecordPath]
	if !known || !store.exists || !bytes.Equal(store.data, expectedStore) || !bytes.Equal(a.files[secretstore.RecordPath], store.data) {
		return errors.New("secret cleanup lacks the exact published store expectation")
	}
	return a.prune(paths, nil)
}

func (a *limitArea) PruneUnpublished(ctx context.Context, guards []secretstore.RecordExpectation, paths []string) error {
	a.mutations++
	if err := a.available(ctx, true); err != nil {
		return err
	}
	store, known := a.expected[secretstore.RecordPath]
	if a.phase != limitBeforePublication || !known || store.exists || a.isFile(secretstore.RecordPath) || len(guards) == 0 || len(guards) > 8 {
		return errors.New("unpublished secret cleanup lacks bounded publication guards")
	}
	guarded, total := map[string]bool{}, 0
	for _, guard := range guards {
		expected, known := a.expected[guard.Path]
		if limitPath(guard.Path, 1, 1) != nil || guard.Path == secretstore.RecordPath || guarded[guard.Path] || !known || !expected.exists ||
			len(guard.Data) == 0 || len(guard.Data) > (8<<20)-total || !bytes.Equal(expected.data, guard.Data) || !bytes.Equal(a.files[guard.Path], guard.Data) {
			return errors.New("unpublished secret cleanup guard lacks its exact bounded observation")
		}
		total += len(guard.Data)
		guarded[guard.Path] = true
	}
	return a.prune(paths, guarded)
}

// prune removes observed files and empty directories, and nothing unless it
// can remove every one: never the store record or a guard.
func (a *limitArea) prune(paths []string, guarded map[string]bool) error {
	seen := map[string]bool{}
	for _, path := range paths {
		empty := a.directories[path] && !a.holdsFiles(path)
		if limitPath(path, 1, 2) != nil || path == secretstore.RecordPath || seen[path] || guarded[path] || !a.observed[path] || !a.isFile(path) && !empty {
			return errors.New("secret cleanup target lacks an exact prior observation")
		}
		seen[path] = true
	}
	for _, path := range paths {
		delete(a.files, path)
		delete(a.directories, path)
		delete(a.observed, path)
		delete(a.expected, path)
	}
	return nil
}
