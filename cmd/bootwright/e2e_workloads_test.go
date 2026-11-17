//go:build linux && amd64

package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The acceptance rows rest on these workloads, so a signer, validation or
// comparison fault in one must fail here before it can pass a live run.
func TestE2EWorkloadsPassTheirUnitTests(t *testing.T) {
	for _, directory := range []string{
		filepath.Join("lab-5-nodes-ceph-networks", "app"),
		filepath.Join("lab-sno-fips-controller", "app"),
		filepath.Join("lab-sno-single-ceph-fdf", "app"),
	} {
		t.Run(directory, func(t *testing.T) {
			command := exec.Command(pinnedPython(t), "-B", "-m", "unittest", "discover", "-s", filepath.Join("..", "..", "e2e", directory), "-p", "*_test.py")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}
