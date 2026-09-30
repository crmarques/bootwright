package localkeyring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestCanonicalEncodedSizeMatchesJSONEncoding(t *testing.T) {
	allBytes := make([]byte, 256)
	for index := range allBytes {
		allBytes[index] = byte(index)
	}
	invalidUTF8 := string(allBytes)
	values := []any{
		envelope{FormatVersion: formatVersion, Algorithm: algorithm, Purpose: invalidUTF8, KeyID: "key", BlobID: "blob", Nonce: "nonce", Ciphertext: "ciphertext\u2028\u2029"},
		secretstore.Selector{SelectorVersion: formatVersion, Context: "ctx", Backend: New().Backend(), Generation: "generation"},
		indexRecord{FormatVersion: formatVersion, Algorithm: algorithm, Keys: nil, Versions: []storedVersion{}, Current: nil, Bindings: []secretstore.Binding{}},
		initializationRecord{FormatVersion: formatVersion, Context: "ctx", Selection: New().Backend(), Attempts: []initializationAttempt{}, MACKeyID: "", MAC: ""},
	}
	for index, value := range values {
		marshaled, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("value %d marshal: %v", index, err)
		}
		want := len(marshaled) + 1
		got, err := canonicalEncodedSize(value, want)
		if err != nil || got != want {
			t.Fatalf("value %d size: got %d, want %d: %v", index, got, want, err)
		}
		encoded, err := encodeCanonical(value, want)
		if err != nil || !bytes.Equal(encoded, append(marshaled, '\n')) {
			t.Fatalf("value %d encoding mismatch: %v", index, err)
		}
		if _, err := canonicalEncodedSize(value, want-1); limitFailureCode(err) != "secret.store.limit" {
			t.Fatalf("value %d accepted an undersized limit: %v", index, err)
		}
	}
	if _, err := canonicalEncodedSize([]int{0, 0}, 4); limitFailureCode(err) != "secret.store.limit" {
		t.Fatalf("slice separator overflow reset the size bound: %v", err)
	}
	type twoFields struct {
		First  int `json:"a"`
		Second int `json:"b"`
	}
	if _, err := canonicalEncodedSize(twoFields{}, 8); limitFailureCode(err) != "secret.store.limit" {
		t.Fatalf("struct separator overflow reset the size bound: %v", err)
	}
}

func TestSealPreflightsEnvelopeAndRequiresAES256(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	plaintext := bytes.Repeat([]byte{0x24}, 257)
	additional := []byte("authenticated metadata")
	wantSize, err := sealedEnvelopeSize(len(plaintext), "part", "key-id", "blob-id", partMaximum)
	if err != nil {
		t.Fatal(err)
	}
	random := &sequenceReader{}
	sealed, err := seal(key, plaintext, additional, "part", "key-id", "blob-id", random, wantSize)
	if err != nil || len(sealed) != wantSize || random.calls != 1 {
		t.Fatalf("exact envelope: size=%d want=%d random-calls=%d error=%v", len(sealed), wantSize, random.calls, err)
	}
	opened, err := openEnvelope(sealed, key, additional, "part", "key-id", "blob-id", wantSize)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open exact envelope: %v", err)
	}
	clear(opened)

	tooLargeRandom := &sequenceReader{}
	if _, err := seal(key, append(plaintext, 0), additional, "part", "key-id", "blob-id", tooLargeRandom, wantSize); limitFailureCode(err) != "secret.store.limit" || tooLargeRandom.calls != 0 {
		t.Fatalf("oversized envelope was not rejected before randomness: calls=%d error=%v", tooLargeRandom.calls, err)
	}
	for _, length := range []int{16, 24} {
		shortKey := bytes.Repeat([]byte{0x42}, length)
		unused := &sequenceReader{}
		if _, err := seal(shortKey, plaintext, additional, "part", "key-id", "blob-id", unused, partMaximum); limitFailureCode(err) != "secret.store.crypto" || unused.calls != 0 {
			t.Fatalf("seal accepted %d-byte AES key: calls=%d error=%v", length, unused.calls, err)
		}
		if _, err := openEnvelope(sealed, shortKey, additional, "part", "key-id", "blob-id", partMaximum); limitFailureCode(err) != "secret.store.crypto" {
			t.Fatalf("open accepted %d-byte AES key: %v", length, err)
		}
	}
}

