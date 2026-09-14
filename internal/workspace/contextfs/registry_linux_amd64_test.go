//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A registry written by a format that named contexts by an allocated identity
// carries records this reader cannot attribute, so every access refuses and
// nothing in the store is rewritten.
func TestSupersededRegistryFormatsRefuseWithoutRewriting(t *testing.T) {
	for _, version := range []string{"2", "3", "4"} {
		t.Run("v"+version, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			path := filepath.Join(store.options.Root, "registry.json")
			before := []byte("{\"version\":" + version + ",\"idNamespace\":\"0123456789abcdef\",\"nextIdentity\":2,\"contexts\":[]}\n")
			writePrivate(t, path, before)
			ctx := context.Background()
			for _, operation := range []func() error{
				func() error { _, err := store.View(ctx); return err },
				func() error { _, err := store.ReadInputs(ctx, record.Name); return err },
				func() error {
					return store.Transact(ctx, false, nil, func(contexts.Transaction) error { return nil })
				},
			} {
				expectState(t, operation())
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("refused registry was rewritten: %q %v", after, err)
			}
		})
	}
}

func TestRegistryCanonicalFields(t *testing.T) {
	valid := []byte("{\"version\":5,\"contexts\":[]}\n")
	for _, data := range [][]byte{
		bytes.Replace(valid, []byte("\"contexts\":[]"), []byte("\"contexts\":null"), 1),
		bytes.Replace(valid, []byte("\"version\":5,"), nil, 1),
		bytes.Replace(valid, []byte("\"contexts\":[]"), []byte("\"identities\":[],\"contexts\":[]"), 1),
		bytes.Replace(valid, []byte("\"contexts\":[]"), []byte("\"contexts\":[],\"nextIdentity\":1"), 1),
		bytes.Replace(valid, []byte("\"version\":5"), []byte("\"version\":5,\"version\":5"), 1),
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
		t.Fatalf("canonical round trip: %q %v", actual, err)
	}
	if _, err := encodeRecord(registry, len(valid)-1); err == nil {
		t.Fatal("canonical size bound was not enforced")
	}
}

// The controller descriptor is absent until confirmed setup introduces it, so a
// store that never had one encodes without the member.
func TestRegistryOmitsAnAbsentControllerDescriptor(t *testing.T) {
	registry := emptyRegistry()
	data, err := encodeRecord(registry, maxRegistry)
	if err != nil || bytes.Contains(data, []byte("controller")) {
		t.Fatalf("empty registry named a controller subtree: %q %v", data, err)
	}
	registry.Controller = contexts.ControllerDescriptor{Version: 1, Mode: "initializing"}
	data, err = encodeRecord(registry, maxRegistry)
	if err != nil || !bytes.Contains(data, []byte("\"controller\":{\"version\":1")) {
		t.Fatalf("declared controller subtree was not recorded: %q %v", data, err)
	}
	var decoded contexts.Registry
	if err := decodeRecord(data, maxRegistry, &decoded); err != nil || decoded.Controller != registry.Controller {
		t.Fatalf("controller descriptor round trip: %#v %v", decoded, err)
	}
}

func TestRegistryRefusesUnorderedOrDuplicateNames(t *testing.T) {
	ready := func(name string) contexts.Record {
		return contexts.Record{Name: name, Mode: contexts.Ready, SecretStoreType: "local-keyring", DirectoryDevice: 1, DirectoryInode: 2}
	}
	for name, records := range map[string][]contexts.Record{
		"unordered": {ready("second"), ready("first")},
		"duplicate": {ready("same"), ready("same")},
		"invalid":   {ready("Not-A-Label")},
	} {
		t.Run(name, func(t *testing.T) {
			registry := emptyRegistry()
			registry.Contexts = records
			expectState(t, validateRegistry(registry))
		})
	}
}

// An interrupted reservation leaves the name reserved and creates no directory;
// the retry resumes that exact name rather than reserving a second one.
func TestRegistryReservationRetryResumesItsReservedName(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	ctx := context.Background()
	reserve := func() error {
		return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			_, err := tx.Reserve(ctx, "another", "", contexts.DefaultConfiguration("another").Canonical())
			return err
		})
	}
	store.fail = func(point string) error {
		if point == "after-context-reservation" {
			return errors.New("synthetic reservation publication interruption")
		}
		return nil
	}
	expectState(t, reserve())
	store.fail = nil
	before, err := store.View(ctx)
	if err != nil || len(before.Contexts) != 2 || before.Contexts[0].Name != "another" || before.Contexts[0].Mode != contexts.Initializing {
		t.Fatalf("interrupted reservation state: %#v %v", before, err)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "another")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("directory creation preceded durable reservation")
	}
	if err := reserve(); err != nil {
		t.Fatal(err)
	}
	after, err := store.View(ctx)
	if err != nil || len(after.Contexts) != 2 || after.Contexts[0].Name != before.Contexts[0].Name {
		t.Fatalf("retry reserved another name: %#v %v", after, err)
	}
}
