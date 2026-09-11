//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestMetadataHeaderAndCiphertextAreAuthenticated(t *testing.T) {
	selector := secretstore.Selector{SelectorVersion: formatVersion, ContextID: fixedID("ctx-", 1), Backend: New().Backend(), Generation: fixedID("gen-", 1)}
	keyID := fixedID("key-", 1)
	key := bytes.Repeat([]byte{7}, 32)
	plain := []byte("synthetic-authenticated-metadata")
	data, err := sealMetadata(key, plain, selector, keyID, &sequenceReader{})
	if err != nil {
		t.Fatal(err)
	}
	record, err := secretstore.DecodeRecord(data, selector.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*secretstore.Record){
		"context":    func(r *secretstore.Record) { r.ContextID = fixedID("ctx-", 2) },
		"backend":    func(r *secretstore.Record) { r.Backend = "other-backend-v2" },
		"generation": func(r *secretstore.Record) { r.Generation = fixedID("gen-", 2) },
		"key": func(r *secretstore.Record) {
			var payload metadataEnvelope
			if err := decodeMetadataPayload(r.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload.KeyID = fixedID("key-", 2)
			r.Payload, _ = encodeCanonical(payload, indexMaximum)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := record
			change(&changed)
			value, err := openMetadata(changed, key)
			defer clear(value)
			if err == nil || len(value) != 0 || strings.Contains(err.Error(), "synthetic") {
				t.Fatal("changed authenticated header was accepted or disclosed plaintext", err)
			}
		})
	}
	size, err := metadataEncodedSize(len(plain), selector, keyID)
	if err != nil || size != len(data) {
		t.Fatalf("metadata bound: %d != %d: %v", size, len(data), err)
	}
	unused := &sequenceReader{}
	if _, err := sealMetadata(key, make([]byte, secretstore.RecordMaximum), selector, keyID, unused); failureCode(err) != "secret.store.limit" || unused.calls != 0 {
		t.Fatal("metadata limit did not precede encryption", err)
	}
}

