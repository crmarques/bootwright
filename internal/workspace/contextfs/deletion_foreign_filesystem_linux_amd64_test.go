//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	mountedEntryBase   = "BOOTWRIGHT_TEST_MOUNTED_ENTRY_BASE"
	mountedEntryMarker = "mounted entry refused with its exit"
)

// A mount point in a context's tree is refused by the walk's open, which never
// crosses a mount, and its exit is to unmount it, never to remove what it
// holds. The mount is made in a user and mount namespace of a child process.
func TestAMountPointInAContextTreeIsUnmountedNeverRemoved(t *testing.T) {
	if base := os.Getenv(mountedEntryBase); base != "" {
		refuseMountedEntry(t, base)
		return
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "mounted"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAMountPointInAContextTreeIsUnmountedNeverRemoved$", "-test.count=1")
	command.Env = append(os.Environ(), mountedEntryBase+"="+base)
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
	}
	output, err := command.CombinedOutput()
	if strings.Contains(string(output), "user namespace unavailable") {
		t.Skipf("this host cannot mount in a user namespace: %s", output)
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Skipf("this host cannot start a process in a user namespace: %v", err)
		}
		t.Fatalf("the mounted entry was not refused with its exit: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), mountedEntryMarker) {
		t.Fatalf("the child never walked the mounted entry:\n%s", output)
	}
}

func refuseMountedEntry(t *testing.T, base string) {
	if err := syscall.Mount("tmpfs", filepath.Join(base, "mounted"), "tmpfs", 0, "mode=0700"); err != nil {
		fmt.Println("user namespace unavailable:", err)
		return
	}
	file, err := os.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	identity, err := statHandle(file)
	if err != nil {
		t.Fatal(err)
	}
	remaining := 16
	err = (&Store{}).walkContextTree(context.Background(), &directory{file: file, path: base, identity: identity}, "", inspectContextTree, &remaining)
	got := diagnostics.Of(deletionDamage(context.Background(), "alpha", err))
	want := "unmount mounted beneath the state root, which is on another filesystem whose contents are not the context's and must never be removed, " +
		"then repeat bootwright context delete --name alpha --purge, adding --allow-orphans when the deletion abandons what the context owns; or " + restoreStoreRemediation
	if len(got) != 1 || got[0].Message != "context alpha cannot be verified: mounted: "+syscall.EXDEV.Error() || got[0].Remediation != want {
		t.Fatalf("the walk refused the mounted entry with %+v (%v), want the remediation %q", got, err, want)
	}
	fmt.Println(mountedEntryMarker)
}

// An entry whose device is not its context's is on another filesystem, and its
// exit is to unmount it; every other reason keeps the exit to remove it.
func TestAnEntryOnAnotherFilesystemIsUnmountedNeverRemoved(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dir := &directory{file: file, identity: syscall.Stat_t{Dev: 1, Uid: 0, Gid: 0}}
	for name, test := range map[string]struct {
		identity syscall.Stat_t
		verb     string
	}{
		"another device":   {identity: syscall.Stat_t{Dev: 2, Mode: syscall.S_IFDIR | 0o700, Nlink: 2}, verb: "unmount "},
		"a symbolic link":  {identity: syscall.Stat_t{Dev: 2, Mode: syscall.S_IFLNK | 0o777, Nlink: 1}, verb: "remove "},
		"a public mode":    {identity: syscall.Stat_t{Dev: 1, Mode: syscall.S_IFDIR | 0o755, Nlink: 2}, verb: "remove "},
		"a hard-link file": {identity: syscall.Stat_t{Dev: 1, Mode: syscall.S_IFREG | 0o600, Nlink: 2}, verb: "remove "},
	} {
		t.Run(name, func(t *testing.T) {
			reason := unsafeEntryReason(dir, "", "entry", test.identity, nil)
			if reason == "" {
				t.Fatal("the walk admitted the entry")
			}
			got := diagnostics.Of(deletionDamage(context.Background(), "alpha", unverifiedEntryRefusal(dir, "entry", "context traversal contains unknown or unsafe state", reason, false)))
			if len(got) != 1 || !strings.HasPrefix(got[0].Remediation, test.verb+"entry beneath the state root") {
				t.Fatalf("the entry refused with %+v, want an exit that begins %q", got, test.verb)
			}
		})
	}
}
