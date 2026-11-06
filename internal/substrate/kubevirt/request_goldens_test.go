package kubevirt

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the request goldens the collection's tests read")

const collectionGoldens = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens"

func matchesCollectionGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	if bytes.ContainsAny(data, "\n\t") || len(data) == 0 {
		t.Fatalf("%s: a canonical request is one line of compact JSON: %q", name, data)
	}
	data = append(data, '\n')
	path := filepath.Join(collectionGoldens, name)
	if *update {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, data) {
		t.Errorf("%s differs; rerun with -update if the change is intended, then run the collection's KubeVirt tests:\ngolden %s\ngot    %s", path, want, data)
	}
}

// The collection's tests drive the arm from these bytes, so a member either
// side renames fails a test on the other.
func TestTheCollectionDrivesTheArmFromTheRequestsGoFreezes(t *testing.T) {
	crossing, err := MachineRequests(labCatalog(providerWithNAD("net")), "controller", testContext)
	if err != nil || len(crossing) != 1 {
		t.Fatalf("requests = %v, %v", crossing, err)
	}
	machine, err := crossing[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesCollectionGolden(t, "kubevirt_machine_request.json", machine)
	host, err := hostRequest(t).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesCollectionGolden(t, "kubevirt_host_request.json", host)
}
