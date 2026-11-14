package localkeyring

import (
	"bytes"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func versionFor(id, name string, sequence int) storedVersion {
	return storedVersion{ID: id, Sequence: sequence, Declaration: secrets.VersionDeclaration{Name: name, Type: "opaque", Source: "contextStore", Fingerprint: "f"}, Parts: []storedPart{}}
}

func TestEveryStoredVersionCarriesItsOrdinal(t *testing.T) {
	data, err := encodeCanonical(storedVersion{ID: "ver-a", Declaration: secrets.VersionDeclaration{Name: "token", Type: "opaque", Source: "contextStore", Fingerprint: "f"}, Parts: []storedPart{}}, indexMaximum)
	if err != nil {
		t.Fatalf("version could not be encoded: %v", err)
	}
	if !bytes.Contains(data, []byte(`"sequence":0`)) {
		t.Fatalf("ordinal was omitted from the record: %s", data)
	}
}

func TestVersionWithSequenceRoundTripsCanonically(t *testing.T) {
	data, err := encodeCanonical(versionFor("ver-a", "token", 3), indexMaximum)
	if err != nil {
		t.Fatalf("version could not be encoded: %v", err)
	}
	var decoded storedVersion
	if err := decodeCanonical(data, indexMaximum, maxIndexItems, &decoded); err != nil {
		t.Fatalf("version was rejected: %v", err)
	}
	if decoded.Sequence != 3 {
		t.Fatalf("ordinal lost: %d", decoded.Sequence)
	}
}

// Each secret counts from one on its own, and a deletion never renumbers what
// remains, so an ordinal keeps naming the same material.
func TestNextSequenceContinuesPerSecretSeries(t *testing.T) {
	index := indexRecord{Versions: []storedVersion{
		versionFor("ver-a", "token", 1),
		versionFor("ver-b", "token", 2),
		versionFor("ver-c", "other", 1),
	}}
	if got := nextSequence(index, "token"); got != 3 {
		t.Fatalf("token series did not continue: %d", got)
	}
	if got := nextSequence(index, "other"); got != 2 {
		t.Fatalf("other series did not continue: %d", got)
	}
	if got := nextSequence(index, "fresh"); got != 1 {
		t.Fatalf("new secret did not start at one: %d", got)
	}
	index.Versions = []storedVersion{versionFor("ver-b", "token", 2)}
	if got := nextSequence(index, "token"); got != 3 {
		t.Fatalf("deleting the first version renumbered the series: %d", got)
	}
}

func TestIndexWithAnUnnumberedVersionIsRefused(t *testing.T) {
	selector := secretstore.Selector{SelectorVersion: secretstore.RecordVersion, Context: "lab", Backend: (&Implementation{}).Backend(), Generation: fixedID("gen-", 1)}
	declaration := secrets.Declaration{Name: "token", Type: "opaque", Source: "contextStore"}
	declaration.Fingerprint = declarationFingerprint(declaration)
	version := storedVersion{ID: fixedID("ver-", 1), Declaration: declaration.Summary(), Parts: []storedPart{{Part: secrets.ValuePart, BlobID: fixedID("blob-", 1), KeyID: fixedID("key-", 1), Generation: selector.Generation, Size: 1}}}
	index := indexRecord{
		FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: fixedID("key-", 1),
		Keys:     []storedKey{{ID: fixedID("key-", 1)}},
		Versions: []storedVersion{version},
		Current:  []secretstore.Current{{Name: "token", Version: version.ID}}, Bindings: []secretstore.Binding{}, Produced: []secretstore.Produced{},
	}
	index.Versions[0].Sequence = 1
	if err := validateIndex(index, selector); err != nil {
		t.Fatalf("the numbered fixture is refused for another reason: %v", err)
	}
	index.Versions[0].Sequence = 0
	if err := validateIndex(index, selector); err == nil || err.Error() != "invalid secret version" {
		t.Fatalf("a version without an ordinal was not refused as one: %v", err)
	}
}
