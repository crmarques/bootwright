package inputfs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An unmounted browse-mode autofs directory answers a path-only open with its
// empty mount point; only a directory open mounts what lies beneath it.
func TestReaderMountsBrowseModeDirectoriesBeforeLookingBeneath(t *testing.T) {
	files := newVirtualFiles(t)
	environment := files.write(t, "auto/input/environment.yaml", "environment")
	stub := t.TempDir()
	auto := filepath.Join(files.prefix, "auto")
	files.replace = func(path string, flags int) (*os.File, bool, error) {
		if path == auto && flags&syscall.O_DIRECTORY == 0 {
			return openHost(stub, flags)
		}
		return nil, false, nil
	}
	reader := Reader{Files: files}
	ctx := context.Background()
	for name, read := range map[string]func() (desiredstate.Sources, error){
		"an automounted ancestor":   func() (desiredstate.Sources, error) { return reader.ReadDirectory(ctx, filepath.Dir(environment)) },
		"an automounted root":       func() (desiredstate.Sources, error) { return reader.ReadDirectory(ctx, auto) },
		"an automounted descendant": func() (desiredstate.Sources, error) { return reader.ReadDirectory(ctx, files.prefix) },
	} {
		sources, err := read()
		if err != nil || len(sources.Files) != 1 || sources.Files[0].Path() != environment {
			t.Fatalf("%s: %v (%#v)", name, sourcePaths(sources.Files), diagnostics.Of(err))
		}
	}
	data, err := reader.ReadFile(ctx, environment, 64)
	if err != nil || string(data) != "environment" {
		t.Fatalf("ReadFile = %q (%#v)", data, diagnostics.Of(err))
	}
}
