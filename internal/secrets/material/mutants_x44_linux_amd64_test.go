//go:build linux && amd64

package material

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Two held files that claim the same part are refused before either is
// returned, so a request can never replace one part's bytes with another
// file's (X33's mutants, B255).
func TestHeldFilesThatClaimOnePartTwiceAreRefused(t *testing.T) {
	directory := t.TempDir()
	open := func(name string) *os.File {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("value"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		return file
	}
	held := func(file *os.File, path string) heldPart {
		var stat syscall.Stat_t
		if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
			t.Fatal(err)
		}
		return heldPart{request: fileRequest{Part: secrets.ValuePart, Path: path}, file: file, before: stat}
	}
	parts, err := readHeldParts(context.Background(), []heldPart{held(open("first"), "~/first"), held(open("second"), "~/second")}, "secret.input", false)
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Message != "secret file request contains a duplicate part" || found[0].Source == nil || found[0].Source.Path != "~/second" || parts != nil {
		t.Fatalf("a part claimed twice was read: %v, %+v", parts, found)
	}
}
