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
	workspace := contextfs.New(contextfs.Options{Root: root, Owner: &contextfs.Ownership{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}})
	sources := desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\n"))}, Markers: []desiredstate.SourceFile{}}
	err := workspace.Transact(context.Background(), true, sources.Roots, func(tx contexts.Transaction) error {
		record, err := tx.Reserve(context.Background(), "example", input, contexts.DefaultConfiguration("example").Canonical())
		if err != nil {
			return err
		}
		if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), record.ID, input, sources)
		if err != nil {
			return err
		}
		registry := tx.Registry()
		record.Revision, record.Mode = revision, contexts.Ready
		registry.Contexts[0] = record
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
	data, exists, err := area.ReadMutable(context.Background(), selectorPath, storage.RecordMaximum)
	if err != nil || !exists {
		t.Fatalf("read metadata: %v", err)
	}
	var record storage.Record
	if err := storage.DecodeCanonical(data, &record); err != nil {
		t.Fatal(err)
	}
	return record.Selector
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
		if !slices.Equal(beforeRotation, after) || snapshot.ActiveKey != rotated || len(snapshot.Keys) != 1 || snapshot.RetainedArtifacts != 0 || snapshot.CleanupRequired {
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
		if len(snapshot.Versions) != 0 || len(snapshot.Current) != 0 || len(snapshot.Bindings) != 0 || snapshot.RetainedArtifacts != 0 || snapshot.CleanupRequired {
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

func TestReaderBlocksPublicationAndRotationUntilSessionCloses(t *testing.T) {
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
	secondMaterial := opaque("second")
	defer secondMaterial.Clear()
	var second storage.Version
	mutations := []func(storage.StoreSession) error{
		func(current storage.StoreSession) error {
			versions, err := current.PutBatch(context.Background(), []storage.Put{{Declaration: d, Material: secondMaterial}})
			if err == nil {
				second = versions[0]
			}
			return err
		},
		func(current storage.StoreSession) error {
			_, err := current.Rotate(context.Background())
			return err
		},
	}
	if err := h.view(func(reader storage.StoreSession) error {
		for _, mutation := range mutations {
			entered := false
			err := h.mutate(func(current storage.StoreSession) error {
				entered = true
				return mutation(current)
			})
			if entered || failureCode(err) != "secret.store.conflict" {
				t.Fatalf("mutation during read session: entered=%v error=%v", entered, err)
			}
			if _, err := reader.Inspect(context.Background()); err != nil {
				return err
			}
		}
		material, err := reader.Read(context.Background(), first.ID)
		if err != nil {
			return err
		}
		defer material.Clear()
		if materialValue(t, material, secrets.ValuePart) != "first" {
			t.Fatal("reader observed replacement material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range mutations {
		if err := h.mutate(mutation); err != nil {
			t.Fatalf("mutation after read session closed: %v", err)
		}
	}
	if err := h.view(func(reader storage.StoreSession) error {
		material, err := reader.Read(context.Background(), second.ID)
		if err != nil {
			return err
		}
		defer material.Clear()
		if materialValue(t, material, secrets.ValuePart) != "second" {
			t.Fatal("rotation changed published replacement material")
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
	partsDirectory := filepath.Join(h.root, "contexts", h.context.Name, "secrets", "parts")
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
	var activeKey string
	if err := h.view(func(session storage.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		activeKey = snapshot.ActiveKey
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.root, "contexts", h.context.Name, "secrets", ledgerPath(activeKey))
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
	h := newUninitializedIntegrationStore(t)
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		_, err := h.implementation.Initialize(context.Background(), h.context, &crashArea{Area: area, failBeforeIndex: true}, nil)
		return err
	})
	if err == nil {
		t.Fatal("initialization interruption succeeded")
	}
	path := filepath.Join(h.root, "contexts", h.context.Name, "secrets", initializationPath)
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
	err = h.workspace.MutateSecrets(context.Background(), h.context, func(area storage.Area) error {
		guard := &rejectMutationArea{Area: area}
		_, err := h.implementation.Initialize(context.Background(), h.context, guard, nil)
		if guard.effects != 0 {
			t.Fatal("tampered recovery attempted mutation")
		}
		return err
	})
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

func (a *rejectMutationArea) SyncFile(context.Context, string) error {
	a.effects++
	return errSimulatedCrash
}

func (a *rejectMutationArea) PruneUnpublished(context.Context, []storage.RecordExpectation, []string) error {
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
	err := a.Area.WriteExclusive(ctx, path, data)
	if err == nil && a.effects == a.failAfter {
		return errSimulatedCrash
	}
	return err
}

func (a *crashArea) SyncFile(ctx context.Context, path string) error {
	a.effects++
	err := a.Area.SyncFile(ctx, path)
	if err == nil && a.effects == a.failAfter {
		return errSimulatedCrash
	}
	return err
}

func (a *crashArea) Replace(ctx context.Context, path string, data, expected []byte) (storage.Outcome, error) {
	a.effects++
	if a.failBeforeIndex && path == selectorPath {
		a.failBeforeIndex = false
		return storage.NotCommitted, errSimulatedCrash
	}
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
	baseline := newUninitializedIntegrationStore(t)
	var effects int
	if err := baseline.workspace.MutateSecrets(context.Background(), baseline.context, func(area storage.Area) error {
		tracked := &crashArea{Area: area}
		session, err := baseline.implementation.Initialize(context.Background(), baseline.context, tracked, nil)
		if session != nil {
			session.Close()
		}
		effects = tracked.effects
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for failAfter := 1; failAfter <= effects; failAfter++ {
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
	selector := filepath.Join(h.root, "contexts", h.context.Name, "secrets", selectorPath)
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
	selector := filepath.Join(h.root, "contexts", h.context.Name, "secrets", selectorPath)
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

func (a *rejectMutationArea) Prune(context.Context, []byte, []string) error {
	a.effects++
	return errSimulatedCrash
}

func (a *crashArea) Prune(ctx context.Context, expected []byte, paths []string) error {
	a.effects++
	err := a.Area.Prune(ctx, expected, paths)
	if err == nil && a.effects == a.failAfter {
		return errSimulatedCrash
	}
	return err
}
