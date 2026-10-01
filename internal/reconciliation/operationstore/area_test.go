package operationstore

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areadouble"
)

// memoryArea is an in-memory Area that areacontract.Verify holds to the same
// clauses as contextfs's area (TestMemoryAreaHonoursTheAreaContract): exclusive
// creation, expectation-checked replacement and append-only logs, with no
// other way to change a published byte. The real area serializes concurrent
// blocks through the filesystem, so this one holds a mutex and answers each
// call whole.
type memoryArea struct {
	mutex       sync.Mutex
	files       map[string][]byte
	directories map[string]bool
	fail        map[string]error
	reads       int
	appends     int
	syncs       []string
	location    string
	reference   string
	// landed, when set, runs once a write or replacement has landed and
	// before the call returns, still holding the area, so a test can fail the
	// next write exactly where a kill between the two would fall.
	landed func(operation, target string)
}

func newArea() *memoryArea {
	return &memoryArea{files: map[string][]byte{}, directories: map[string]bool{"": true}, fail: map[string]error{}}
}

func (a *memoryArea) admit(ctx context.Context, target string, record bool) error {
	return areadouble.Admit(ctx, a.files, a.directories, target, record)
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
	if err := a.admit(ctx, target, true); err != nil {
		return nil, false, err
	}
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
	if err := a.admit(ctx, target, false); err != nil {
		return nil, err
	}
	if err := a.check("entries", target); err != nil {
		return nil, err
	}
	return areadouble.Entries[Entry](a.files, a.directories, target), nil
}

func (a *memoryArea) EnsureDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return err
	}
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
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
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
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
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
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if err := a.check("append", target); err != nil {
		return err
	}
	a.files[target] = append(a.files[target], data...)
	return nil
}

// RemoveDirectory removes only an empty directory, as the kernel does, and
// never the area itself.
func (a *memoryArea) RemoveDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return err
	}
	if target == "" {
		return errors.New("the area itself is not removable")
	}
	if err := a.check("remove", target); err != nil {
		return err
	}
	for name := range a.directories {
		if strings.HasPrefix(name, target+"/") {
			return errors.New("directory not empty")
		}
	}
	for name := range a.files {
		if strings.HasPrefix(name, target+"/") {
			return errors.New("directory not empty")
		}
	}
	delete(a.directories, target)
	return nil
}

// RemoveRecord removes a record only while it holds exactly expected, and
// keeps the directories above it, which the real area leaves in place.
func (a *memoryArea) RemoveRecord(ctx context.Context, target string, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if err := a.check("unlink", target); err != nil {
		return err
	}
	current, exists := a.files[target]
	if !exists {
		return nil
	}
	if !slices.Equal(current, expected) {
		return errors.New("the record changed before its removal")
	}
	delete(a.files, target)
	for parent := path.Dir(target); parent != "."; parent = path.Dir(parent) {
		a.directories[parent] = true
	}
	return nil
}

// Sync refuses a record, as the contract requires, so a caller that names one
// fails in a test rather than on a host.
func (a *memoryArea) Sync(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return err
	}
	if err := a.check("sync", target); err != nil {
		return err
	}
	a.syncs = append(a.syncs, target)
	return nil
}

func (a *memoryArea) Location() string { return a.location }

func (a *memoryArea) Reference() string { return a.reference }
