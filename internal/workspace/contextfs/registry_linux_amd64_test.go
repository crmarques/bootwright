//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func legacyContextRegistry(t *testing.T, store *Store, record contexts.Record) []byte {
	t.Helper()
	registry := emptyRegistry()
	registry.Identities = []contexts.Identity{{ID: "ctx-00000000000000000000000000000000"}, {ID: record.ID}}
	slices.SortFunc(registry.Identities, func(a, b contexts.Identity) int { return strings.Compare(a.ID, b.ID) })
	registry.Contexts = []contexts.Record{record}
	data, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(store.options.Root, "registry.json"), data)
	return data
}

func TestLegacyRegistryUpgradeOccursOnlyAtPublication(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	before := legacyContextRegistry(t, store, record)
	ctx := context.Background()
	for _, operation := range []func() error{
		func() error { _, err := store.View(ctx); return err },
		func() error { _, err := store.ReadInputs(ctx, record.Name, record.ID); return err },
		func() error { return store.Transact(ctx, false, nil, func(contexts.Transaction) error { return nil }) },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("read or no-op transaction upgraded legacy state")
		}
	}
	refusal := errors.New("synthetic refused mutation")
	if err := store.Transact(ctx, false, nil, func(contexts.Transaction) error { return refusal }); err == nil {
		t.Fatal("refused mutation succeeded")
	}
	after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused mutation upgraded legacy state")
	}
	if err := replaceInput(store, record, sources); err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(ctx)
	if err != nil || registry.Version != 3 || len(registry.Contexts) != 1 || registry.Contexts[0].ID != record.ID || registry.Contexts[0].Name != record.Name || len(registry.Identities) != 0 || registry.NextIdentity != 1 {
		t.Fatalf("legacy identity was not preserved: %#v %v", registry, err)
	}
	if registry.IDNamespace == identityNamespace(record.ID) || registry.IDNamespace == "0000000000000000" {
		t.Fatal("new allocator shares a legacy allocation namespace")
	}
	after, err = os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err != nil || bytes.Contains(after, []byte("identities")) || !bytes.Contains(after, []byte("\"nextIdentity\":1")) {
		t.Fatalf("v3 registry retained the lifetime ledger: %q %v", after, err)
	}
}

func TestLegacyRegistryUpgradeExcludesRetiredNamespaces(t *testing.T) {
	store, _ := fixture(t)
	registry := emptyRegistry()
	for index := range maxIdentities {
		registry.Identities = append(registry.Identities, contexts.Identity{ID: fmt.Sprintf("ctx-%016x0000000000000001", index)})
	}
	old, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	// First try a retired namespace, then a new one.
	store.random = io.MultiReader(bytes.NewReader(make([]byte, 16)), constantRandom(255))
	upgraded, err := store.upgradeRegistry(registry)
	if err != nil || upgraded.IDNamespace != "ffffffffffffffff" || upgraded.NextIdentity != 1 || len(upgraded.Identities) != 0 {
		t.Fatalf("namespace allocation did not exclude retired identities: %#v %v", upgraded, err)
	}
	after, err := encodeRecord(registry, maxRegistry)
	if err != nil || !bytes.Equal(old, after) {
		t.Fatal("in-memory migration modified legacy input")
	}
	for index := range maxIdentities * 2 {
		id, err := allocateContextIdentity(&upgraded)
		if err != nil || id != fmt.Sprintf("ctx-ffffffffffffffff%016x", index+1) {
			t.Fatalf("allocation stopped at historical limit: %s %v", id, err)
		}
	}
}

func TestRegistryAllocationContinuesBeyondHistoricalLimit(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	if err := deleteContext(t, store, record); err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registry.NextIdentity = maxIdentities + 1
	data, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(store.options.Root, "registry.json"), data)
	next := publish(t, store, "example", sources)
	if next.ID != fmt.Sprintf("ctx-%s%016x", registry.IDNamespace, maxIdentities+1) {
		t.Fatalf("context allocation after deletion: %s", next.ID)
	}
	if _, err := store.ReadInputs(context.Background(), record.Name, record.ID); err == nil {
		t.Fatal("stale identity was accepted after name reuse")
	}
}

