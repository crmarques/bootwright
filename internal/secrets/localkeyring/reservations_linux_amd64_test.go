//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (h *integrationStore) reserved() []string {
	h.t.Helper()
	var reserved []string
	if err := h.view(func(opened secretstore.StoreSession) error {
		reserved = slices.Clone(opened.(*session).index.Reserved)
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return reserved
}

// reseal rewrites the store record in place with changed plaintext, sealed
// under the store's own key, as only a holder of the keys could.
func (h *integrationStore) reseal(change func([]byte) []byte) {
	h.t.Helper()
	path := filepath.Join(h.secretsRoot(), selectorPath)
	data, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	record, err := secretstore.DecodeRecord(data, h.context.Name)
	if err != nil {
		h.t.Fatal(err)
	}
	var wrapped metadataEnvelope
	if err := decodeMetadataPayload(record.Payload, &wrapped); err != nil {
		h.t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(h.secretsRoot(), keyPath(wrapped.KeyID)))
	if err != nil {
		h.t.Fatal(err)
	}
	plaintext, err := openMetadata(record, key)
	if err != nil {
		h.t.Fatal(err)
	}
	sealed, err := sealMetadata(key, change(plaintext), record.Selector, wrapped.KeyID, rand.Reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *integrationStore) putOne(name, value string) secretstore.Version {
	h.t.Helper()
	material := opaque(value)
	defer material.Clear()
	var version secretstore.Version
	if err := h.mutate(func(session secretstore.StoreSession) error {
		versions, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration(name, "opaque", "contextStore"), Material: material}})
		if err == nil {
			version = versions[0]
		}
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	return version
}

func (h *integrationStore) bindOne(name, value string, version secretstore.Version) secretstore.Binding {
	h.t.Helper()
	material := opaque(value)
	defer material.Clear()
	var binding secretstore.Binding
	if err := h.mutate(func(session secretstore.StoreSession) error {
		var err error
		binding, err = session.Bind(context.Background(), []secretstore.BoundInput{{Declaration: declaration(name, "opaque", "contextStore"), Version: version.ID, Material: material}})
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	return binding
}

func replaceMember(t *testing.T, plaintext []byte, member string, replacement []byte) []byte {
	t.Helper()
	start := bytes.Index(plaintext, []byte(`,"`+member+`":[`))
	if start < 0 {
		t.Fatalf("the metadata has no %s member", member)
	}
	end := start + bytes.IndexByte(plaintext[start:], ']') + 1
	return slices.Concat(plaintext[:start], replacement, plaintext[end:])
}

func reservedMember(ids []string) []byte {
	if len(ids) == 0 {
		return []byte(`,"reserved":[]`)
	}
	return []byte(`,"reserved":["` + strings.Join(ids, `","`) + `"]`)
}

// Every identity the store issued stays reserved inside its authenticated
// metadata after its version is deleted and its binding released, no
// reservation file is written, and a fresh identity that draws a retired one
// is drawn again.
func TestReservationsOutliveTheirVersionsAndBindings(t *testing.T) {
	h := newIntegrationStore(t)
	first := h.putOne("credential", "synthetic-first")
	binding := h.bindOne("credential", "synthetic-first", first)
	second := h.putOne("credential", "synthetic-second")
	if err := h.mutate(func(session secretstore.StoreSession) error {
		if _, err := session.Release(context.Background(), binding.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.Delete(context.Background(), "credential")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{binding.ID, first.ID, second.ID}
	slices.Sort(want)
	if got := h.reserved(); !slices.Equal(got, want) {
		t.Fatalf("reservations = %v, want %v", got, want)
	}
	if snapshot := h.snapshot(); len(snapshot.Versions) != 0 || len(snapshot.Bindings) != 0 {
		t.Fatalf("the store still holds %d versions and %d bindings", len(snapshot.Versions), len(snapshot.Bindings))
	}
	if _, err := os.Stat(filepath.Join(h.secretsRoot(), earlierIdentityDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a per-file reservation directory exists: %v", err)
	}
	retired, err := hex.DecodeString(strings.TrimPrefix(first.ID, "ver-"))
	if err != nil {
		t.Fatal(err)
	}
	h.implementation = NewWithOptions(Options{Random: io.MultiReader(bytes.NewReader(retired), rand.Reader)})
	again := h.putOne("credential", "synthetic-third")
	if again.ID == first.ID || !slices.Contains(h.reserved(), again.ID) {
		t.Fatalf("a retired identity was issued again or the new one is unreserved: %s", again.ID)
	}
}

func refusesWithoutEffects(t *testing.T, h *integrationStore, code string) error {
	t.Helper()
	before := artifactTree(t, h.secretsRoot())
	err := h.view(func(secretstore.StoreSession) error { return nil })
	if failureCode(err) != code {
		t.Fatalf("open = %v, want %s", err, code)
	}
	if !reflect.DeepEqual(before, artifactTree(t, h.secretsRoot())) {
		t.Fatal("a refused open changed the store")
	}
	return err
}

// The reservation record is the metadata's own member, so whoever changes it
// without the keys is refused by authentication, and a reseal that drops, malforms,
// disorders or overfills it, or omits it as an earlier build did, is refused
// as corrupt before any session.
func TestATamperedOrMissingReservationRecordRefuses(t *testing.T) {
	cases := map[string]func(*testing.T, []string, []byte) []byte{
		"a live version's reservation dropped": func(t *testing.T, ids []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", reservedMember(ids[:1]))
		},
		"a live binding's reservation dropped": func(t *testing.T, ids []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", reservedMember(ids[1:]))
		},
		"the member missing": func(t *testing.T, _ []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", nil)
		},
		"a null member": func(t *testing.T, _ []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", []byte(`,"reserved":null`))
		},
		"out of order": func(t *testing.T, ids []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", reservedMember([]string{ids[1], ids[0]}))
		},
		"a duplicate": func(t *testing.T, ids []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", reservedMember([]string{ids[0], ids[0], ids[1]}))
		},
		"a foreign identifier": func(t *testing.T, ids []string, plaintext []byte) []byte {
			return replaceMember(t, plaintext, "reserved", reservedMember(append(slices.Clone(ids), fixedID("key-", 1))))
		},
		"past its cap": func(t *testing.T, ids []string, plaintext []byte) []byte {
			all := slices.Clone(ids)
			for item := 0; len(all) <= maxReservations; item++ {
				all = append(all, fixedID("ver-", item+1))
			}
			slices.Sort(all)
			return replaceMember(t, plaintext, "reserved", reservedMember(all))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			h := newIntegrationStore(t)
			version := h.putOne("credential", "synthetic-value")
			binding := h.bindOne("credential", "synthetic-value", version)
			ids := []string{binding.ID, version.ID}
			if got := h.reserved(); !slices.Equal(got, ids) {
				t.Fatalf("reservations = %v, want %v", got, ids)
			}
			h.reseal(func(plaintext []byte) []byte { return change(t, ids, plaintext) })
			refusesWithoutEffects(t, h, "secret.store.corrupt")
		})
	}
	t.Run("a changed ciphertext", func(t *testing.T) {
		h := newIntegrationStore(t)
		h.putOne("credential", "synthetic-value")
		path := filepath.Join(h.secretsRoot(), selectorPath)
		data := h.storeRecord()
		record, err := secretstore.DecodeRecord(data, h.context.Name)
		if err != nil {
			t.Fatal(err)
		}
		var wrapped metadataEnvelope
		if err := decodeMetadataPayload(record.Payload, &wrapped); err != nil {
			t.Fatal(err)
		}
		ciphertext, err := rawBase64.DecodeString(wrapped.Ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		ciphertext[len(ciphertext)/2] ^= 1
		changed := strings.Replace(string(data), wrapped.Ciphertext, rawBase64.EncodeToString(ciphertext), 1)
		if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
		refusesWithoutEffects(t, h, "secret.store.crypto")
	})
}

// A store record from another keyring, or one sealed for another context, is
// refused: the reservations it carries are never adopted.
func TestAForeignReservationRecordRefuses(t *testing.T) {
	h := newIntegrationStore(t)
	h.putOne("credential", "synthetic-value")
	other := newIntegrationStore(t)
	other.putOne("credential", "synthetic-other")
	if err := os.WriteFile(filepath.Join(h.secretsRoot(), selectorPath), other.storeRecord(), 0600); err != nil {
		t.Fatal(err)
	}
	before := artifactTree(t, h.secretsRoot())
	if err := h.view(func(secretstore.StoreSession) error { return nil }); err == nil || !reflect.DeepEqual(before, artifactTree(t, h.secretsRoot())) {
		t.Fatalf("another keyring's record = %v", err)
	}

	foreign := newIntegrationStore(t)
	foreign.putOne("credential", "synthetic-value")
	path := filepath.Join(foreign.secretsRoot(), selectorPath)
	record, err := secretstore.DecodeRecord(foreign.storeRecord(), foreign.context.Name)
	if err != nil {
		t.Fatal(err)
	}
	var wrapped metadataEnvelope
	if err := decodeMetadataPayload(record.Payload, &wrapped); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(foreign.secretsRoot(), keyPath(wrapped.KeyID)))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := openMetadata(record, key)
	if err != nil {
		t.Fatal(err)
	}
	selector := record.Selector
	selector.Context = "other"
	sealed, err := sealMetadata(key, plaintext, selector, wrapped.KeyID, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	before = artifactTree(t, foreign.secretsRoot())
	err = foreign.workspace.ReadSecrets(context.Background(), foreign.context, func(area secretstore.Area) error {
		session, err := foreign.implementation.Open(context.Background(), foreign.context, area, record.Selector, nil)
		if session != nil {
			session.Close()
		}
		return err
	})
	if failureCode(err) != "secret.store.corrupt" || !reflect.DeepEqual(before, artifactTree(t, foreign.secretsRoot())) {
		t.Fatalf("a record sealed for another context = %v", err)
	}
}

// A keyring an earlier build left keeps one reservation file each and
// metadata without the member; it is refused, never converted, and the
// refusal names that layout.
func TestAnEarlierBuildsPerFileReservationsAreRefused(t *testing.T) {
	for name, metadata := range map[string]bool{"its metadata": true, "only its directory": false} {
		t.Run(name, func(t *testing.T) {
			h := newIntegrationStore(t)
			version := h.putOne("credential", "synthetic-value")
			directory := filepath.Join(h.secretsRoot(), earlierIdentityDirectory)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, version.ID+".json"), []byte(`{"formatVersion":1,"context":"example","id":"`+version.ID+`"}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if metadata {
				h.reseal(func(plaintext []byte) []byte { return replaceMember(t, plaintext, "reserved", nil) })
			}
			err := refusesWithoutEffects(t, h, "secret.store.corrupt")
			if reported := diagnostics.Of(err); !strings.Contains(reported[0].Message, "per-file layout of an earlier build") {
				t.Fatalf("the refusal does not name the earlier layout: %v", err)
			}
		})
	}
}

// At its cap the record admits no new identity: a publication refuses with
// store.limit before any effect, and the store stays readable.
func TestAFullReservationRecordRefusesNewIdentitiesBeforeEffects(t *testing.T) {
	h := newIntegrationStore(t)
	version := h.putOne("credential", "synthetic-value")
	full := []string{version.ID}
	for item := 0; len(full) < maxReservations; item++ {
		full = append(full, fixedID("bind-", item+1))
	}
	slices.Sort(full)
	h.reseal(func(plaintext []byte) []byte { return replaceMember(t, plaintext, "reserved", reservedMember(full)) })
	if got := h.reserved(); len(got) != maxReservations {
		t.Fatalf("the full record holds %d reservations", len(got))
	}
	before := artifactTree(t, h.secretsRoot())
	material := opaque("synthetic-value")
	defer material.Clear()
	for name, publish := range map[string]func(secretstore.StoreSession) error{
		"a version": func(session secretstore.StoreSession) error {
			_, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("other", "opaque", "contextStore"), Material: material}})
			return err
		},
		"a binding": func(session secretstore.StoreSession) error {
			_, err := session.Bind(context.Background(), []secretstore.BoundInput{{Declaration: declaration("credential", "opaque", "contextStore"), Version: version.ID, Material: material}})
			return err
		},
	} {
		if err := h.mutate(publish); failureCode(err) != "secret.store.limit" {
			t.Fatalf("%s at the cap = %v, want secret.store.limit", name, err)
		}
		if !reflect.DeepEqual(before, artifactTree(t, h.secretsRoot())) {
			t.Fatalf("%s at the cap changed the store", name)
		}
	}
}

type stagedThenKilledArea struct {
	secretstore.Area
	root   string
	staged string
}

func (a *stagedThenKilledArea) Replace(ctx context.Context, path string, replacement, expected []byte) (secretstore.Outcome, error) {
	if path != selectorPath {
		return a.Area.Replace(ctx, path, replacement, expected)
	}
	a.staged = filepath.Join(a.root, "pending-"+strings.Repeat("e", 32))
	if err := os.WriteFile(a.staged, replacement, 0600); err != nil {
		return secretstore.NotCommitted, err
	}
	return secretstore.NotCommitted, errors.New("killed between stage and rename")
}

// A publication killed after its record's stage is written and before the
// rename leaves the earlier record and its reservations in force; the stage
// is a retained artifact the next publication collects.
func TestACrashBetweenStageAndRenameKeepsTheEarlierReservations(t *testing.T) {
	h := newIntegrationStore(t)
	h.putOne("credential", "synthetic-first")
	record, reserved := h.storeRecord(), h.reserved()
	killed := &stagedThenKilledArea{root: h.secretsRoot()}
	material := opaque("synthetic-second")
	defer material.Clear()
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		killed.Area = area
		session, err := h.implementation.Open(context.Background(), h.context, killed, readTestSelector(t, area), nil)
		if err != nil {
			return err
		}
		defer session.Close()
		_, err = session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("credential", "opaque", "contextStore"), Material: material}})
		return err
	})
	if err == nil || killed.staged == "" {
		t.Fatalf("the killed publication = %v", err)
	}
	if !bytes.Equal(h.storeRecord(), record) || !slices.Equal(h.reserved(), reserved) {
		t.Fatal("a killed publication changed the record or its reservations")
	}
	if snapshot := h.snapshot(); !snapshot.CleanupRequired {
		t.Fatal("the stage is not reported as a retained artifact")
	}
	next := h.putOne("credential", "synthetic-third")
	if _, err := os.Stat(killed.staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the next publication left the stage: %v", err)
	}
	if got := h.reserved(); len(got) != len(reserved)+1 || !slices.Contains(got, next.ID) {
		t.Fatalf("reservations after recovery = %v", got)
	}
}
