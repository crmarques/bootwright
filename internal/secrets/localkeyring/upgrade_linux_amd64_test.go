//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type legacyFixture struct {
	store      *integrationStore
	root       string
	selector   legacySelector
	index      legacyIndex
	values     map[string]string
	files      map[string][]byte
	historical string
}

func newLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	h := newUninitializedIntegrationStore(t)
	root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
	for _, directory := range []string{"identities", "indexes", "keys", "ledgers", "parts"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	id := func(prefix string, value byte) string { return prefix + strings.Repeat(string(value), 32) }
	initialKeyID, activeKeyID := id("key-", '1'), id("key-", '2')
	initialKey, activeKey := bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x42}, 32)
	defer clear(initialKey)
	defer clear(activeKey)
	selector := legacySelector{SelectorVersion: 1, ContextID: h.context.ID, Selection: legacySelection(), Generation: id("gen-", '3')}
	index := legacyIndex{FormatVersion: 1, Algorithm: algorithm, Selector: selector, ActiveKey: activeKeyID, Keys: []secretstore.Key{{ID: initialKeyID, State: "retired", Seals: 3}, {ID: activeKeyID, State: "active", Seals: 4}}, Versions: []legacyVersion{}, Current: []secretstore.Current{}, Bindings: []secretstore.Binding{}}
	fixture := legacyFixture{store: h, root: root, selector: selector, values: map[string]string{}, files: map[string][]byte{}, historical: id("ver-", '9')}
	write := func(path string, value any, maximum int) {
		t.Helper()
		data, err := encodeCanonical(value, maximum)
		if err != nil {
			t.Fatal(err)
		}
		fixture.files[path] = slices.Clone(data)
		if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		version, blob, name, source, material string
	}{
		{id("ver-", '4'), id("blob-", '4'), "credential", "contextStore", "current-secret-canary"},
		{id("ver-", '5'), id("blob-", '5'), "credential", "contextStore", "bound-prior-secret-canary"},
		{id("ver-", '6'), id("blob-", '6'), "file", "file", "bound-file-secret-canary"},
	} {
		declaration := declaration(item.name, "opaque", item.source)
		version := legacyVersion{ID: item.version, Declaration: declaration, Parts: []storedPart{{Part: secrets.ValuePart, BlobID: item.blob, KeyID: activeKeyID, Generation: selector.Generation, Size: len(item.material)}}}
		index.Versions = append(index.Versions, version)
		fixture.values[item.version] = item.material
		part := version.Parts[0]
		aad, _ := json.Marshal(legacyPartAAD{Domain: "bootwright.secret.part.v1", FormatVersion: 1, Algorithm: algorithm, ContextID: h.context.ID, Selection: selector.Selection, Generation: part.Generation, KeyID: activeKeyID, BlobID: part.BlobID, DeclarationFingerprint: declaration.Fingerprint, Name: declaration.Name, Type: declaration.Type, Source: declaration.Source, Version: item.version, Part: secrets.ValuePart})
		write("parts/"+item.blob+".bin", legacyFixtureEnvelope(t, activeKey, []byte(item.material), aad, "part", activeKeyID, item.blob, byte(len(index.Versions))), partMaximum)
		write("identities/"+item.version+".json", identityRecord{FormatVersion: 1, ContextID: h.context.ID, ID: item.version}, selectorMaximum)
	}
	index.Current = []secretstore.Current{{Name: "credential", Version: index.Versions[0].ID}}
	index.Bindings = []secretstore.Binding{{ID: id("bind-", '7'), Versions: []string{index.Versions[1].ID, index.Versions[2].ID}}}
	write("identities/"+index.Bindings[0].ID+".json", identityRecord{FormatVersion: 1, ContextID: h.context.ID, ID: index.Bindings[0].ID}, selectorMaximum)
	write("identities/"+fixture.historical+".json", identityRecord{FormatVersion: 1, ContextID: h.context.ID, ID: fixture.historical}, selectorMaximum)
	for n, key := range [][]byte{initialKey, activeKey} {
		reference := index.Keys[n]
		path := "keys/" + reference.ID + ".bin"
		fixture.files[path] = slices.Clone(key)
		if err := os.WriteFile(filepath.Join(root, path), key, 0600); err != nil {
			t.Fatal(err)
		}
		authentication, _ := json.Marshal(legacyLedgerAuthentication{Domain: "bootwright.secret.ledger.v1", FormatVersion: 1, Algorithm: "HMAC-SHA256", ContextID: h.context.ID, Selection: selector.Selection, KeyID: reference.ID, Seals: reference.Seals})
		write("ledgers/"+reference.ID+".json", sealLedger{FormatVersion: 1, KeyID: reference.ID, Seals: reference.Seals, MAC: legacyMAC(key, "bootwright.secret.ledger.mac-key.v1", authentication)}, ledgerMaximum)
	}
	marker := legacyInitialization{FormatVersion: 1, ContextID: h.context.ID, Selection: selector.Selection, Attempts: []legacyInitializationAttempt{{AttemptID: id("init-", 'a'), KeyID: initialKeyID, Generation: id("gen-", '1')}}, MACKeyID: initialKeyID}
	authentication, _ := json.Marshal(legacyInitializationAuthentication{Domain: "bootwright.secret.initialization.v1", FormatVersion: 1, ContextID: h.context.ID, Selection: selector.Selection, Attempts: marker.Attempts, MACKeyID: initialKeyID})
	marker.MAC = legacyMAC(initialKey, "bootwright.secret.initialization.mac-key.v1", authentication)
	write(initializationPath, marker, selectorMaximum)
	plaintext, err := encodeCanonical(index, indexMaximum)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plaintext)
	aad, _ := json.Marshal(legacyIndexAAD{Domain: "bootwright.secret.index.v1", FormatVersion: 1, Algorithm: algorithm, ContextID: h.context.ID, Selection: selector.Selection, Generation: selector.Generation, KeyID: activeKeyID, BlobID: selector.Generation})
	write("indexes/"+selector.Generation+".bin", legacyFixtureEnvelope(t, activeKey, plaintext, aad, "index", activeKeyID, selector.Generation, 4), indexMaximum)
	write(legacySelectorPath, selector, selectorMaximum)
	fixture.index = index
	t.Cleanup(func() {
		for _, data := range fixture.files {
			clear(data)
		}
	})
	return fixture
}