func TestIndexStructuralPreflightEnforcesTypedArrayLimits(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		suffix  string
		element string
		limit   int
	}{
		{name: "keys", prefix: `{"keys":[`, suffix: `]}`, element: `{}`, limit: maxPhysicalItems},
		{name: "versions", prefix: `{"versions":[`, suffix: `]}`, element: `{}`, limit: secrets.MaxVersions},
		{name: "current", prefix: `{"current":[`, suffix: `]}`, element: `{}`, limit: secrets.MaxVersions},
		{name: "bindings", prefix: `{"bindings":[`, suffix: `]}`, element: `{}`, limit: maxBindings},
		{name: "version parts", prefix: `{"versions":[{"parts":[`, suffix: `]}]}`, element: `{}`, limit: 2},
		{name: "binding versions", prefix: `{"bindings":[{"versions":[`, suffix: `]}]}`, element: `"id"`, limit: secrets.MaxVersions},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			exact := repeatedJSONArray(test.prefix, test.suffix, test.element, test.limit)
			if !boundedIndexJSON(exact) {
				t.Fatalf("exact limit %d rejected", test.limit)
			}
			over := repeatedJSONArray(test.prefix, test.suffix, test.element, test.limit+1)
			if boundedIndexJSON(over) {
				t.Fatalf("limit+1 (%d) accepted", test.limit+1)
			}
		})
	}
	for _, duplicate := range []string{
		`{"versions":[],"versions":[]}`,
		`{"versions":[{"parts":[],"parts":[]}]}`,
		`{"bindings":[{"versions":[],"versions":[]}]}`,
		`{"Versions":[]}`,
		`{"vErSiOnS":[]}`,
		`{"Vers\u0069ons":[]}`,
		`{"versions":[],"vers\u0069ons":[]}`,
		`{"versions":[{"Parts":[]}]}`,
		`{"versions":[{"Pa\u0072ts":[]}]}`,
		`{"bindings":[{"Versions":[]}]}`,
	} {
		if boundedIndexJSON([]byte(duplicate)) {
			t.Fatalf("duplicate bounded array was accepted: %s", duplicate)
		}
	}

	over := repeatedJSONArray(`{"formatVersion":99,"versions":[`, `]}`, `{}`, secrets.MaxVersions+1)
	target := indexRecord{FormatVersion: 77}
	if err := decodeCanonical(over, indexMaximum, maxIndexItems, &target); err == nil || target.FormatVersion != 77 {
		t.Fatalf("oversized versions array reached typed decoding: formatVersion=%d error=%v", target.FormatVersion, err)
	}

	highSANCount := 262146
	highSAN := repeatedJSONArray(`{"versions":[{"declaration":{"generation":{"dnsNames":[`, `]}}}]}`, `""`, highSANCount)
	if len(highSAN) > indexMaximum || bytes.Count(highSAN, []byte{','}) <= 262144 || !boundedJSON(highSAN, maxIndexItems, indexMaximum) || !boundedIndexJSON(highSAN) {
		t.Fatalf("byte-bounded high-SAN index was rejected: bytes=%d commas=%d", len(highSAN), bytes.Count(highSAN, []byte{','}))
	}
}

