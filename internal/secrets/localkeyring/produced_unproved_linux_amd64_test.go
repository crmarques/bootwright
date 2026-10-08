//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (h *integrationStore) produceInput(block string, input secretstore.ProducedInput) []secretstore.Produced {
	h.t.Helper()
	defer input.Material.Clear()
	var produced []secretstore.Produced
	if err := h.mutate(func(session secretstore.StoreSession) error {
		var err error
		produced, err = session.Produce(context.Background(), block, []secretstore.ProducedInput{input})
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	return produced
}

func (h *integrationStore) readMark(block, name string) (string, bool) {
	h.t.Helper()
	var value string
	unproved := false
	if err := h.view(func(session secretstore.StoreSession) error {
		read, exists, err := session.ReadProduced(context.Background(), block, name)
		if err != nil || !exists {
			h.t.Fatalf("the entry is unreadable or absent: %v", err)
		}
		defer read.Material.Clear()
		value, unproved = materialValue(h.t, read.Material, "value"), read.Unproved
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return value, unproved
}

// A copy kept from an effect never proved is an entry like any other, marked
// unproved in the store's own record, which each session reads back. It
// never replaces an entry custody already holds, and a proved capture of the
// same bytes clears the mark (D124).
func TestAnUnprovedEntryIsMarkedUntilAProvedCaptureClearsIt(t *testing.T) {
	h := newIntegrationStore(t)
	kept := h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig), Unproved: true})
	if len(kept) != 1 || !kept[0].Unproved {
		t.Fatalf("produced = %+v, want one unproved entry", kept)
	}
	if value, unproved := h.readMark(producedBlock, producedName); value != firstKubeconfig || !unproved {
		t.Fatalf("read back %q, unproved %t", value, unproved)
	}
	record := h.storeRecord()
	again := h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig + "# other\n"), Unproved: true})
	if len(again) != 1 || again[0] != kept[0] || !bytes.Equal(h.storeRecord(), record) {
		t.Fatalf("an unproved copy replaced the entry custody holds: %+v after %+v", again, kept)
	}
	proved := h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig)})
	if len(proved) != 1 || proved[0].Unproved || proved[0].Version != kept[0].Version {
		t.Fatalf("the proved capture = %+v, want the same version without the mark", proved)
	}
	if value, unproved := h.readMark(producedBlock, producedName); value != firstKubeconfig || unproved {
		t.Fatalf("read back %q, unproved %t after the proved capture", value, unproved)
	}
	record = h.storeRecord()
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig), Unproved: true})
	if !bytes.Equal(h.storeRecord(), record) {
		t.Fatal("an unproved copy marked a proved entry")
	}
}