func legacyFixtureEnvelope(t *testing.T, key, plaintext, aad []byte, purpose, keyID, blobID string, nonceByte byte) envelope {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{nonceByte}, gcmNonceSize)
	return envelope{FormatVersion: 1, Algorithm: algorithm, Purpose: purpose, KeyID: keyID, BlobID: blobID, Nonce: rawBase64.EncodeToString(nonce), Ciphertext: rawBase64.EncodeToString(aead.Seal(nil, nonce, plaintext, aad))}
}

func (f legacyFixture) initialize(wrap func(secretstore.Area) secretstore.Area) error {
	return f.store.workspace.MutateSecrets(context.Background(), f.store.context, func(area secretstore.Area) error {
		if wrap != nil {
			area = wrap(area)
		}
		session, err := f.store.implementation.Initialize(context.Background(), f.store.context, area, nil)
		if err != nil {
			return err
		}
		return session.Close()
	})
}

func (f legacyFixture) assertLegacyUnchanged(t *testing.T) {
	t.Helper()
	for path, expected := range f.files {
		actual, err := os.ReadFile(filepath.Join(f.root, path))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("upgrade changed source artifact before commit: %s (%v)", path, err)
		}
		clear(actual)
	}
}

func (f legacyFixture) assertUpgraded(t *testing.T) {
	t.Helper()
	if err := f.store.view(func(session secretstore.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Keys) != 1 || snapshot.Keys[0].ID == f.index.ActiveKey || snapshot.Keys[0].Seals != uint64(len(f.values)+1) || !slices.Equal(snapshot.Current, f.index.Current) || len(snapshot.Bindings) != 1 || snapshot.Bindings[0].ID != f.index.Bindings[0].ID || !slices.Equal(snapshot.Bindings[0].Versions, f.index.Bindings[0].Versions) || snapshot.RetainedArtifacts != 0 || snapshot.CleanupRequired {
			t.Fatal("upgrade changed logical references or failed to retire artifacts", snapshot)
		}
		for _, version := range snapshot.Versions {
			material, err := session.Read(context.Background(), version.ID)
			if err != nil {
				return err
			}
			value, _ := material.Part(secrets.ValuePart)
			material.Clear()
			if string(value) != f.values[version.ID] {
				t.Fatal("upgrade changed current or bound secret material")
			}
			clear(value)
			data, err := json.Marshal(version.Declaration)
			if err != nil || bytes.Contains(data, []byte("/input/")) || bytes.Contains(data, []byte(`"generation"`)) || bytes.Contains(data, []byte(`"files"`)) {
				t.Fatal("upgraded declaration retained acquisition details", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{legacySelectorPath, initializationPath, upgradePath, "indexes", "ledgers"} {
		if _, err := os.Stat(filepath.Join(f.root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy artifact remained after durable upgrade: %s (%v)", path, err)
		}
	}
	for path, expected := range f.files {
		if strings.HasPrefix(path, "identities/") {
			actual, err := os.ReadFile(filepath.Join(f.root, path))
			if err != nil || !bytes.Equal(actual, expected) {
				t.Fatal("upgrade lost identity reservation history", path, err)
			}
		}
	}
}

func TestLegacyUpgradePreservesCurrentBindingsAndIdentityHistory(t *testing.T) {
	f := newLegacyFixture(t)
	before, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatal(err)
	}
	err = f.store.workspace.ReadSecrets(context.Background(), f.store.context, func(area secretstore.Area) error {
		_, _, err := secretstore.ReadSelector(context.Background(), area, f.store.context.ID)
		return err
	})
	if err == nil {
		t.Fatal("ordinary inspection accepted legacy state without explicit upgrade")
	}
	f.assertLegacyUnchanged(t)
	after, err := os.ReadDir(f.root)
	if err != nil || len(before) != len(after) {
		t.Fatal("read-only legacy inspection created state", err)
	}
	if err := f.initialize(nil); err != nil {
		t.Fatal(err)
	}
	f.assertUpgraded(t)
	if err := f.initialize(nil); err != nil {
		t.Fatal("repeated encryption init was not idempotent", err)
	}
	f.assertUpgraded(t)
}

type upgradeFaultArea struct {
	secretstore.Area
	failAt        int
	effects       int
	partial       bool
	afterCommit   bool
	cleanupKey    bool
	cleanupFailed bool
	pending       bool
}

func (a *upgradeFaultArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	a.effects++
	if a.effects == a.failAt {
		if a.partial {
			if err := a.Area.WriteExclusive(ctx, path, data[:len(data)/2]); err != nil {
				return err
			}
		}
		return errSimulatedCrash
	}
	return a.Area.WriteExclusive(ctx, path, data)
}

func (a *upgradeFaultArea) Replace(ctx context.Context, path string, data, expected []byte) (secretstore.Outcome, error) {
	a.effects++
	if a.effects == a.failAt && !a.afterCommit {
		if a.pending {
			if err := a.Area.WriteExclusive(ctx, "pending-"+strings.Repeat("e", 32), data); err != nil {
				return secretstore.NotCommitted, err
			}
		}
		return secretstore.NotCommitted, errSimulatedCrash
	}
	outcome, err := a.Area.Replace(ctx, path, data, expected)
	if err == nil && a.effects == a.failAt {
		return secretstore.Uncertain, errSimulatedCrash
	}
	return outcome, err
}

type upgradeQuotaArea struct {
	secretstore.Area
	maximumBytes int64
	maximumFiles int
}

func (a *upgradeQuotaArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	if strings.HasSuffix(path, ".bin") {
		return a.Area.WriteExclusive(ctx, path, data)
	}
	var size int64
	count := 0
	for _, directory := range []string{"keys", "parts"} {
		entries, err := a.Area.Entries(ctx, directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name, ".key") || strings.HasSuffix(entry.Name, ".usage.json") || strings.HasSuffix(entry.Name, ".enc") {
				size += entry.Size
				count++
			}
		}
	}
	if count+1 > a.maximumFiles || size+int64(len(data)) > a.maximumBytes {
		return secretstore.Failure("store.limit", "synthetic physical upgrade capacity is full")
	}
	return a.Area.WriteExclusive(ctx, path, data)
}

func TestLegacyUpgradeReclaimsAttributedAttemptsBeforeRetryCapacity(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "partial-part"
		if pending {
			name = "complete-pending-metadata"
		}
		t.Run(name, func(t *testing.T) {
			f := newLegacyFixture(t)
			point := 6
			if pending {
				point = 7
			}
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				return &upgradeFaultArea{Area: area, failAt: point, partial: true, pending: pending}
			})
			if err == nil {
				t.Fatal("injected partial upgrade succeeded")
			}
			var occupied int64
			for _, directory := range []string{"keys", "parts"} {
				entries, err := os.ReadDir(filepath.Join(f.root, directory))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasSuffix(entry.Name(), ".bin") {
						continue
					}
					info, err := entry.Info()
					if err != nil {
						t.Fatal(err)
					}
					occupied += info.Size()
				}
			}
			// Enough for one complete attempt, but neither the remaining failed
			// files nor their bytes fit alongside another attempt.
			quota := occupied + 512
			err = f.initialize(func(area secretstore.Area) secretstore.Area {
				return &upgradeQuotaArea{Area: area, maximumFiles: 5, maximumBytes: quota}
			})
			if err != nil {
				t.Fatal("attributed retry artifacts consumed the upgrade capacity", err)
			}
			f.assertUpgraded(t)
		})
	}
}

