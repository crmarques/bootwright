//go:build linux && amd64

package inputfs

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type refusedRoot struct{ err error }

func (r refusedRoot) Root() (*os.File, error) { return nil, r.err }

func (refusedRoot) OpenAt(*os.File, string, int) (*os.File, error) { return nil, syscall.EIO }

func (refusedRoot) Close() error { return nil }

func TestARefusedRootNamesWhoWasDenied(t *testing.T) {
	for _, row := range []struct {
		err     error
		message string
	}{
		{denial{errno: syscall.EACCES}, "the invoking account cannot read this input path"},
		{denial{errno: syscall.EPERM, root: true}, "root cannot read this input path"},
		{syscall.EIO, "input root cannot be opened safely"},
	} {
		_, err := discoverThrough(context.Background(), refusedRoot{row.err})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "input.read" || !strings.HasPrefix(reported[0].Message, row.message) {
			t.Errorf("%v: diagnostics = %#v, want %q", row.err, reported, row.message)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverThrough(ctx, refusedRoot{syscall.EACCES}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled discovery = %v", err)
	}
}

func TestAnOpenFailureNamesItsCause(t *testing.T) {
	for err, code := range map[error]string{
		syscall.ENOENT:                           "input.not-found",
		syscall.ELOOP:                            "input.symlink",
		syscall.ENOTDIR:                          "input.not-directory",
		denial{errno: syscall.EACCES}:            "input.read",
		syscall.EIO:                              "input.read",
		denial{errno: syscall.EPERM, root: true}: "input.read",
	} {
		reported := diagnostics.Of(openFailure(err, "/input"))
		if len(reported) != 1 || reported[0].Code != code {
			t.Errorf("%v: diagnostics = %#v, want %s", err, reported, code)
		}
	}
}
