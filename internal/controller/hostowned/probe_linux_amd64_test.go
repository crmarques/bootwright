//go:build linux && amd64

package hostowned

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A libvirt socket directly under the runtime directory or one level down
// counts as libvirt present; a missing or socketless directory does not.
func TestALibvirtSocketIsFoundBeneathTheRuntimeDirectory(t *testing.T) {
	base := t.TempDir()
	if found, err := socketBeneath(filepath.Join(base, "missing")); err != nil || found {
		t.Fatalf("a missing directory = %v (%v)", found, err)
	}
	nested := filepath.Join(base, "q")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if found, err := socketBeneath(base); err != nil || found {
		t.Fatalf("a socketless directory = %v (%v)", found, err)
	}
	listener, err := net.Listen("unix", filepath.Join(nested, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if found, err := socketBeneath(base); err != nil || !found {
		t.Fatalf("a nested socket = %v (%v)", found, err)
	}
	if names, err := directoryNames(base); err != nil || !slices.Equal(names, []string{"q"}) {
		t.Fatalf("names = %q (%v)", names, err)
	}
	empty := t.TempDir()
	if names, err := directoryNames(empty); err != nil || len(names) != 0 {
		t.Fatalf("an empty directory = %q (%v)", names, err)
	}
}
