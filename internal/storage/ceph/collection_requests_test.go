package ceph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const collectionRequests = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/ceph_request_goldens.json"

// The collection's tests run ceph_native.validate over every request golden Go
// freezes, so a field Go adds that the validator refuses fails a Python test
// instead of a host. The collection runs from a copy that holds no internal
// directory, so the goldens travel as one document there; run with -update
// twice after a request golden changes, the second run bundling the first's
// output.
func TestTheCollectionValidatesEveryRequestGoldenGoFreezes(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "request-*.golden"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no request golden: %v", err)
	}
	requests := map[string]json.RawMessage{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		requests[strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "request-"), ".golden")] = compact.Bytes()
	}
	data, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *update {
		if err := os.WriteFile(collectionRequests, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(collectionRequests)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, data) {
		t.Errorf("%s differs from testdata/request-*.golden; rerun with -update if the change is intended, then run the collection's Ceph tests", collectionRequests)
	}
}