func TestMaximumLogicalIndexRoundTrips(t *testing.T) {
	selector := secretstore.Selector{SelectorVersion: formatVersion, Context: "example", Backend: New().Backend(), Generation: fixedID("gen-", 1)}
	keyID := fixedID("key-", 1)
	index := indexRecord{
		FormatVersion: formatVersion,
		Algorithm:     algorithm,
		Selector:      selector,
		ActiveKey:     keyID,
		Keys:          []storedKey{{ID: keyID, Seals: 1}},
		Versions:      make([]storedVersion, 0, secrets.MaxVersions),
		Current:       make([]secretstore.Current, 0, secrets.MaxVersions),
		Bindings:      []secretstore.Binding{},
	}
	for item := 0; item < secrets.MaxVersions; item++ {
		name := fmt.Sprintf("secret-%04x", item)
		versionID := fixedID("ver-", item+1)
		declaration := secrets.Declaration{Name: name, Type: "opaque", Source: "contextStore"}
		declaration.Fingerprint = declarationFingerprint(declaration)
		index.Versions = append(index.Versions, storedVersion{
			ID:          versionID,
			Sequence:    1,
			Declaration: declaration.Summary(),
			Parts: []storedPart{{
				Part:       secrets.ValuePart,
				BlobID:     fixedID("blob-", item+1),
				KeyID:      keyID,
				Generation: selector.Generation,
				Size:       0,
			}},
		})
		index.Current = append(index.Current, secretstore.Current{Name: name, Version: versionID})
	}
	if err := validateIndex(index, selector); err != nil {
		t.Fatalf("valid maximum index: %v", err)
	}
	encoded, err := encodeCanonical(index, indexMaximum)
	if err != nil {
		t.Fatal(err)
	}
	if commas := bytes.Count(encoded, []byte{','}); commas <= 32768 {
		t.Fatalf("maximum index did not exercise the former structural ceiling: %d commas", commas)
	}
	var decoded indexRecord
	if err := decodeCanonical(encoded, indexMaximum, maxIndexItems, &decoded); err != nil {
		t.Fatalf("maximum index decode: %v", err)
	}
	decoded.FormatVersion, decoded.Algorithm, decoded.Selector = formatVersion, algorithm, selector
	if err := validateIndex(decoded, selector); err != nil {
		t.Fatalf("decoded maximum index: %v", err)
	}
}

func TestPublishRejectsOversizedProjectedEnvelopeBeforeEffects(t *testing.T) {
	selection := New().Backend()
	contextName := "example"
	keyID := fixedID("key-", 1)
	oldGeneration := fixedID("gen-", 99)
	versionID := fixedID("ver-", 1)
	longDNSName := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63)
	declaration := secrets.Declaration{
		Name:   "oversized",
		Type:   "tlsCertificate",
		Source: "generated",
		Generation: secrets.Generation{
			CommonName:   "oversized.example",
			DNSNames:     repeatedStrings(longDNSName, 35000),
			ValidityDays: 365,
		},
	}
	declaration.Fingerprint = declarationFingerprint(declaration)
	next := indexRecord{
		FormatVersion: formatVersion,
		Algorithm:     algorithm,
		Selector:      secretstore.Selector{SelectorVersion: formatVersion, Context: contextName, Backend: selection, Generation: oldGeneration},
		ActiveKey:     keyID,
		Keys:          []storedKey{{ID: keyID}},
		Versions: []storedVersion{{
			ID:          versionID,
			Sequence:    1,
			Declaration: declaration.Summary(),
			Parts: []storedPart{
				{Part: secrets.CertificatePart, Size: 1},
				{Part: secrets.PrivateKeyPart, Size: 1},
			},
		}},
		Current:  []secretstore.Current{{Name: declaration.Name, Version: versionID}},
		Bindings: []secretstore.Binding{},
	}
	ids := make([]string, secrets.MaxVersions)
	for n := range ids {
		ids[n] = fixedID("ver-", n+1)
	}
	for n := 0; n < 45; n++ {
		next.Bindings = append(next.Bindings, secretstore.Binding{ID: fixedID("bind-", n+1), Versions: ids})
	}
	projected := cloneIndex(next)
	projected.Selector.Generation = fixedID("gen-", 1)
	for part := range projected.Versions[0].Parts {
		projected.Versions[0].Parts[part].BlobID = fixedID("blob-", part+2)
		projected.Versions[0].Parts[part].KeyID = keyID
		projected.Versions[0].Parts[part].Generation = projected.Selector.Generation
	}
	indexSize, err := canonicalEncodedSize(projected, indexMaximum)
	if err != nil {
		t.Fatalf("test index must fit the plaintext bound: %v", err)
	}
	if _, err := metadataEncodedSize(indexSize, projected.Selector, keyID); limitFailureCode(err) != "secret.store.limit" {
		t.Fatalf("test index must exceed only the projected envelope bound: size=%d error=%v", indexSize, err)
	}

	area := initializedLimitArea(map[string][]byte{})
	random := &sequenceReader{}
	session := &session{
		implementation: NewWithOptions(Options{Random: random}),
		context:        secretstore.Context{Name: contextName, Mode: "ready", Revision: fixedID("rev-", 1)},
		area:           area,
		selector:       next.Selector,
		selectorData:   []byte("prior selector"),
		index:          cloneIndex(next),
		keys:           map[string][]byte{},
	}
	plain := []plainPart{
		{version: versionID, part: secrets.CertificatePart, data: []byte("c")},
		{version: versionID, part: secrets.PrivateKeyPart, data: []byte("k")},
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	err = session.publish(context.Background(), &next, plain, publicationKey{id: keyID, value: key, fresh: true}, nil)
	if limitFailureCode(err) != "secret.store.limit" || area.mutations != 0 || session.mutated || random.calls != 3 {
		t.Fatalf("projected envelope preflight: mutations=%d session-mutated=%t random-calls=%d error=%v", area.mutations, session.mutated, random.calls, err)
	}
}

