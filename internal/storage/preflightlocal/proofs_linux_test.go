package preflightlocal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestRootProofsAreRootOwnedRegularFilesWithinTheirBound(t *testing.T) {
	if data, err := readRootProof("/proc/version", 64<<10); err != nil || len(data) == 0 {
		t.Fatalf("a root-owned proof was refused: %v", err)
	}
	if _, err := readRootProof("/proc/version", 1); err == nil || !strings.Contains(err.Error(), "byte bound") {
		t.Fatalf("a proof beyond its bound: %v", err)
	}
	root := t.TempDir()
	owned := filepath.Join(root, "proof")
	if err := os.WriteFile(owned, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if _, err := readRootProof(owned, 64); err == nil || !strings.Contains(err.Error(), "root-owned") {
			t.Fatalf("a proof another account owns: %v", err)
		}
	}
	if err := os.Symlink("/proc", filepath.Join(root, "proc")); err != nil {
		t.Fatal(err)
	}
	if _, err := readRootProof(filepath.Join(root, "proc", "version"), 64<<10); err == nil {
		t.Fatal("a proof reached through a link was read")
	}
	if _, err := readRootProof("relative", 64); err == nil {
		t.Fatal("a relative proof path was read")
	}
}

func TestProcessCreditReadsTheProcessAndRefusesAnotherDomain(t *testing.T) {
	_, err := processCredit(os.Getpid(), preflight.Allocation{Domain: "absent-domain", UUID: "absent-uuid"})
	if err == nil || !strings.Contains(err.Error(), "does not name the expected domain") {
		t.Fatalf("a process naming no domain: %v", err)
	}
	fields := strings.Repeat("0 ", 19) + "4242 rest"
	if start := processStart([]byte("17 (qemu kvm) " + fields)); start != "4242" {
		t.Fatalf("the start time is %q", start)
	}
	for _, malformed := range []string{"17 qemu S 1", "17 (qemu) S 1 2"} {
		if start := processStart([]byte(malformed)); start != "" {
			t.Fatalf("%q gave the start time %q", malformed, start)
		}
	}
}
