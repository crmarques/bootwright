//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const firstKubeconfig = "apiVersion: v1\nkind: Config\nusers:\n- name: admin\n"

func (h *integrationStore) produce(block string, outputs map[string]string) []secretstore.Produced {
	h.t.Helper()
	inputs := []secretstore.ProducedInput{}
	for _, name := range slices.Sorted(maps.Keys(outputs)) {
		inputs = append(inputs, secretstore.ProducedInput{Name: name, Material: opaque(outputs[name])})
	}
	var produced []secretstore.Produced
	if err := h.mutate(func(session secretstore.StoreSession) error {
		var err error
		produced, err = session.Produce(context.Background(), block, inputs)
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	for _, input := range inputs {
		input.Material.Clear()
	}
	return produced
}

func (h *integrationStore) readProduced(block, name string) (string, bool) {
	h.t.Helper()
	var value string
	var found bool
	if err := h.view(func(session secretstore.StoreSession) error {
		read, exists, err := session.ReadProduced(context.Background(), block, name)
		if err != nil {
			return err
		}
		material := read.Material
		defer material.Clear()
		found = exists
		if exists {
			value = materialValue(h.t, material, secrets.ValuePart)
		}
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return value, found
}

func (h *integrationStore) snapshot() secretstore.Snapshot {
	h.t.Helper()
	var snapshot secretstore.Snapshot
	if err := h.view(func(session secretstore.StoreSession) error {
		var err error
		snapshot, err = session.Inspect(context.Background())
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	return snapshot
}

func (h *integrationStore) storeRecord() []byte {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.secretsRoot(), secretstore.RecordPath))
	if err != nil {
		h.t.Fatal(err)
	}
	return data
}

func (h *integrationStore) partFiles() int {
	h.t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.secretsRoot(), "parts"))
	if err != nil {
		h.t.Fatal(err)
	}
	return len(entries)
}

func TestAProducedEntryRoundTripsAndReadsBack(t *testing.T) {
	h := newIntegrationStore(t)
	produced := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	if len(produced) != 1 || produced[0].Block != producedBlock || produced[0].Name != producedName || produced[0].Version == "" {
		t.Fatalf("produced = %+v", produced)
	}
	if value, found := h.readProduced(producedBlock, producedName); !found || value != firstKubeconfig {
		t.Fatalf("read back %q (%t)", value, found)
	}
	if _, found := h.readProduced(producedBlock, "other"); found {
		t.Fatal("an entry never produced was found")
	}
	snapshot := h.snapshot()
	if !slices.Equal(snapshot.Produced, produced) || len(snapshot.Current) != 0 || len(snapshot.Bindings) != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Versions) != 1 || snapshot.Versions[0].Declaration != producedDeclaration(producedBlock, producedName) {
		t.Fatalf("versions = %+v", snapshot.Versions)
	}
}

func TestAnUnchangedRecaptureAddsNoVersion(t *testing.T) {
	h := newIntegrationStore(t)
	first := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	record, parts := h.storeRecord(), h.partFiles()
	again := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	if !slices.Equal(again, first) || !bytes.Equal(h.storeRecord(), record) || h.partFiles() != parts {
		t.Fatalf("an unchanged recapture published: %+v after %+v", again, first)
	}
}

func TestARecaptureReplacesAndCollectsTheOldVersion(t *testing.T) {
	h := newIntegrationStore(t)
	first := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig, "password": "first"})
	parts := h.partFiles()
	second := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig + "# rewritten\n"})
	if len(second) != 1 || second[0].Version == first[0].Version || second[0].Name != producedName {
		t.Fatalf("the recapture kept the old version: %+v after %+v", second, first)
	}
	if value, _ := h.readProduced(producedBlock, producedName); value != firstKubeconfig+"# rewritten\n" {
		t.Fatalf("read back %q", value)
	}
	if value, found := h.readProduced(producedBlock, "password"); !found || value != "first" {
		t.Fatal("the block's entry of another name was not kept")
	}
	snapshot := h.snapshot()
	if len(snapshot.Versions) != 2 || slices.ContainsFunc(snapshot.Versions, func(version secretstore.Version) bool { return version.ID == first[0].Version }) {
		t.Fatalf("the replaced version was not collected: %+v", snapshot.Versions)
	}
	if h.partFiles() != parts {
		t.Fatalf("the replaced part was not collected: %d parts, want %d", h.partFiles(), parts)
	}
}

