//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type upgradeCapacityArea struct {
	secretstore.Area
	entries map[string][]secretstore.Entry
}

func (a upgradeCapacityArea) Entries(ctx context.Context, path string) ([]secretstore.Entry, error) {
	return a.entries[path], ctx.Err()
}

func upgradeCapacityFixture(t *testing.T, withPart bool) (indexRecord, []byte, []byte, int64) {
	t.Helper()
	id := func(prefix string, value byte) string { return prefix + strings.Repeat(string(value), 32) }
	selector := secretstore.Selector{SelectorVersion: formatVersion, Context: "example", Backend: "local-keyring-v3", Generation: id("gen-", '2')}
	next := indexRecord{FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: id("key-", '3'), Keys: []storedKey{{ID: id("key-", '3'), Seals: 1}}, Versions: []storedVersion{}, Current: []secretstore.Current{}, Bindings: []secretstore.Binding{}, Legacy: true}
	key := bytes.Repeat([]byte{0x41}, 32)
	defer clear(key)
	outputBytes := int64(len(key))
	if withPart {
		version := storedVersion{ID: id("ver-", '4'), Declaration: declaration("credential", "opaque", "contextStore").Summary(), Parts: []storedPart{{Part: secrets.ValuePart, BlobID: id("blob-", '5'), KeyID: next.ActiveKey, Generation: selector.Generation, Size: 73}}}
		next.Versions = []storedVersion{version}
		next.Current = []secretstore.Current{{Name: version.Declaration.Name, Version: version.ID}}
		next.Keys[0].Seals++
		part := version.Parts[0]
		sealed, err := seal(key, make([]byte, part.Size), partAAD(selector.Context, selector.Backend, version, part), "part", part.KeyID, part.BlobID, bytes.NewReader(make([]byte, gcmNonceSize)), partMaximum)
		if err != nil {
			t.Fatal(err)
		}
		outputBytes += int64(len(sealed))
		clear(sealed)
	}
	plaintext, err := encodeCanonical(next, indexMaximum)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sealMetadata(key, plaintext, selector, next.ActiveKey, bytes.NewReader(make([]byte, gcmNonceSize)))
	if err != nil {
		t.Fatal(err)
	}
	outputBytes += int64(len(metadata))
	clear(metadata)
	usage, err := encodeLedger(selector.Context, selector.Backend, key, next.ActiveKey, next.Keys[0].Seals)
	if err != nil {
		t.Fatal(err)
	}
	outputBytes += int64(len(usage))
	journal := upgradeRecord{Version: formatVersion, Context: selector.Context, Backend: selector.Backend, SourceSelector: strings.Repeat("0", 64), Attempts: []upgradeAttempt{{KeyID: next.ActiveKey, Generation: selector.Generation}}, MAC: strings.Repeat("A", 43)}
	intent, err := encodeCanonical(journal, selectorMaximum)
	if err != nil {
		t.Fatal(err)
	}
	outputBytes += int64(len(intent))
	return next, plaintext, intent, outputBytes
}

func expectUpgradeHeadroom(t *testing.T, err error) {
	t.Helper()
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "secret.store.limit" || !strings.Contains(diagnostics[0].Message, "headroom") {
		t.Fatalf("expected bounded upgrade headroom refusal: %#v %v", diagnostics, err)
	}
}

func TestUpgradeCapacityMatchesActualEncodedOutput(t *testing.T) {
	for _, withPart := range []bool{false, true} {
		t.Run(fmt.Sprint(withPart), func(t *testing.T) {
			next, plaintext, intent, outputBytes := upgradeCapacityFixture(t, withPart)
			for _, excess := range []int64{0, 1} {
				area := upgradeCapacityArea{entries: map[string][]secretstore.Entry{
					"":      {{Name: "parts", Directory: true}},
					"parts": {{Name: "source.bin", Size: maxUpgradePhysicalBytes - outputBytes + excess}},
				}}
				err := preflightUpgradeCapacity(context.Background(), area, next, len(plaintext), intent, nil)
				if excess == 0 && err != nil {
					t.Fatal("exact migration peak was refused", err)
				}
				if excess != 0 {
					expectUpgradeHeadroom(t, err)
				}
			}
		})
	}
}

