package localstore

import (
	"bytes"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
)

func versionFor(id, name string, sequence int) storedVersion {
	return storedVersion{ID: id, Sequence: sequence, Declaration: secrets.VersionDeclaration{Name: name, Type: "opaque", Source: "contextStore", Fingerprint: "f"}, Parts: []storedPart{}}
}

// A record written before ordinals existed carries no sequence key. Reading it
// must produce exactly the bytes that were stored, or the store is unreadable.
func TestVersionWithoutSequenceRoundTripsCanonically(t *testing.T) {
	stored := storedVersion{ID: "ver-a", Declaration: secrets.VersionDeclaration{Name: "token", Type: "opaque", Source: "contextStore", Fingerprint: "f"}, Parts: []storedPart{}}
	data, err := encodeCanonical(stored, indexMaximum)
	if err != nil {
		t.Fatalf("legacy version could not be encoded: %v", err)
	}
	if bytes.Contains(data, []byte("sequence")) {
		t.Fatalf("absent ordinal was encoded: %s", data)
	}
	var decoded storedVersion
	if err := decodeCanonical(data, indexMaximum, maxIndexItems, &decoded); err != nil {
		t.Fatalf("legacy version was rejected: %v", err)
	}
	if decoded.Sequence != 0 {
		t.Fatalf("absent ordinal decoded as %d", decoded.Sequence)
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

func TestAssignLegacySequencesIsDeterministicAndIdempotent(t *testing.T) {
	index := indexRecord{Versions: []storedVersion{
		versionFor("ver-a", "token", 0),
		versionFor("ver-b", "token", 0),
		versionFor("ver-c", "other", 0),
	}}
	assignLegacySequences(&index)
	for position, want := range []int{1, 2, 1} {
		if index.Versions[position].Sequence != want {
			t.Fatalf("version %d numbered %d, want %d", position, index.Versions[position].Sequence, want)
		}
	}
	assignLegacySequences(&index)
	for position, want := range []int{1, 2, 1} {
		if index.Versions[position].Sequence != want {
			t.Fatalf("repeat assignment renumbered version %d to %d", position, index.Versions[position].Sequence)
		}
	}
}

// A store part way through adoption keeps the ordinals it already has and only
// numbers what is still missing one.
func TestAssignLegacySequencesContinuesPastNumberedVersions(t *testing.T) {
	index := indexRecord{Versions: []storedVersion{
		versionFor("ver-a", "token", 4),
		versionFor("ver-b", "token", 0),
	}}
	assignLegacySequences(&index)
	if index.Versions[0].Sequence != 4 || index.Versions[1].Sequence != 5 {
		t.Fatalf("numbered version was disturbed: %d %d", index.Versions[0].Sequence, index.Versions[1].Sequence)
	}
}