func TestRotationKeepsPhysicalStorageBounded(t *testing.T) {
	h := newIntegrationStore(t)
	material := opaque("synthetic-current-value")
	defer material.Clear()
	var version secretstore.Version
	if err := h.mutate(func(s secretstore.StoreSession) error {
		versions, err := s.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("credential", "opaque", "contextStore"), Material: material}})
		if err == nil {
			version = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
	initial := len(artifactTree(t, root))
	for range 24 {
		if err := h.mutate(func(s secretstore.StoreSession) error { _, err := s.Rotate(context.Background()); return err }); err != nil {
			t.Fatal(err)
		}
		if count := len(artifactTree(t, root)); count != initial {
			t.Fatalf("rotation accumulated artifacts: %d -> %d", initial, count)
		}
	}
	for _, removed := range []string{initializationPath, "selector.json", "indexes", "ledgers"} {
		if _, err := os.Stat(filepath.Join(root, removed)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete layout still present: %s: %v", removed, err)
		}
	}
	if err := h.view(func(s secretstore.StoreSession) error {
		value, err := s.Read(context.Background(), version.ID)
		defer value.Clear()
		if err == nil && materialValue(t, value, secrets.ValuePart) != "synthetic-current-value" {
			t.Fatal("rotation changed logical material")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMissingLiveIdentityReservationRefusesBeforeCleanup(t *testing.T) {
	for _, kind := range []string{"version", "binding"} {
		t.Run(kind, func(t *testing.T) {
			h := newIntegrationStore(t)
			material := opaque("synthetic-value")
			defer material.Clear()
			declaration := declaration("credential", "opaque", "contextStore")
			var version secretstore.Version
			if err := h.mutate(func(s secretstore.StoreSession) error {
				versions, err := s.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration, Material: material}})
				if err == nil {
					version = versions[0]
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			id := version.ID
			if kind == "binding" {
				if err := h.mutate(func(s secretstore.StoreSession) error {
					binding, err := s.Bind(context.Background(), []secretstore.BoundInput{{Declaration: declaration, Version: version.ID, Material: material}})
					id = binding.ID
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
			if err := os.Remove(filepath.Join(root, "identities", id+".json")); err != nil {
				t.Fatal(err)
			}
			before := artifactTree(t, root)
			err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
				s, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
				if s != nil {
					s.Close()
				}
				return err
			})
			if failureCode(err) != "secret.store.corrupt" || !reflect.DeepEqual(before, artifactTree(t, root)) {
				t.Fatal("missing reservation did not refuse without effects", err)
			}
		})
	}
}

type failingPruneArea struct{ secretstore.Area }

type substitutingDependencyArea struct {
	secretstore.Area
	t        *testing.T
	target   string
	replaced bool
}

func (a *substitutingDependencyArea) Replace(ctx context.Context, path string, replacement, expected []byte) (secretstore.Outcome, error) {
	if path == selectorPath && !a.replaced {
		data, err := os.ReadFile(a.target)
		if err != nil {
			a.t.Fatal(err)
		}
		defer clear(data)
		if err := os.Rename(a.target, filepath.Join(a.t.TempDir(), "prior")); err != nil {
			a.t.Fatal(err)
		}
		if err := os.WriteFile(a.target, data, 0600); err != nil {
			a.t.Fatal(err)
		}
		a.replaced = true
	}
	return a.Area.Replace(ctx, path, replacement, expected)
}

func TestPublicationRevalidatesAcquiredKeyAndPartIdentity(t *testing.T) {
	for _, dependency := range []string{"key", "part"} {
		t.Run(dependency, func(t *testing.T) {
			h := newIntegrationStore(t)
			material := opaque("synthetic-value")
			defer material.Clear()
			if err := h.mutate(func(s secretstore.StoreSession) error {
				_, err := s.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("credential", "opaque", "contextStore"), Material: material}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
			before, err := os.ReadFile(filepath.Join(root, selectorPath))
			if err != nil {
				t.Fatal(err)
			}
			err = h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
				guard := &substitutingDependencyArea{Area: area, t: t}
				s, err := h.implementation.open(context.Background(), h.context, guard, readTestSelector(t, area), nil)
				if err != nil {
					return err
				}
				defer s.Close()
				path := keyPath(s.index.ActiveKey)
				if dependency == "part" {
					path = partPath(s.index.Versions[0].Parts[0].BlobID)
				}
				guard.target = filepath.Join(root, path)
				_, err = s.Rotate(context.Background())
				if !guard.replaced {
					t.Fatal("substitution boundary was not reached")
				}
				return err
			})
			if err == nil {
				t.Fatal("publication accepted a replaced acquisition dependency")
			}
			after, err := os.ReadFile(filepath.Join(root, selectorPath))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("source substitution changed committed metadata", err)
			}
		})
	}
}

func (a failingPruneArea) Prune(context.Context, []byte, []string) error {
	return errors.New("synthetic-cleanup-fault")
}

func TestInitializationCleanupFailureReportsCommittedStore(t *testing.T) {
	h := newUninitializedIntegrationStore(t)
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		s, err := h.implementation.Initialize(context.Background(), h.context, failingPruneArea{area}, nil)
		if s != nil {
			s.Close()
		}
		return err
	})
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "committed") || !strings.Contains(diagnostics[0].Message, "secret encryption init") {
		t.Fatal("initialization cleanup failure concealed committed state", err)
	}
	if err := h.view(func(s secretstore.StoreSession) error {
		status, err := s.Inspect(context.Background())
		if err == nil && (!status.CleanupRequired || len(status.Keys) != 1) {
			t.Fatal("initialized store or cleanup evidence was lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupFailurePreservesCommittedMaterialAndExplicitRetry(t *testing.T) {
	h := newIntegrationStore(t)
	old := opaque("synthetic-old")
	defer old.Clear()
	declaration := declaration("credential", "opaque", "contextStore")
	if err := h.mutate(func(s secretstore.StoreSession) error {
		_, err := s.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration, Material: old}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	replacement := opaque("synthetic-replacement")
	defer replacement.Clear()
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		selector := readTestSelector(t, area)
		s, err := h.implementation.Open(context.Background(), h.context, failingPruneArea{area}, selector, nil)
		if err != nil {
			return err
		}
		defer s.Close()
		_, err = s.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration, Material: replacement}})
		return err
	})
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "committed") || strings.Contains(diagnostics[0].Message, "synthetic") {
		t.Fatal("cleanup fault concealed publication outcome", err)
	}
	root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
	before := artifactTree(t, root)
	if err := h.view(func(s secretstore.StoreSession) error {
		snapshot, err := s.Inspect(context.Background())
		if err != nil {
			return err
		}
		if !snapshot.CleanupRequired || snapshot.RetainedArtifacts == 0 {
			t.Fatal("cleanup fault was lost")
		}
		value, err := s.Read(context.Background(), snapshot.Current[0].Version)
		defer value.Clear()
		if err == nil && materialValue(t, value, secrets.ValuePart) != "synthetic-replacement" {
			t.Fatal("committed replacement was lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, artifactTree(t, root)) {
		t.Fatal("inspection performed cleanup")
	}
	if err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		s, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
		if err != nil {
			return err
		}
		defer s.Close()
		snapshot, err := s.Inspect(context.Background())
		if snapshot.CleanupRequired || snapshot.RetainedArtifacts != 0 {
			t.Fatal("explicit retry did not collect obsolete artifacts")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