type upgradePruneFaultArea struct {
	secretstore.Area
	calls  int
	failAt int
}

func (a *upgradePruneFaultArea) PruneUnpublished(ctx context.Context, guards []secretstore.RecordExpectation, paths []string) error {
	a.calls++
	if a.calls == a.failAt {
		if err := a.Area.PruneUnpublished(ctx, guards, paths[:1]); err != nil {
			return err
		}
		return errSimulatedCrash
	}
	return a.Area.PruneUnpublished(ctx, guards, paths)
}

func TestLegacyUpgradeCleanupInterruptionRetainsSourceAndAttribution(t *testing.T) {
	for _, point := range []int{1, 2} {
		t.Run(string(rune('a'+point)), func(t *testing.T) {
			f := newLegacyFixture(t)
			if err := f.initialize(func(area secretstore.Area) secretstore.Area {
				return &upgradeFaultArea{Area: area, failAt: 7, pending: true}
			}); err == nil {
				t.Fatal("injected pending metadata interruption succeeded")
			}
			var fault *upgradePruneFaultArea
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				fault = &upgradePruneFaultArea{Area: area, failAt: point}
				return fault
			})
			if err == nil || fault == nil || fault.calls != point {
				t.Fatal("injected unpublished cleanup interruption succeeded", err)
			}
			f.assertLegacyUnchanged(t)
			if _, err := os.Stat(filepath.Join(f.root, selectorPath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("interrupted cleanup published metadata", err)
			}
			if err := f.initialize(nil); err != nil {
				t.Fatal("cleanup lost surviving attempt attribution", err)
			}
			f.assertUpgraded(t)
		})
	}
}

