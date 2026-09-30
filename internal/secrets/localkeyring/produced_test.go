package localkeyring

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	producedBlock = "cluster-install-sno"
	producedName  = "kubeconfig"
)

func summary(name, kind, source, path string) secrets.VersionDeclaration {
	declaration := secrets.Declaration{Name: name, Type: kind, Source: source, Origin: "/input/environment.yaml", Document: 1}
	declaration.Files.Path = path
	declaration.Fingerprint = declarationFingerprint(declaration)
	return declaration.Summary()
}

// formatFourIndex reaches every member format four holds: two keys, a current
// version, a version only a binding holds and a produced entry, each version
// sealed under one of those keys.
func formatFourIndex() (indexRecord, secretstore.Selector) {
	selector := secretstore.Selector{SelectorVersion: secretstore.RecordVersion, Context: "lab", Backend: New().Backend(), Generation: fixedID("gen-", 3)}
	part := func(blob, key, size int) []storedPart {
		return []storedPart{{Part: secrets.ValuePart, BlobID: fixedID("blob-", blob), KeyID: fixedID("key-", key), Generation: fixedID("gen-", 2), Size: size}}
	}
	return indexRecord{
		FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: fixedID("key-", 2),
		Keys: []storedKey{{ID: fixedID("key-", 1), Seals: 4}, {ID: fixedID("key-", 2), Seals: 3}},
		Versions: []storedVersion{
			{ID: fixedID("ver-", 1), Sequence: 2, Declaration: summary("registry-token", "token", "contextStore", ""), Parts: part(1, 2, 24)},
			{ID: fixedID("ver-", 2), Sequence: 1, Declaration: summary("pull-secret", "opaque", "file", "secrets/pull-secret"), Parts: part(2, 1, 12)},
			{ID: fixedID("ver-", 3), Sequence: 1, Declaration: producedDeclaration(producedBlock, producedName), Parts: part(3, 2, 2048)},
		},
		Current:  []secretstore.Current{{Name: "registry-token", Version: fixedID("ver-", 1)}},
		Bindings: []secretstore.Binding{{ID: fixedID("bind-", 1), Versions: []string{fixedID("ver-", 2)}}},
		Produced: []secretstore.Produced{{Block: producedBlock, Name: producedName, Version: fixedID("ver-", 3)}},
	}, selector
}

func decodeIndex(t *testing.T, data []byte, selector secretstore.Selector) (indexRecord, error) {
	t.Helper()
	var index indexRecord
	if err := decodeCanonical(data, indexMaximum, maxIndexItems, &index); err != nil {
		return indexRecord{}, err
	}
	index.FormatVersion, index.Algorithm, index.Selector = formatVersion, algorithm, selector
	return index, validateIndex(index, selector)
}

func TestTheMetadataGoldenPinsFormatFour(t *testing.T) {
	index, selector := formatFourIndex()
	if err := validateIndex(index, selector); err != nil {
		t.Fatalf("the format four fixture is refused: %v", err)
	}
	data, err := encodeCanonical(index, indexMaximum)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "metadata-v4", data, true)
	decoded, err := decodeIndex(t, data, selector)
	if err != nil {
		t.Fatalf("the golden does not decode back into a valid index: %v", err)
	}
	again, err := encodeCanonical(decoded, indexMaximum)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("the golden does not round-trip: %v", err)
	}
}

