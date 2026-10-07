//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An unsealed setup bundle's writer removes a file an earlier build left
// shorter than its approved size under its final name, which only a write
// killed between creating and syncing it leaves, and the exact replay then
// publishes it. It refuses everything else: a file not shorter than approved,
// a directory, a stage name, a read-only area and a sealed one, each leaving
// the file where it is.
func TestTheAreasWriterDiscardsAShortFileAnEarlierBuildLeft(t *testing.T) {
	store, _ := fixture(t)
	scope := prerequisites.SetupContext{}
	value := syntheticControllerState(t, scope)
	value.Receipt.Actions[0].Phase = "intent"
	publishControllerState(t, store, scope, value)
	ctx := context.Background()
	var location string
	leave := func(name string, size int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(location, name), bytes.Repeat([]byte("p"), size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	present := func(name string) bool {
		_, err := os.Lstat(filepath.Join(location, name))
		return err == nil
	}
	refused := func(t *testing.T, area prerequisites.BundleArea, name string, approved int64) {
		t.Helper()
		if err := area.(prerequisites.BundleDiscard).DiscardPartial(ctx, name, approved); err == nil {
			t.Fatalf("%s was removed at an approved size of %d", name, approved)
		}
	}
	err := store.MutateController(ctx, scope, false, func(tx prerequisites.StorageTransaction) error {
		area, err := tx.Bundle(ctx, value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		if err := area.EnsureDirectory(ctx, "sources"); err != nil {
			return err
		}
		if err := area.EnsureDirectory(ctx, "python/lib"); err != nil {
			return err
		}
		held, err := area.Location(ctx)
		if err != nil {
			return err
		}
		location = held.Path
		leave("sources/python", 5)
		leave("sources/whole", 10)
		refused(t, area, "sources/whole", 10)
		refused(t, area, "sources/whole", 9)
		refused(t, area, "python/lib", 10)
		refused(t, area, "sources/"+bundleStagePrefix+strings.Repeat("0", 32), 10)
		if !present("sources/whole") || !present("python/lib") || !present("sources/python") {
			t.Fatal("a refused removal removed what it refused")
		}
		if err := area.(prerequisites.BundleDiscard).DiscardPartial(ctx, "sources/python", 10); err != nil {
			return err
		}
		entries, err := area.Entries(ctx)
		if err != nil {
			return err
		}
		if present("sources/python") || slices.ContainsFunc(entries, func(entry prerequisites.BundleEntry) bool { return entry.Path == "sources/python" }) {
			t.Fatal("the short file survived its removal")
		}
		return area.Write(ctx, "sources/python", bytes.Repeat([]byte("p"), 10), false)
	})
	if err != nil {
		t.Fatalf("the writer's removal failed: %#v (%v)", diagnostics.Of(err), err)
	}
	leave("sources/short", 3)
	err = store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		area, err := view.OpenBundle(ctx, value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		refused(t, area, "sources/short", 10)
		return nil
	})
	if err != nil || !present("sources/short") {
		t.Fatalf("a read-only area removed a short file: %v", err)
	}
	err = store.MutateController(ctx, scope, false, func(tx prerequisites.StorageTransaction) error {
		if _, err := tx.Publish(ctx, completeControllerState(tx.Snapshot().State)); err != nil {
			return err
		}
		area, err := tx.Bundle(ctx, value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		refused(t, area, "sources/short", 10)
		return nil
	})
	if err != nil || !present("sources/short") {
		t.Fatalf("a sealed area removed a short file: %#v (%v)", diagnostics.Of(err), err)
	}
}