func TestLegacyUpgradeKeepsUnattributedTemporaryBeforeCommit(t *testing.T) {
	f := newLegacyFixture(t)
	if err := f.initialize(func(area secretstore.Area) secretstore.Area { return &upgradeFaultArea{Area: area, failAt: 4} }); err == nil {
		t.Fatal("injected part interruption succeeded")
	}
	path := "pending-" + strings.Repeat("f", 32)
	data := []byte("unattributed-partial-publication")
	f.files[path] = data
	if err := os.WriteFile(filepath.Join(f.root, path), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.initialize(func(area secretstore.Area) secretstore.Area { return &upgradeFaultArea{Area: area, failAt: 7} }); err == nil {
		t.Fatal("injected metadata interruption succeeded")
	}
	f.assertLegacyUnchanged(t)
}

type upgradeSubstitutionArea struct {
	secretstore.Area
	root   string
	target string
	swap   bool
}

func (a *upgradeSubstitutionArea) Replace(ctx context.Context, path string, data, expected []byte) (secretstore.Outcome, error) {
	if path == selectorPath && !a.swap {
		original, err := os.ReadFile(filepath.Join(a.root, a.target))
		if err != nil {
			return secretstore.NotCommitted, err
		}
		defer clear(original)
		temporary := filepath.Join(a.root, "replacement")
		if err := os.WriteFile(temporary, original, 0600); err != nil {
			return secretstore.NotCommitted, err
		}
		if err := os.Rename(temporary, filepath.Join(a.root, a.target)); err != nil {
			return secretstore.NotCommitted, err
		}
		a.swap = true
	}
	return a.Area.Replace(ctx, path, data, expected)
}

func TestLegacyUpgradeRejectsSourceAndIntentInodeSubstitutionAtCommit(t *testing.T) {
	for _, target := range []string{"selector", "index", "key", "ledger", "initialization", "part", "identity", "upgrade-intent"} {
		t.Run(target, func(t *testing.T) {
			f := newLegacyFixture(t)
			path := map[string]string{
				"selector":       legacySelectorPath,
				"index":          "indexes/" + f.selector.Generation + ".bin",
				"key":            "keys/" + f.index.ActiveKey + ".bin",
				"ledger":         "ledgers/" + f.index.ActiveKey + ".json",
				"initialization": initializationPath,
				"part":           "parts/" + f.index.Versions[0].Parts[0].BlobID + ".bin",
				"identity":       "identities/" + f.historical + ".json",
				"upgrade-intent": upgradePath,
			}[target]
			var guard *upgradeSubstitutionArea
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				guard = &upgradeSubstitutionArea{Area: area, root: f.root, target: path}
				return guard
			})
			if err == nil || guard == nil || !guard.swap {
				t.Fatal("same-byte source substitution reached metadata publication", err)
			}
			if _, err := os.Stat(filepath.Join(f.root, selectorPath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("source substitution published upgraded metadata", err)
			}
			f.assertLegacyUnchanged(t)
		})
	}
}