// A produced version is reached through the one entry it was captured for and
// through nothing else, and the entry keys it by the block and name its
// fingerprint covers.
func TestProducedVersionsAreRefusedOutsideTheirEntry(t *testing.T) {
	produced := fixedID("ver-", 3)
	for _, test := range []struct {
		name   string
		change func(*indexRecord)
		want   string
	}{
		{"reached from current", func(index *indexRecord) {
			index.Current = append([]secretstore.Current{{Name: producedName, Version: produced}}, index.Current...)
		}, "invalid current mapping"},
		{"reached from a binding", func(index *indexRecord) {
			index.Bindings[0].Versions = append(index.Bindings[0].Versions, produced)
		}, "invalid bound version"},
		{"named by two entries", func(index *indexRecord) {
			index.Produced = append(index.Produced, secretstore.Produced{Block: producedBlock, Name: "other", Version: produced})
		}, "invalid produced entry"},
		{"the same entry twice", func(index *indexRecord) {
			index.Produced = append(index.Produced, index.Produced[0])
		}, "invalid produced entry"},
		{"dangling", func(index *indexRecord) {
			index.Produced[0].Version = fixedID("ver-", 9)
		}, "invalid produced entry"},
		{"left without its entry", func(index *indexRecord) {
			index.Produced = []secretstore.Produced{}
		}, "unreferenced logical version"},
		{"keyed by another block", func(index *indexRecord) {
			index.Produced[0].Block = "cluster-install-other"
		}, "invalid produced entry"},
		{"a wrong fingerprint", func(index *indexRecord) {
			index.Versions[2].Declaration.Fingerprint = strings.Repeat("0", 64)
		}, "invalid produced entry"},
		{"a wrong source", func(index *indexRecord) {
			index.Versions[2].Declaration.Source = "contextStore"
		}, "invalid produced entry"},
		{"a second ordinal", func(index *indexRecord) {
			index.Versions[2].Sequence = 2
		}, "invalid produced entry"},
		{"an empty part", func(index *indexRecord) {
			index.Versions[2].Parts[0].Size = 0
		}, "invalid produced entry"},
		{"out of order", func(index *indexRecord) {
			index.Versions = append(index.Versions, storedVersion{ID: fixedID("ver-", 4), Sequence: 1, Declaration: producedDeclaration("cluster-install-aaa", producedName), Parts: []storedPart{{Part: secrets.ValuePart, BlobID: fixedID("blob-", 4), KeyID: fixedID("key-", 2), Generation: fixedID("gen-", 2), Size: 1}}})
			index.Produced = append(index.Produced, secretstore.Produced{Block: "cluster-install-aaa", Name: producedName, Version: fixedID("ver-", 4)})
		}, "invalid produced entry order"},
		{"absent", func(index *indexRecord) { index.Produced = nil }, "invalid index header"},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, selector := formatFourIndex()
			test.change(&index)
			if err := validateIndex(index, selector); err == nil || err.Error() != test.want {
				t.Fatalf("validateIndex = %v, want %s", err, test.want)
			}
		})
	}
	index, selector := formatFourIndex()
	data, err := encodeCanonical(index, indexMaximum)
	if err != nil {
		t.Fatal(err)
	}
	member := data[bytes.Index(data, []byte(`,"produced":[`)):bytes.LastIndexByte(data, '}')]
	for name, replacement := range map[string][]byte{"null": []byte(`,"produced":null`), "absent": nil} {
		if _, err := decodeIndex(t, bytes.Replace(data, member, replacement, 1), selector); err == nil {
			t.Fatalf("a %s produced member was admitted", name)
		}
	}
}

// A Secret's ordinals count its own versions only, so a produced version of the
// same name neither takes nor shifts one.
func TestAProducedVersionTakesNoOrdinalFromASecretOfTheSameName(t *testing.T) {
	index, _ := formatFourIndex()
	if got := nextSequence(index, producedName); got != 1 {
		t.Fatalf("a Secret named like a produced entry starts at %d", got)
	}
	index.Versions = append(index.Versions, storedVersion{ID: fixedID("ver-", 4), Sequence: 1, Declaration: summary(producedName, "opaque", "contextStore", "")})
	if got := nextSequence(index, producedName); got != 2 {
		t.Fatalf("that Secret's second version is %d", got)
	}
}

// A store this build's catalog does not carry, the format three keyring
// included, refuses before any session with its own identity and the way out.
func TestTheCatalogRefusesTheV3Identity(t *testing.T) {
	_, err := secretstore.NewCatalog(New()).Reopen("local-keyring-v3")
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "secret.store.implementation" || !strings.Contains(found[0].Message, "local-keyring-v3") ||
		found[0].Remediation != "destroy this context's effects with the Bootwright build that created it, then run bootwright context delete --name <context> --purge and create the context again" {
		t.Fatalf("the v3 identity was refused as %+v", found)
	}
	if implementation, err := secretstore.NewCatalog(New()).Reopen("local-keyring-v4"); err != nil || implementation == nil {
		t.Fatalf("the v4 identity was refused: %v", err)
	}
}
