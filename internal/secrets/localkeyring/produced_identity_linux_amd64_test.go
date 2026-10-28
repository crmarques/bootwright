//go:build linux && amd64

package localkeyring

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (h *integrationStore) readIdentity(block, name string) string {
	h.t.Helper()
	identity := ""
	if err := h.view(func(session secretstore.StoreSession) error {
		read, exists, err := session.ReadProduced(context.Background(), block, name)
		if err != nil || !exists {
			h.t.Fatalf("the entry is unreadable or absent: %v", err)
		}
		read.Material.Clear()
		identity = read.Identity
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return identity
}

// The identity a producer presents is kept in the store's own record beside
// its entry, which each session reads back; an entry recorded without one
// records it on an equal recapture, a different recapture records its own,
// and an identity that is not one bounded line of printable ASCII, or one an
// unproved copy carries, refuses before anything is published.
func TestAProducedEntryKeepsThePresentedIdentity(t *testing.T) {
	h := newIntegrationStore(t)
	const fsidA, fsidB = "cluster fsid 1b0d3c1e-1111-4a4a-8b8b-000000000001 as client.lab", "cluster fsid 1b0d3c1e-2222-4a4a-8b8b-000000000002 as client.lab"
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig)})
	if identity := h.readIdentity(producedBlock, producedName); identity != "" {
		t.Fatalf("an entry presented without identity reads %q", identity)
	}
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig), Identity: fsidA})
	if identity := h.readIdentity(producedBlock, producedName); identity != fsidA {
		t.Fatalf("an equal recapture recorded %q", identity)
	}
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig + "# rotated\n"), Identity: fsidB})
	if identity := h.readIdentity(producedBlock, producedName); identity != fsidB {
		t.Fatalf("a recapture recorded %q", identity)
	}
	record := h.storeRecord()
	for name, input := range map[string]secretstore.ProducedInput{
		"a line break":     {Name: producedName, Material: opaque("x"), Identity: "cluster fsid a\nas client.lab"},
		"past the bound":   {Name: producedName, Material: opaque("x"), Identity: strings.Repeat("a", maxProducedIdentity+1)},
		"an unproved copy": {Name: producedName, Material: opaque("x"), Identity: fsidA, Unproved: true},
	} {
		err := h.mutate(func(session secretstore.StoreSession) error {
			_, err := session.Produce(context.Background(), producedBlock, []secretstore.ProducedInput{input})
			return err
		})
		input.Material.Clear()
		if reported := diagnostics.Of(err); len(reported) == 0 || !strings.Contains(reported[0].Message, "identity") {
			t.Fatalf("an identity with %s = %v", name, err)
		}
	}
	if string(h.storeRecord()) != string(record) || h.readIdentity(producedBlock, producedName) != fsidB {
		t.Fatal("a refused identity changed the store")
	}
}

// A recapture that presents no identity keeps the one custody recorded, so no
// later capture can erase what a recapture must match.
func TestARecaptureWithoutIdentityKeepsTheRecordedOne(t *testing.T) {
	h := newIntegrationStore(t)
	const recorded = "cluster fsid 1b0d3c1e-1111-4a4a-8b8b-000000000001 as client.lab"
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig), Identity: recorded})
	h.produceInput(producedBlock, secretstore.ProducedInput{Name: producedName, Material: opaque(firstKubeconfig + "# rotated\n")})
	if value, _ := h.readProduced(producedBlock, producedName); value != firstKubeconfig+"# rotated\n" || h.readIdentity(producedBlock, producedName) != recorded {
		t.Fatalf("a recapture without identity left %q with identity %q", value, h.readIdentity(producedBlock, producedName))
	}
}
