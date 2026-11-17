//go:build linux && amd64

package selectionfs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// copyingContext copies every descriptor open on path each time a call checks
// it after its first check, as a child forked meanwhile holds them until it
// executes.
type copyingContext struct {
	context.Context
	t      *testing.T
	path   string
	checks int
	copies []int
}

func (c *copyingContext) Err() error {
	c.checks++
	if c.checks > 1 {
		c.copies = append(c.copies, copyDescriptors(c.t, c.path)...)
	}
	return c.Context.Err()
}

func copyDescriptors(t *testing.T, path string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	copies := []int{}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || target != path {
			continue
		}
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		copied, err := syscall.Dup(fd)
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, copied)
	}
	return copies
}

// A selection call unlocks its directory before it closes it, so a copy of
// the descriptor a child forked meanwhile still holds never keeps the next
// call out.
func TestSelectionReleasesItsLockWhileACopyOfItsDescriptorLives(t *testing.T) {
	store, home := fixture(t)
	selected := contexts.Selection{Version: contexts.SelectionVersion, Name: "test"}
	if err := store.Write(context.Background(), selected); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".bootwright")
	for _, action := range []string{"read", "write"} {
		ctx := &copyingContext{Context: context.Background(), t: t, path: path}
		var err error
		if action == "read" {
			_, err = store.Read(ctx)
		} else {
			err = store.Write(ctx, selected)
		}
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if len(ctx.copies) == 0 {
			t.Fatalf("%s: no descriptor of %s was copied while the call held it", action, path)
		}
		dir, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		dir.Close()
		for _, copied := range ctx.copies {
			syscall.Close(copied)
		}
		if err != nil {
			t.Fatalf("%s left the selection locked for a copy of its descriptor: %v", action, err)
		}
	}
}
