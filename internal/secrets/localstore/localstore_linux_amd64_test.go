//go:build linux && amd64

package localstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type integrationStore struct {
	t              *testing.T
	workspace      *contextfs.Store
	root           string
	context        storage.Context
	implementation *Implementation
}

func newIntegrationStore(t *testing.T) *integrationStore {
	t.Helper()
	h := newUninitializedIntegrationStore(t)
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		session, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		status, err := session.Inspect(context.Background())
		if err != nil || len(status.Keys) != 1 || status.Keys[0].Seals != 1 || status.ActiveKey == "" {
			t.Fatalf("initialized status: %#v %v", status, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func newUninitializedIntegrationStore(t *testing.T) *integrationStore {
	t.Helper()
	base := t.TempDir()
	input := filepath.Join(base, "input")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "state")
	workspace := contextfs.New(contextfs.Options{Root: root})
	sources := desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\n"))}, Markers: []desiredstate.SourceFile{}}
	err := workspace.Transact(context.Background(), true, sources.Roots, func(tx contexts.Transaction) error {
		registry := tx.Registry()
		id, err := tx.Reserve(context.Background(), input)
		if err != nil {
			return err
		}
		if _, err := tx.MutationState(context.Background(), id); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), id, input, sources)
		if err != nil {
			return err
		}
		registry.Identities = append(registry.Identities, contexts.Identity{EnvironmentDirectory: input, ID: id})
		registry.Contexts = append(registry.Contexts, contexts.Record{Name: "example", ID: id, EnvironmentDirectory: input, Revision: revision, Mode: contexts.Active})
		registry.Current = "example"
		return tx.Commit(context.Background(), registry)
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := workspace.SecretContext(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	h := &integrationStore{t: t, workspace: workspace, root: root, context: snapshot.Context, implementation: New()}
	return h
}

func (h *integrationStore) mutate(callback func(storage.StoreSession) error) error {
	return h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		selector := readTestSelector(h.t, area)
		session, err := h.implementation.Open(context.Background(), h.context, area, selector, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		return callback(session)
	})
}

func (h *integrationStore) view(callback func(storage.StoreSession) error) error {
	return h.workspace.ReadSecrets(context.Background(), h.context, func(area storage.Area) error {
		selector := readTestSelector(h.t, area)
		session, err := h.implementation.Open(context.Background(), h.context, area, selector, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		return callback(session)
	})
}

func readTestSelector(t *testing.T, area storage.Area) storage.Selector {
	t.Helper()
	data, exists, err := area.ReadMutable(context.Background(), selectorPath, selectorMaximum)
	if err != nil || !exists {
		t.Fatalf("selector: %v", err)
	}
	var selector storage.Selector
	if err := decodeCanonical(data, selectorMaximum, 64, &selector); err != nil {
		t.Fatal(err)
	}
	return selector
}

func declaration(name, kind, source string) secrets.Declaration {
	d := secrets.Declaration{Name: name, Type: kind, Source: source, Origin: "/input/environment.yaml", Document: 1}
	if source == "file" {
		d.Files.Path = "secrets/" + name
	}
	d.Fingerprint = declarationFingerprint(d)
	return d
}

func opaque(value string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
}

func certificate(value string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(value)})
}

func materialValue(t *testing.T, material secrets.Material, part secrets.Part) string {
	t.Helper()
	value, exists := material.Part(part)
	defer clear(value)
	if !exists {
		t.Fatalf("missing material part %s", part)
	}
	return string(value)
}

func TestLocalStoreLifecycleRetainsBindingsAndRotatesLogicalVersions(t *testing.T) {
	h := newIntegrationStore(t)
	storedDeclaration := declaration("credential", "opaque", "contextStore")
	firstMaterial := opaque("first")
	defer firstMaterial.Clear()
	var first storage.Version
	if err := h.mutate(func(session storage.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: storedDeclaration, Material: firstMaterial}})
		if err == nil {
			first = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fileDeclaration := declaration("operator-ca", "caBundle", "file")
	fileMaterial := certificate("frozen certificate")
	defer fileMaterial.Clear()
	var binding storage.Binding
	if err := h.mutate(func(session storage.StoreSession) error {
		var err error
		binding, err = session.Bind(context.Background(), []storage.BoundInput{
			{Declaration: storedDeclaration, Version: first.ID, Material: firstMaterial},
			{Declaration: fileDeclaration, Material: fileMaterial},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	secondMaterial := opaque("second")
	defer secondMaterial.Clear()
	var second storage.Version
	if err := h.mutate(func(session storage.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: storedDeclaration, Material: secondMaterial}})
		if err == nil {
			second = versions[0]
		}
		return err
	}); err != nil || second.ID == first.ID {
		t.Fatalf("replacement: %v %#v", err, second)
	}
	var beforeRotation []string
	if err := h.view(func(session storage.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Versions) != 3 || len(snapshot.Current) != 1 || len(snapshot.Bindings) != 1 {
			t.Fatalf("retention snapshot: %#v", snapshot)
		}
		for _, version := range snapshot.Versions {
			beforeRotation = append(beforeRotation, version.ID)
		}
		bound, err := session.Reopen(context.Background(), binding.ID)
		if err != nil {
			return err
		}
		defer clearBound(bound)
		values := map[string]string{}
		for _, item := range bound {
			part := secrets.ValuePart
			if item.Version.Declaration.Type == "caBundle" {
				part = secrets.CertificatePart
			}
			values[item.Version.Declaration.Name] = materialValue(t, item.Material, part)
		}
		if values["credential"] != "first" || values["operator-ca"] != "frozen certificate" {
			t.Fatalf("reopened immutable values: %#v", values)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var rotated string
	if err := h.mutate(func(session storage.StoreSession) error {
		var err error
		rotated, err = session.Rotate(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(session storage.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		after := []string{}
		for _, version := range snapshot.Versions {
			after = append(after, version.ID)
		}
		if !slices.Equal(beforeRotation, after) || snapshot.ActiveKey != rotated || len(snapshot.Keys) != 2 || snapshot.RetainedArtifacts == 0 || !snapshot.CleanupRequired {
			t.Fatalf("rotation metadata: %#v", snapshot)
		}
		material, err := session.Read(context.Background(), second.ID)
		if err != nil {
			return err
		}
		defer material.Clear()
		if materialValue(t, material, secrets.ValuePart) != "second" {
			t.Fatal("rotated current material changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.mutate(func(session storage.StoreSession) error {
		changed, err := session.Delete(context.Background(), "credential")
		if err == nil && !changed {
			t.Fatal("current secret was not deleted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.mutate(func(session storage.StoreSession) error {
		changed, err := session.Release(context.Background(), binding.ID)
		if err == nil && !changed {
			t.Fatal("binding was not released")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(session storage.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Versions) != 0 || len(snapshot.Current) != 0 || len(snapshot.Bindings) != 0 || snapshot.RetainedArtifacts == 0 {
			t.Fatalf("logical collection: %#v", snapshot)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPutBatchAlwaysPublishesANewLogicalVersion(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("credential", "opaque", "contextStore")
	material := opaque("same bytes")
	defer material.Clear()
	var first, second storage.Version
	for _, result := range []*storage.Version{&first, &second} {
		if err := h.mutate(func(session storage.StoreSession) error {
			versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: material}})
			if err == nil {
				*result = versions[0]
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if first.ID == second.ID {
		t.Fatalf("same-byte publication reused logical version %q", first.ID)
	}
}

func TestOldReaderSurvivesConcurrentPublicationAndRotation(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("credential", "opaque", "contextStore")
	firstMaterial := opaque("first")
	defer firstMaterial.Clear()
	var first storage.Version
	if err := h.mutate(func(session storage.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: firstMaterial}})
		if err == nil {
			first = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(old storage.StoreSession) error {
		secondMaterial := opaque("second")
		defer secondMaterial.Clear()
		if err := h.mutate(func(current storage.StoreSession) error {
			_, err := current.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: secondMaterial}})
			return err
		}); err != nil {
			return err
		}
		if _, err := old.Inspect(context.Background()); err != nil {
			t.Fatalf("old reader after replacement: %v", err)
		}
		if err := h.mutate(func(current storage.StoreSession) error {
			_, err := current.Rotate(context.Background())
			return err
		}); err != nil {
			return err
		}
		if _, err := old.Inspect(context.Background()); err != nil {
			t.Fatalf("old reader after rotation: %v", err)
		}
		material, err := old.Read(context.Background(), first.ID)
		if err != nil {
			return err
		}
		defer material.Clear()
		if materialValue(t, material, secrets.ValuePart) != "first" {
			t.Fatal("old reader observed replacement material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInspectDoesNotReadOrAuthenticatePartPayloads(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("payload", "opaque", "contextStore")
	material := opaque("confidential")
	defer material.Clear()
	var version storage.Version
	if err := h.mutate(func(session storage.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: material}})
		if err == nil {
			version = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	partsDirectory := filepath.Join(h.root, "contexts", h.context.ID, "secrets", "parts")
	entries, err := os.ReadDir(partsDirectory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("part artifacts: %v %d", err, len(entries))
	}
	path := filepath.Join(partsDirectory, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wrapped envelope
	if err := decodeCanonical(data, partMaximum, 32, &wrapped); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := decodeBase64(wrapped.Ciphertext, partMaximum)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	wrapped.Ciphertext = rawBase64.EncodeToString(ciphertext)
	clear(ciphertext)
	tampered, err := encodeCanonical(wrapped, partMaximum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(session storage.StoreSession) error {
		if _, err := session.Inspect(context.Background()); err != nil {
			t.Fatalf("metadata inspection opened a part: %v", err)
		}
		_, err := session.Read(context.Background(), version.ID)
		if failureCode(err) != "secret.store.crypto" {
			t.Fatalf("tampered part: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSealLedgerRejectsUnauthenticatedIncrease(t *testing.T) {
	h := newIntegrationStore(t)
	ledgerDirectory := filepath.Join(h.root, "contexts", h.context.ID, "secrets", "ledgers")
	entries, err := os.ReadDir(ledgerDirectory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ledger artifacts: %v %d", err, len(entries))
	}
	path := filepath.Join(ledgerDirectory, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ledger sealLedger
	if err := decodeCanonical(data, ledgerMaximum, 24, &ledger); err != nil {
		t.Fatal(err)
	}
	ledger.Seals++
	tampered, err := encodeCanonical(ledger, ledgerMaximum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	err = h.view(func(storage.StoreSession) error { return nil })
	if failureCode(err) != "secret.store.corrupt" {
		t.Fatalf("unauthenticated ledger increase: %v", err)
	}
}

func TestInitializationRecordRejectsUnauthenticatedChange(t *testing.T) {
	h := newIntegrationStore(t)
	path := filepath.Join(h.root, "contexts", h.context.ID, "secrets", initializationPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record initializationRecord
	if err := decodeCanonical(data, selectorMaximum, 512, &record); err != nil {
		t.Fatal(err)
	}
	record.MAC = rawBase64.EncodeToString(make([]byte, 32))
	tampered, err := encodeCanonical(record, selectorMaximum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	err = h.view(func(storage.StoreSession) error { return nil })
	if failureCode(err) != "secret.store.corrupt" {
		t.Fatalf("unauthenticated initialization record: %v", err)
	}
}

type crashArea struct {
	storage.Area
	effects         int
	failAfter       int
	failBeforeIndex bool
}

type rejectMutationArea struct {
	storage.Area
	effects int
}

func (a *rejectMutationArea) EnsureDirectory(context.Context, string) error {
	a.effects++
	return errSimulatedCrash
}

func (a *rejectMutationArea) WriteExclusive(context.Context, string, []byte) error {
	a.effects++
	return errSimulatedCrash
}

func (a *rejectMutationArea) Replace(context.Context, string, []byte, []byte) (storage.Outcome, error) {
	a.effects++
	return storage.NotCommitted, errSimulatedCrash
}

func (a *rejectMutationArea) Sync(context.Context, string) error {
	a.effects++
	return errSimulatedCrash
}

var errSimulatedCrash = errors.New("simulated process interruption")

func (a *crashArea) EnsureDirectory(ctx context.Context, path string) error {
	a.effects++
	err := a.Area.EnsureDirectory(ctx, path)
	if err == nil && a.effects == a.failAfter {
		return errSimulatedCrash
	}
	return err
}

func (a *crashArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	a.effects++
	if a.failBeforeIndex && strings.HasPrefix(path, "indexes/") {
		a.failBeforeIndex = false
		return errSimulatedCrash
	}
	err := a.Area.WriteExclusive(ctx, path, data)
	if err == nil && a.effects == a.failAfter {
		return errSimulatedCrash
	}
	return err
}

func (a *crashArea) Replace(ctx context.Context, path string, data, expected []byte) (storage.Outcome, error) {
	a.effects++
	outcome, err := a.Area.Replace(ctx, path, data, expected)
	if err == nil && a.effects == a.failAfter {
		if outcome == storage.Committed {
			outcome = storage.Uncertain
		}
		return outcome, errSimulatedCrash
	}
	return outcome, err
}

func TestFreshInitializationResumesEveryDurableEffect(t *testing.T) {
	for failAfter := 1; failAfter <= 11; failAfter++ {
		t.Run(string(rune('a'+failAfter-1)), func(t *testing.T) {
			h := newUninitializedIntegrationStore(t)
			err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
				_, err := h.implementation.Initialize(context.Background(), h.context, &crashArea{Area: area, failAfter: failAfter}, nil)
				return err
			})
			if err == nil {
				t.Fatal("injected initialization interruption succeeded")
			}
			err = h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
				session, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
				if err != nil {
					return err
				}
				defer session.Close()
				snapshot, err := session.Inspect(context.Background())
				if err != nil || len(snapshot.Keys) != 1 || snapshot.ActiveKey == "" {
					t.Fatalf("recovered initialization: %#v %v", snapshot, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInitializationRefusesSelectorLossAfterPublishedUseWithoutWrites(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("credential", "opaque", "contextStore")
	material := opaque("published material")
	defer material.Clear()
	var version storage.Version
	if err := h.mutate(func(session storage.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: material}})
		if err == nil {
			version = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.mutate(func(session storage.StoreSession) error {
		_, err := session.Bind(context.Background(), []storage.BoundInput{{Declaration: d, Version: version.ID, Material: material}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.mutate(func(session storage.StoreSession) error {
		_, err := session.Rotate(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	selector := filepath.Join(h.root, "contexts", h.context.ID, "secrets", selectorPath)
	if err := os.Remove(selector); err != nil {
		t.Fatal(err)
	}
	var attempts int
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		guard := &rejectMutationArea{Area: area}
		_, err := h.implementation.Initialize(context.Background(), h.context, guard, nil)
		attempts = guard.effects
		return err
	})
	if failureCode(err) != "secret.store.corrupt" || attempts != 0 {
		t.Fatalf("selector-loss recovery: code=%q mutation-attempts=%d error=%v", failureCode(err), attempts, err)
	}
}

func TestInitializationRefusesSelectorLossAfterEmptyRotationWithoutWrites(t *testing.T) {
	h := newIntegrationStore(t)
	if err := h.mutate(func(session storage.StoreSession) error {
		_, err := session.Rotate(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	selector := filepath.Join(h.root, "contexts", h.context.ID, "secrets", selectorPath)
	if err := os.Remove(selector); err != nil {
		t.Fatal(err)
	}
	var attempts int
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		guard := &rejectMutationArea{Area: area}
		_, err := h.implementation.Initialize(context.Background(), h.context, guard, nil)
		attempts = guard.effects
		return err
	})
	if failureCode(err) != "secret.store.corrupt" || attempts != 0 {
		t.Fatalf("selector-loss recovery after rotation: code=%q mutation-attempts=%d error=%v", failureCode(err), attempts, err)
	}
}

func TestFreshInitializationBurnsReservationWhenIndexSealMayHaveStarted(t *testing.T) {
	h := newUninitializedIntegrationStore(t)
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		_, err := h.implementation.Initialize(context.Background(), h.context, &crashArea{Area: area, failBeforeIndex: true}, nil)
		return err
	})
	if err == nil {
		t.Fatal("injected pre-index publication interruption succeeded")
	}
	err = h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		session, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Keys) != 1 || snapshot.Keys[0].Seals != 2 {
			t.Fatalf("abandoned seal reservation was reused: %#v", snapshot.Keys)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func failureCode(err error) string {
	var failure *desiredstate.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) == 0 {
		return ""
	}
	return failure.Diagnostics[0].Code
}

func TestMaterialNeverAppearsInMetadataOrErrors(t *testing.T) {
	h := newIntegrationStore(t)
	secretValue := "unique-confidential-value"
	d := declaration("payload", "opaque", "contextStore")
	material := opaque(secretValue)
	defer material.Clear()
	if err := h.mutate(func(session storage.StoreSession) error {
		_, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: material}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(session storage.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if strings.Contains(snapshot.ActiveKey, secretValue) {
			t.Fatal("secret appeared in metadata")
		}
		return nil
	}); err != nil || bytes.Contains([]byte(errString(err)), []byte(secretValue)) {
		t.Fatalf("secret appeared in error: %v", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
