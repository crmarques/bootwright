//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// told is the message and correction a scoped failure carries.
func told(err error) string {
	var scoped *prerequisites.ScopedFailure
	if !errors.As(err, &scoped) {
		return ""
	}
	return scoped.Message + " " + scoped.Correction
}

// countingFS counts every file opened in the carried set.
type countingFS struct {
	fstest.MapFS
	opened int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opened++
	return c.MapFS.Open(name)
}

func TestTheCacheIsFilledFromTheCarriedWheelsAndAWarmCacheCopiesNone(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	carried := &countingFS{MapFS: carriedWheels(adapter.lock)}
	adapter.carried = carried
	if err := adapter.Fetch(context.Background(), "3.14"); err != nil {
		t.Fatal(err)
	}
	files, _ := adapter.lock.Files("3.14")
	if got, want := carried.opened, len(files)+1; got != want {
		t.Fatalf("a cold cache opened %d carried files, want one per locked file and the installer, %d", got, want)
	}
	wheels, err := adapter.Wheels(context.Background(), "3.14")
	if err != nil {
		t.Fatal(err)
	}
	for _, wheel := range wheels {
		if !wheel.Cached {
			t.Fatalf("%s is not cached after a fetch", wheel.Filename)
		}
	}
	carried.opened = 0
	if err := adapter.Fetch(context.Background(), "3.14"); err != nil || carried.opened != 0 {
		t.Fatalf("a warm cache opened %d carried files (%v)", carried.opened, err)
	}
	if err := os.WriteFile(adapter.cachePath(files[0]), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Fetch(context.Background(), "3.14"); err != nil || carried.opened != 1 {
		t.Fatalf("one tampered cache entry opened %d carried files (%v), want 1", carried.opened, err)
	}
}

func TestACarriedWheelWithAnotherHashIsRefused(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	files, _ := adapter.lock.Files("3.14")
	carried := carriedWheels(adapter.lock)
	carried[files[0].Filename].Data[0] ^= 0xff
	adapter.carried = carried
	if err := adapter.Fetch(context.Background(), "3.14"); !strings.Contains(told(err), "does not match its locked size and SHA-256") {
		t.Fatalf("a carried wheel whose bytes differ from the lock's SHA-256: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(adapter.wheels, files[0].SHA256))
	if len(entries) != 0 {
		t.Fatalf("a refused wheel left %v in its cache directory", entries)
	}
	if adapter.cached(files[0]) == nil {
		t.Fatal("the refused wheel reads as cached")
	}
}

// A build without make wheels carries nothing: setup refuses before it plans,
// naming the build step, and nothing falls back to a download.
func TestAnExecutableWithoutItsWheelsRefusesBeforeThePlan(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	adapter.carried = fstest.MapFS{".gitignore": {Data: []byte("*.whl\n")}}
	for name, call := range map[string]func() error{
		"wheels": func() error { _, err := adapter.Wheels(context.Background(), "3.14"); return err },
		"fetch":  func() error { return adapter.Fetch(context.Background(), "3.14") },
	} {
		err := call()
		if !strings.Contains(told(err), "does not carry the locked wheel") || !strings.Contains(told(err), "built with make build") {
			t.Errorf("%s: %v", name, err)
		}
	}
	host.fillCache(adapter, "3.14")
	if _, err := adapter.Wheels(context.Background(), "3.14"); err != nil {
		t.Errorf("a host whose cache holds every wheel needs none carried: %v", err)
	}
}