func TestSealReservationCeilingIncludesAbandonedReservations(t *testing.T) {
	contextName := "example"
	keyID := fixedID("key-", 1)
	selection := New().Backend()
	key := bytes.Repeat([]byte{0x42}, 32)
	initial, err := encodeLedger(contextName, selection, key, keyID, maxSeals-2)
	if err != nil {
		t.Fatal(err)
	}
	area := initializedLimitArea(map[string][]byte{ledgerPath(keyID): initial})
	newSession := func() *session {
		return &session{
			context:  secretstore.Context{Name: contextName},
			area:     area,
			selector: secretstore.Selector{Backend: selection},
			index:    indexRecord{Keys: []storedKey{{ID: keyID, Seals: maxSeals - 2}}},
		}
	}

	first := newSession()
	reservation, err := first.prepareSeals(context.Background(), publicationKey{id: keyID, value: key}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.commitSeals(context.Background(), reservation); err != nil {
		t.Fatal(err)
	}
	// Simulate an interruption after the reservation but before selector
	// publication. A reopened session still has the older index floor.
	second := newSession()
	reservation, err = second.prepareSeals(context.Background(), publicationKey{id: keyID, value: key}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.commitSeals(context.Background(), reservation); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(area.files[ledgerPath(keyID)])
	mutations := area.mutations
	if _, err := newSession().prepareSeals(context.Background(), publicationKey{id: keyID, value: key}, 1); limitFailureCode(err) != "secret.store.limit" {
		t.Fatalf("seal ceiling was exceeded: %v", err)
	}
	if area.mutations != mutations || !bytes.Equal(before, area.files[ledgerPath(keyID)]) {
		t.Fatal("failed ceiling reservation changed durable state")
	}
	ledger, err := decodeLedger(before, contextName, selection, key, keyID, maxSeals-2)
	if err != nil || ledger.Seals != maxSeals {
		t.Fatalf("reserved ledger: seals=%d error=%v", ledger.Seals, err)
	}
}

func TestUniqueIDUsesExactlySixteenCollisionAttempts(t *testing.T) {
	t.Run("exhausted", func(t *testing.T) {
		random := &sequenceReader{}
		_, err := NewWithOptions(Options{Random: random}).uniqueID("ver-", func(string) bool { return true })
		if limitFailureCode(err) != "secret.store.limit" || random.calls != 16 {
			t.Fatalf("collision attempts=%d error=%v", random.calls, err)
		}
	})
	t.Run("sixteenth succeeds", func(t *testing.T) {
		random := &sequenceReader{}
		checks := 0
		id, err := NewWithOptions(Options{Random: random}).uniqueID("ver-", func(string) bool {
			checks++
			return checks < 16
		})
		if err != nil || !validID(id, "ver-") || random.calls != 16 || checks != 16 {
			t.Fatalf("id=%q random-calls=%d checks=%d error=%v", id, random.calls, checks, err)
		}
	})
}

type sequenceReader struct{ calls int }

func (r *sequenceReader) Read(value []byte) (int, error) {
	r.calls++
	clear(value)
	if len(value) != 0 {
		value[len(value)-1] = byte(r.calls)
	}
	return len(value), nil
}

func repeatedJSONArray(prefix, suffix, element string, count int) []byte {
	var result strings.Builder
	result.Grow(len(prefix) + len(suffix) + count*(len(element)+1))
	result.WriteString(prefix)
	for item := 0; item < count; item++ {
		if item != 0 {
			result.WriteByte(',')
		}
		result.WriteString(element)
	}
	result.WriteString(suffix)
	return []byte(result.String())
}

func repeatedStrings(value string, count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func fixedID(prefix string, value int) string { return fmt.Sprintf("%s%032x", prefix, value) }

func limitFailureCode(err error) string {
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) == 0 {
		return ""
	}
	return diagnostics[0].Code
}
