package clusterobjects

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const collectionGoldens = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens"

type ownershipVector struct {
	Operation    string `json:"operation"`
	Block        string `json:"block"`
	Ref          Ref    `json:"ref"`
	OwnerMark    string `json:"ownerMark"`
	FieldManager string `json:"fieldManager"`
}

type ownershipVectors struct {
	ClockSkewSeconds int               `json:"clockSkewSeconds"`
	Vectors          []ownershipVector `json:"vectors"`
}

func matchesCollectionFixture(t *testing.T, name string, data []byte) {
	t.Helper()
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
		t.Errorf("%s differs; rerun with -update if the change is intended, then run the collection's cluster object tests:\ngolden %s\ngot    %s", path, want, data)
	}
}

// The collection's engine is the live copy of the owner mark, the field
// manager and the creation-time skew; its tests read these vectors, so a rule
// either side changes fails a test on the other.
func TestTheOwnerMarkFieldManagerAndSkewVectorsAreTheCollectionsToo(t *testing.T) {
	refs := []struct {
		operation, block string
		ref              Ref
	}{
		{"operation-1", "network-1", Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: "operators", Name: "settings", Namespaced: true}},
		{"operation-1", "network-1", Ref{APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: "openshift-operators", Name: "operator", Namespaced: true}},
		{"operation-2", "block-9", Ref{APIVersion: "config.openshift.io/v1", Kind: "Proxy", Name: "cluster"}},
		{"operation-2", "block-9", Ref{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "operators", Name: "controller", Namespaced: true}},
	}
	document := ownershipVectors{ClockSkewSeconds: int(ClockSkew.Seconds())}
	for _, entry := range refs {
		document.Vectors = append(document.Vectors, ownershipVector{entry.operation, entry.block, entry.ref,
			OwnerMark(entry.operation, entry.block, entry.ref), FieldManager(entry.operation, entry.block)})
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	matchesCollectionFixture(t, "cluster_objects_ownership.json", data)
}

// The collection's engine runs the program vocabulary Go freezes, so a step or
// check kind Go adds that the engine refuses fails a collection test, not a
// live cluster. The collection runs from a copy with no internal directory, so
// the golden travels as one compact document; run with -update twice after
// testdata/vocabulary.golden changes.
func TestTheCollectionRunsTheVocabularyGoFreezes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "vocabulary.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatal(err)
	}
	matchesCollectionFixture(t, "cluster_objects_vocabulary.json", compact.Bytes())
}
