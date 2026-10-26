//go:build linux && amd64

package hostlinux

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The host's own MAC addresses are read in canonical form, every interface
// once, and a loopback's all-zero address is no address.
func TestTheHostReadsItsOwnHardwareAddresses(t *testing.T) {
	root := t.TempDir()
	for name, address := range map[string]string{"lo": "00:00:00:00:00:00", "eno1": "AA:BB:CC:00:00:01", "br0": "aa:bb:cc:00:00:01", "eno2": "aa:bb:cc:00:00:02"} {
		directory := filepath.Join(root, "sys", "class", "net", name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "address"), []byte(address+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	addresses, err := Inspector{view: filesystemView{root: root}}.HardwareAddresses(context.Background())
	if err != nil || !slices.Equal(addresses, []string{"aa:bb:cc:00:00:01", "aa:bb:cc:00:00:02"}) {
		t.Fatalf("addresses = %v, %v", addresses, err)
	}
	if _, err := (Inspector{view: filesystemView{root: t.TempDir()}}).HardwareAddresses(context.Background()); err == nil {
		t.Fatal("a host with no readable interfaces reported addresses")
	}
}