func (a *upgradeFaultArea) Prune(ctx context.Context, expected []byte, paths []string) error {
	if a.cleanupKey && !a.cleanupFailed {
		for _, path := range paths {
			if strings.HasPrefix(path, "keys/") && strings.HasSuffix(path, ".bin") {
				if err := a.Area.Prune(ctx, expected, []string{path}); err != nil {
					return err
				}
				a.cleanupFailed = true
				return errSimulatedCrash
			}
		}
		return errors.New("cleanup did not include a retired key")
	}
	return a.Area.Prune(ctx, expected, paths)
}

func TestLegacyUpgradeRetriesWithoutChangingSourceBeforeCommit(t *testing.T) {
	for failAt := 1; failAt <= 7; failAt++ {
		t.Run(string(rune('a'+failAt)), func(t *testing.T) {
			f := newLegacyFixture(t)
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				return &upgradeFaultArea{Area: area, failAt: failAt, partial: true}
			})
			if err == nil {
				t.Fatal("injected upgrade interruption succeeded")
			}
			f.assertLegacyUnchanged(t)
			if _, err := os.Stat(filepath.Join(f.root, selectorPath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("interrupted upgrade published metadata", err)
			}
			if err := f.initialize(nil); err != nil {
				t.Fatal("upgrade failed to resume attributed artifacts", err)
			}
			f.assertUpgraded(t)
		})
	}
}