func TestUpgradeCapacityIncludesJournalReplacementPeak(t *testing.T) {
	next, plaintext, intent, outputBytes := upgradeCapacityFixture(t, false)
	oldJournal := upgradeRecord{Version: formatVersion, Context: next.Selector.Context, Backend: next.Selector.Backend, SourceSelector: strings.Repeat("0", 64), MAC: strings.Repeat("A", 43)}
	for index := range 16 {
		oldJournal.Attempts = append(oldJournal.Attempts, upgradeAttempt{KeyID: fmt.Sprintf("key-%032x", index), Generation: fmt.Sprintf("gen-%032x", index)})
	}
	prior, err := encodeCanonical(oldJournal, selectorMaximum)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(prior)) <= outputBytes-int64(len(intent)) {
		t.Fatal("fixture does not exercise the larger journal-replacement peak")
	}
	for _, excess := range []int64{0, 1} {
		area := upgradeCapacityArea{entries: map[string][]secretstore.Entry{
			"":      {{Name: upgradePath, Size: int64(len(prior))}, {Name: "parts", Directory: true}},
			"parts": {{Name: "source.bin", Size: maxUpgradePhysicalBytes - int64(len(prior)+len(intent)) + excess}},
		}}
		err := preflightUpgradeCapacity(context.Background(), area, next, len(plaintext), intent, prior)
		if excess == 0 && err != nil {
			t.Fatal("exact journal replacement peak was refused", err)
		}
		if excess != 0 {
			expectUpgradeHeadroom(t, err)
		}
	}
}

func TestUpgradeCapacityIncludesAllNewEntries(t *testing.T) {
	next, plaintext, intent, _ := upgradeCapacityFixture(t, true)
	// One new intent, one key, one usage ledger, one part and one metadata file.
	const newEntries = 5
	for _, excess := range []int{0, 1} {
		children := make([]secretstore.Entry, maxPhysicalItems-newEntries-1+excess)
		for index := range children {
			children[index] = secretstore.Entry{Name: fmt.Sprintf("artifact-%d", index)}
		}
		area := upgradeCapacityArea{entries: map[string][]secretstore.Entry{
			"":      {{Name: "parts", Directory: true}},
			"parts": children,
		}}
		err := preflightUpgradeCapacity(context.Background(), area, next, len(plaintext), intent, nil)
		if excess == 0 && err != nil {
			t.Fatal("exact migration entry peak was refused", err)
		}
		if excess != 0 {
			expectUpgradeHeadroom(t, err)
		}
	}
}

func TestLegacyUpgradeCapacityRefusesBeforeNewEffects(t *testing.T) {
	for _, headroom := range []int64{0, 1024} {
		t.Run(fmt.Sprint(headroom), func(t *testing.T) {
			fixture := newLegacyFixture(t)
			existing := int64(0)
			if err := filepath.WalkDir(fixture.root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				info, err := entry.Info()
				if err == nil {
					existing += info.Size()
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			remaining := maxUpgradePhysicalBytes - existing - headroom
			for index := 1; remaining > 0; index++ {
				path := filepath.Join(fixture.root, "parts", fmt.Sprintf("blob-%032x.bin", index))
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				size := min(remaining, 1<<20)
				err = file.Truncate(size)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatal("cannot create bounded sparse history", err, closeErr)
				}
				remaining -= size
			}
			var guard *upgradeFaultArea
			err := fixture.initialize(func(area secretstore.Area) secretstore.Area {
				guard = &upgradeFaultArea{Area: area}
				return guard
			})
			expectUpgradeHeadroom(t, err)
			if guard == nil || guard.effects != 0 {
				t.Fatal("insufficient whole-upgrade capacity consumed a new attempt")
			}
			fixture.assertLegacyUnchanged(t)
			for _, path := range []string{upgradePath, selectorPath} {
				if _, err := os.Stat(filepath.Join(fixture.root, path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("capacity refusal published new upgrade state")
				}
			}
		})
	}
}