func TestRegistryAllocationRefusesWithoutChangingLegacyStore(t *testing.T) {
	for _, random := range []struct {
		name   string
		reader io.Reader
	}{
		{"missing entropy", bytes.NewReader(nil)},
		{"retired namespace collisions", constantRandom(0)},
	} {
		t.Run(random.name, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			before := legacyContextRegistry(t, store, record)
			store.random = random.reader
			err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				_, err := tx.Reserve(context.Background(), "another", "", contexts.DefaultConfiguration("another").Canonical())
				return err
			})
			expectState(t, err)
			after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed namespace allocation changed legacy state")
			}
			if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "another")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed namespace allocation created a directory")
			}
		})
	}
}

func TestRegistryCounterExhaustionAndTamperingRefuse(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	registry, err := store.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contexts.Registry){
		func(r *contexts.Registry) { r.NextIdentity = 0 },
		func(r *contexts.Registry) { r.NextIdentity = 1 },
		func(r *contexts.Registry) { r.IDNamespace = "invalid" },
		func(r *contexts.Registry) { r.Identities = []contexts.Identity{{ID: record.ID}} },
	} {
		changed := cloneRegistry(registry)
		change(&changed)
		expectState(t, validateRegistry(changed))
	}
	registry.NextIdentity = ^uint64(0)
	before, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(store.options.Root, "registry.json"), before)
	err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		_, err := tx.Reserve(context.Background(), "another", "", contexts.DefaultConfiguration("another").Canonical())
		return err
	})
	expectState(t, err)
	after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("exhausted allocation wrapped or rewrote registry")
	}
}

func TestRegistryReservationRetryPreservesAllocatedIdentity(t *testing.T) {
	store, sources := fixture(t)
	existing := publish(t, store, "example", sources)
	legacyContextRegistry(t, store, existing)
	ctx := context.Background()
	reserve := func() error {
		return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			_, err := tx.Reserve(ctx, "another", "", contexts.DefaultConfiguration("another").Canonical())
			return err
		})
	}
	store.fail = func(point string) error {
		if point == "after-registry-rename" {
			return errors.New("synthetic namespace publication interruption")
		}
		return nil
	}
	expectState(t, reserve())
	store.fail = nil
	before, err := store.View(ctx)
	if err != nil || before.Version != 3 || len(before.Contexts) != 2 || before.NextIdentity != 2 || before.Contexts[0].Name != "another" {
		t.Fatalf("uncertain allocation state: %#v %v", before, err)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "another")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("directory creation preceded durable allocation")
	}
	if err := reserve(); err != nil {
		t.Fatal(err)
	}
	after, err := store.View(ctx)
	if err != nil || after.IDNamespace != before.IDNamespace || after.NextIdentity != before.NextIdentity || after.Contexts[0].ID != before.Contexts[0].ID || after.Contexts[1].ID != existing.ID {
		t.Fatalf("retry allocated another identity or namespace: %#v %v", after, err)
	}
}

func TestRegistryV3CanonicalFields(t *testing.T) {
	valid := []byte("{\"version\":3,\"idNamespace\":\"0123456789abcdef\",\"nextIdentity\":1,\"contexts\":[]}\n")
	for _, data := range [][]byte{
		bytes.Replace(valid, []byte("\"contexts\":[]"), []byte("\"contexts\":null"), 1),
		bytes.Replace(valid, []byte("\"nextIdentity\":1,"), nil, 1),
		bytes.Replace(valid, []byte("\"contexts\":[]"), []byte("\"identities\":[],\"contexts\":[]"), 1),
		bytes.Replace(valid, []byte("\"nextIdentity\":1"), []byte("\"nextIdentity\":1,\"nextIdentity\":2"), 1),
		bytes.Replace(valid, []byte("\"nextIdentity\":1"), []byte("\"nextIdentity\":18446744073709551616"), 1),
	} {
		var registry contexts.Registry
		expectState(t, decodeRecord(data, maxRegistry, &registry))
	}
	var registry contexts.Registry
	if err := decodeRecord(valid, maxRegistry, &registry); err != nil {
		t.Fatal(err)
	}
	if err := validateRegistry(registry); err != nil {
		t.Fatal(err)
	}
	actual, err := encodeRecord(registry, len(valid))
	if err != nil || !bytes.Equal(actual, valid) {
		t.Fatalf("v3 canonical round trip: %q %v", actual, err)
	}
	if _, err := encodeRecord(registry, len(valid)-1); err == nil {
		t.Fatal("v3 canonical size bound was not enforced")
	}
}
