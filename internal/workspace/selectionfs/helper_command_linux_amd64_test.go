//go:build linux && amd64

package selectionfs

import (
	"context"
	"slices"
	"syscall"
	"testing"
	"time"
)

// The helper runs the program's own procfs-pinned image, never a pathname, so
// a file renamed over the program's path can never run as the selection
// account, and root never resolves that path, which a root-squashed home
// refuses it; its credential drop, parent-death kill and fixed environment
// stay as they were.
func TestTheSelectionHelperRunsTheProcfsPinnedExecutable(t *testing.T) {
	options := Options{UID: 60001, GID: 60002, Home: "/home/synthetic", Groups: []uint32{60002, 60003}}
	command := helperCommand(context.Background(), []byte("{}"), options)
	if command.Path != "/proc/self/exe" || !slices.Equal(command.Args, []string{"/proc/self/exe", HelperMode}) {
		t.Fatalf("helper command = %q %q, want /proc/self/exe in helper mode", command.Path, command.Args)
	}
	attributes := command.SysProcAttr
	if attributes == nil || attributes.Credential == nil || attributes.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("helper process attributes = %#v", attributes)
	}
	if credential := attributes.Credential; credential.Uid != 60001 || credential.Gid != 60002 || !slices.Equal(credential.Groups, []uint32{60002, 60003}) {
		t.Fatalf("helper credential = %#v", credential)
	}
	if !slices.Equal(command.Env, []string{"LANG=C", "LC_ALL=C"}) || command.Dir != "/" || command.Stdin == nil || command.WaitDelay != time.Second {
		t.Fatalf("helper environment = %q in %q", command.Env, command.Dir)
	}
}
