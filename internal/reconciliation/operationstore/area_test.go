package operationstore

import (
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
)

// memoryArea models the contract contextfs implements: exclusive creation,
// expectation-checked replacement and append-only logs, with no other way to
// change a published byte. The real area serializes concurrent blocks through
// the filesystem, so this one holds a mutex and answers each call whole.
type memoryArea struct {
	mutex       sync.Mutex
	files       map[string][]byte
	directories map[string]bool
	fail        map[string]error
	reads       int
	appends     int
	syncs       []string
	location    string
	// landed, when set, runs once a write or replacement has landed and
	// before the call returns, still holding the area, so a test can fail the
	// next write exactly where a kill between the two would fall.
	landed func(operation, target string)
}

func newArea() *memoryArea {
	return &memoryArea{files: map[string][]byte{}, directories: map[string]bool{"": true}, fail: map[string]error{}}
}

func (a *memoryArea) check(operation, target string) error {
	if err := a.fail[operation+" "+target]; err != nil {
		return err
	}
	return a.fail[operation]
}

func (a *memoryArea) Read(ctx context.Context, target string, maximum int) ([]byte, bool, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.reads++
	if err := a.check("read", target); err != nil {
		return nil, false, err
	}
	data, ok := a.files[target]
	if !ok {
		return nil, false, nil
	}
	if maximum > 0 && len(data) > maximum {
		return nil, false, errors.New("bounds")
	}
	return slices.Clone(data), true, nil
}

func (a *memoryArea) Entries(ctx context.Context, target string) ([]Entry, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.check("entries", target); err != nil {
		return nil, err
	}
	seen := map[string]Entry{}
	prefix := target
	if prefix != "" {
		prefix += "/"
	}
	for name, data := range a.files {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		if head, _, nested := strings.Cut(rest, "/"); nested {
			seen[head] = Entry{Name: head, Directory: true}
		} else {
			seen[rest] = Entry{Name: rest, Size: int64(len(data))}
		}
	}
	for name := range a.directories {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" || strings.Contains(rest, "/") {
			continue
		}
		seen[rest] = Entry{Name: rest, Directory: true}
	}
	entries := slices.Collect(maps.Values(seen))
	slices.SortFunc(entries, func(x, y Entry) int { return strings.Compare(x.Name, y.Name) })
	return entries, nil
}

func (a *memoryArea) EnsureDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.check("ensure", target); err != nil {
		return err
	}
	for current := target; current != "." && current != ""; current = path.Dir(current) {
		a.directories[current] = true
	}
	return nil
}

func (a *memoryArea) WriteExclusive(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.check("write", target); err != nil {
		return err
	}
	if _, exists := a.files[target]; exists {
		return errors.New("exists")
	}
	a.files[target] = slices.Clone(data)
	a.land("write", target)
	return nil
}

func (a *memoryArea) Replace(ctx context.Context, target string, data, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.check("replace", target); err != nil {
		return err
	}
	current, exists := a.files[target]
	if expected == nil {
		if exists {
			return errors.New("destination exists")
		}
	} else if !exists || !slices.Equal(current, expected) {
		return errors.New("expectation")
	}
	a.files[target] = slices.Clone(data)
	a.land("replace", target)
	return nil
}

func (a *memoryArea) land(operation, target string) {
	if a.landed != nil {
		a.landed(operation, target)
	}
}

func (a *memoryArea) Append(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.appends++
	if err := a.check("append", target); err != nil {
		return err
	}
	a.files[target] = append(a.files[target], data...)
	return nil
}

// Sync refuses a record here exactly as contextfs does, so a caller that names
// one fails in a test rather than on a host.
func (a *memoryArea) Sync(ctx context.Context, target string) error {
	if err := a.check("sync", target); err != nil {
		return err
	}
	if _, isFile := a.files[target]; isFile {
		return errors.New("not a directory")
	}
	a.syncs = append(a.syncs, target)
	return nil
}

func (a *memoryArea) Location() string { return a.location }
