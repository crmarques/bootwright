package preflightlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestMemoryFactsRequireOneBoundedQuantity(t *testing.T) {
	for _, row := range []struct {
		input string
		value int64
		ok    bool
	}{{"MemAvailable: 12 kB\n", 12288, true}, {"MemFree: 12 kB\n", 0, false}, {"MemAvailable: 12 MB\n", 0, false}, {"MemAvailable: -1 kB\n", 0, false}, {"MemAvailable: 9223372036854775807 kB\n", 0, false}, {"MemAvailable: 1 kB\nMemAvailable: 2 kB\n", 0, false}} {
		got, ok := parseMemory([]byte(row.input))
		if got != row.value || ok != row.ok {
			t.Fatalf("%q: %d %t", row.input, got, ok)
		}
	}
}
func TestRemoteTargetNeverReadsLocalFacts(t *testing.T) {
	facts, err := (Observer{}).Observe(context.Background(), preflight.Target{Host: "remote", Pool: "/ignored"})
	if err != nil || facts.CPUKnown || facts.MemoryKnown || facts.DiskKnown || facts.Reason == "" {
		t.Fatalf("%+v %v", facts, err)
	}
}

func TestADiskOnlyLocalTargetReadsOnlyItsFilesystem(t *testing.T) {
	facts, err := (Observer{}).Observe(context.Background(), preflight.Target{Host: "controller", Local: true, Pool: t.TempDir() + "/absent/install.iso", DiskOnly: true})
	if err != nil || !facts.DiskKnown || facts.DiskIdentity == "" || facts.CPUKnown || facts.MemoryKnown || facts.Reusable != nil {
		t.Fatalf("%+v %v", facts, err)
	}
	facts, err = (Observer{}).Observe(context.Background(), preflight.Target{Host: "controller", Local: true, Pool: "relative", DiskOnly: true})
	if err != nil || facts.DiskKnown || facts.Reason == "" {
		t.Fatalf("%+v %v", facts, err)
	}
}

func TestADiskOnlyLocalTargetObservesAnExistingFileItCannotRead(t *testing.T) {
	directory := t.TempDir()
	image := filepath.Join(directory, "install.iso")
	if err := os.WriteFile(image, []byte("iso"), 0o000); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(image, link); err != nil {
		t.Fatal(err)
	}
	facts, err := (Observer{}).Observe(context.Background(), preflight.Target{Host: "controller", Local: true, Pool: image, DiskOnly: true})
	parent, _ := (Observer{}).Observe(context.Background(), preflight.Target{Host: "controller", Local: true, Pool: directory, DiskOnly: true})
	if err != nil || !facts.DiskKnown || facts.DiskIdentity != parent.DiskIdentity {
		t.Fatalf("an existing installer image left its filesystem unobserved: %+v %v", facts, err)
	}
	if facts, _ := (Observer{}).Observe(context.Background(), preflight.Target{Host: "controller", Local: true, Pool: link, DiskOnly: true}); facts.DiskKnown {
		t.Fatalf("a symbolic link was followed: %+v", facts)
	}
}