// Every other publication rewrites the index from what it holds, so a
// produced entry must be carried through each of them.
func TestAProducedEntrySurvivesAnUnrelatedPublication(t *testing.T) {
	h := newIntegrationStore(t)
	produced := h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	token := opaque("token-value")
	defer token.Clear()
	var binding secretstore.Binding
	steps := []func(secretstore.StoreSession) error{
		func(session secretstore.StoreSession) error {
			_, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("token", "opaque", "contextStore"), Material: token}})
			return err
		},
		func(session secretstore.StoreSession) error {
			snapshot, err := session.Inspect(context.Background())
			if err != nil {
				return err
			}
			binding, err = session.Bind(context.Background(), []secretstore.BoundInput{{Declaration: declaration("token", "opaque", "contextStore"), Material: token, Version: snapshot.Current[0].Version}})
			return err
		},
		func(session secretstore.StoreSession) error {
			_, err := session.Delete(context.Background(), "token")
			return err
		},
		func(session secretstore.StoreSession) error {
			_, err := session.Release(context.Background(), binding.ID)
			return err
		},
		func(session secretstore.StoreSession) error {
			_, err := session.Produce(context.Background(), "cluster-install-other", []secretstore.ProducedInput{{Name: producedName, Material: opaque("other")}})
			return err
		},
	}
	for index, step := range steps {
		if err := h.mutate(step); err != nil {
			t.Fatalf("step %d: %v", index, err)
		}
		if value, found := h.readProduced(producedBlock, producedName); !found || value != firstKubeconfig {
			t.Fatalf("step %d dropped the produced entry", index)
		}
	}
	if snapshot := h.snapshot(); len(snapshot.Produced) != 2 || snapshot.Produced[1] != produced[0] || len(snapshot.Versions) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestRotationReencryptsProducedMaterial(t *testing.T) {
	h := newIntegrationStore(t)
	h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	before := h.snapshot().ActiveKey
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.Rotate(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := h.snapshot()
	if snapshot.ActiveKey == before || len(snapshot.Keys) != 1 || len(snapshot.Produced) != 1 {
		t.Fatalf("the rotation left %+v", snapshot)
	}
	if value, found := h.readProduced(producedBlock, producedName); !found || value != firstKubeconfig {
		t.Fatal("the rotation lost the produced material")
	}
}

func TestWithdrawRemovesEveryEntryAndCollectsItsParts(t *testing.T) {
	h := newIntegrationStore(t)
	token := opaque("token-value")
	defer token.Clear()
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("token", "opaque", "contextStore"), Material: token}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	parts := h.partFiles()
	h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	h.produce("cluster-install-other", map[string]string{producedName: "other"})
	withdraw := func() bool {
		var withdrawn bool
		if err := h.mutate(func(session secretstore.StoreSession) error {
			var err error
			withdrawn, err = session.Withdraw(context.Background())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return withdrawn
	}
	if !withdraw() {
		t.Fatal("a withdrawal over two entries reported none")
	}
	snapshot := h.snapshot()
	if snapshot.Produced == nil || len(snapshot.Produced) != 0 || len(snapshot.Versions) != 1 || len(snapshot.Current) != 1 || h.partFiles() != parts {
		t.Fatalf("the withdrawal left %+v with %d parts, want %d", snapshot, h.partFiles(), parts)
	}
	if _, found := h.readProduced(producedBlock, producedName); found {
		t.Fatal("a withdrawn entry is still read")
	}
	record := h.storeRecord()
	if withdraw() || !bytes.Equal(h.storeRecord(), record) {
		t.Fatal("a withdrawal over no entry published")
	}
}

func TestAProducedEntryTakesNoOrdinalFromTheSecretSeries(t *testing.T) {
	h := newIntegrationStore(t)
	h.produce(producedBlock, map[string]string{producedName: firstKubeconfig})
	value := opaque("a Secret named like the output")
	defer value.Clear()
	var versions []secretstore.Version
	if err := h.mutate(func(session secretstore.StoreSession) error {
		var err error
		versions, err = session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration(producedName, "opaque", "contextStore"), Material: value}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Sequence != 1 {
		t.Fatalf("the Secret's first version is %+v", versions)
	}
}