func TestLegacyUpgradeResumesCommittedMetadataWithoutRetiredKeys(t *testing.T) {
	for _, failure := range []string{"metadata-durability", "retired-key-cleanup"} {
		t.Run(failure, func(t *testing.T) {
			f := newLegacyFixture(t)
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				if failure == "metadata-durability" {
					return &upgradeFaultArea{Area: area, failAt: 7, afterCommit: true}
				}
				return &upgradeFaultArea{Area: area, cleanupKey: true}
			})
			if err == nil {
				t.Fatal("injected committed upgrade interruption succeeded")
			}
			if _, err := os.Stat(filepath.Join(f.root, selectorPath)); err != nil {
				t.Fatal("committed metadata was lost", err)
			}
			if err := f.initialize(nil); err != nil {
				t.Fatal("committed upgrade still depended on old state", err)
			}
			f.assertUpgraded(t)
		})
	}
}

func TestLegacyUpgradeRejectsTamperingBeforeAnyEffects(t *testing.T) {
	for _, target := range []string{"selector", "index", "key", "ledger", "initialization", "part", "identity"} {
		t.Run(target, func(t *testing.T) {
			f := newLegacyFixture(t)
			path := map[string]string{
				"selector":       legacySelectorPath,
				"index":          "indexes/" + f.selector.Generation + ".bin",
				"key":            "keys/" + f.index.ActiveKey + ".bin",
				"ledger":         "ledgers/" + f.index.ActiveKey + ".json",
				"initialization": initializationPath,
				"part":           "parts/" + f.index.Versions[0].Parts[0].BlobID + ".bin",
				"identity":       "identities/" + f.historical + ".json",
			}[target]
			data := slices.Clone(f.files[path])
			data[len(data)/2] ^= 1
			if err := os.WriteFile(filepath.Join(f.root, path), data, 0600); err != nil {
				t.Fatal(err)
			}
			f.files[path] = data
			var guard *upgradeFaultArea
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				guard = &upgradeFaultArea{Area: area}
				return guard
			})
			if err == nil || guard != nil && guard.effects != 0 || strings.Contains(err.Error(), "secret-canary") {
				t.Fatal("corrupt legacy state reached upgrade effects or disclosure", err)
			}
			f.assertLegacyUnchanged(t)
		})
	}
}

func TestLegacyUpgradeRejectsUnattributedArtifactsAndChangedIntent(t *testing.T) {
	for _, target := range []string{"unattributed-key", "intent-mac", "source-selection"} {
		t.Run(target, func(t *testing.T) {
			f := newLegacyFixture(t)
			if target == "unattributed-key" {
				if err := os.WriteFile(filepath.Join(f.root, keyPath("key-"+strings.Repeat("f", 32))), bytes.Repeat([]byte{1}, 32), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.initialize(func(area secretstore.Area) secretstore.Area { return &upgradeFaultArea{Area: area, failAt: 2} }); err == nil {
					t.Fatal("upgrade intent interruption succeeded")
				}
				path := upgradePath
				if target == "source-selection" {
					path = legacySelectorPath
				}
				data, err := os.ReadFile(filepath.Join(f.root, path))
				if err != nil {
					t.Fatal(err)
				}
				data[len(data)/2] ^= 1
				if err := os.WriteFile(filepath.Join(f.root, path), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var guard *upgradeFaultArea
			err := f.initialize(func(area secretstore.Area) secretstore.Area {
				guard = &upgradeFaultArea{Area: area}
				return guard
			})
			if err == nil || guard != nil && guard.effects != 0 {
				t.Fatal("unattributed upgrade state reached effects", err)
			}
		})
	}
}
