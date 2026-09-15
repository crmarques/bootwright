//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type payload struct{ *bytes.Reader }

func (payload) Close() error { return nil }

func mediaPayload(data string) media.Payload { return payload{bytes.NewReader([]byte(data))} }

func mediaDigest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// mediaFixture prepares an initialized store, because media is host-wide state
// that lives beside the registry rather than inside any context.
func mediaFixture(t *testing.T) *Store {
	t.Helper()
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	return store
}

func addMedia(t *testing.T, store *Store, name, data string, replace bool) {
	t.Helper()
	err := store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		staged, err := tx.Stage(context.Background(), mediaPayload(data), managedos.MaxMediaBytes)
		if err != nil {
			return err
		}
		record, err := managedos.EncodeMediaRecord(managedos.MediaEntry{
			Name: name, Size: staged.Size, SHA256: staged.SHA256,
			Source: "file:///images/" + name, Added: "2026-09-15T09:00:00Z",
		})
		if err != nil {
			return err
		}
		return tx.Publish(context.Background(), name, staged, record, replace)
	})
	if err != nil {
		t.Fatalf("media publication failed: %#v", diagnostics.Of(err))
	}
}

func TestMediaPublicationRetainsExactBytesAndTheirRecord(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	err := store.ReadMedia(ctx, func(view media.View) error {
		entries, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].Name != "demo.iso" || entries[0].Size != 15 || entries[0].SHA256 != mediaDigest("installer bytes") {
			t.Fatalf("entries = %+v", entries)
		}
		digest, err := view.Digest(ctx, "demo.iso")
		if err != nil || digest != entries[0].SHA256 {
			t.Fatalf("digest = %q (%v)", digest, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read: %#v", diagnostics.Of(err))
	}
	image := filepath.Join(store.options.Root, "media", "demo.iso")
	info, err := os.Lstat(image)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("published image mode = %v (%v)", info, err)
	}
	data, err := os.ReadFile(image)
	if err != nil || string(data) != "installer bytes" {
		t.Fatalf("published bytes = %q (%v)", data, err)
	}
}

func TestMediaReplacementSupersedesBothTheRecordAndTheBytes(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "first", false)
	addMedia(t, store, "demo.iso", "second image", true)
	err := store.ReadMedia(ctx, func(view media.View) error {
		entries, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].Size != 12 || entries[0].SHA256 != mediaDigest("second image") {
			t.Fatalf("entries = %+v", entries)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMediaPublicationRefusesToOverwriteAnOccupiedName(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "first", false)
	err := store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		staged, err := tx.Stage(context.Background(), mediaPayload("second"), managedos.MaxMediaBytes)
		if err != nil {
			return err
		}
		record, err := managedos.EncodeMediaRecord(managedos.MediaEntry{
			Name: "demo.iso", Size: staged.Size, SHA256: staged.SHA256, Source: "file:///x", Added: "2026-09-15T09:00:00Z",
		})
		if err != nil {
			return err
		}
		return tx.Publish(context.Background(), "demo.iso", staged, record, false)
	})
	if err == nil {
		t.Fatal("a publication replaced an occupied name without asking")
	}
}

func TestMediaDeletionRemovesTheRecordAndTheImage(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	err := store.MutateMedia(ctx, func(tx media.Transaction) error { return tx.Delete(ctx, "demo.iso") })
	if err != nil {
		t.Fatalf("delete: %#v", diagnostics.Of(err))
	}
	entries, err := os.ReadDir(filepath.Join(store.options.Root, "media"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("media directory = %v (%v)", entries, err)
	}
}

// An interrupted publication leaves staged bytes behind. They are never
// adopted: the name stays occupied until a later publication replaces it, and
// the next mutation removes the staging file.
func TestInterruptedStagingIsOccupiedButNeverAdopted(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	if err := os.Remove(filepath.Join(store.options.Root, "media", "demo.iso.json")); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(store.options.Root, "media", "pending-"+strings.Repeat("a", 32))
	if err := os.WriteFile(staging, []byte("abandoned"), 0600); err != nil {
		t.Fatal(err)
	}
	err := store.ReadMedia(ctx, func(view media.View) error {
		names, err := view.Names(ctx)
		if err != nil {
			return err
		}
		if !slices.Equal(names, []string{"demo.iso"}) {
			t.Fatalf("names = %v", names)
		}
		entries, err := view.Entries(ctx)
		if err != nil || len(entries) != 0 {
			t.Fatalf("an image with no record was listed as complete: %+v (%v)", entries, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MutateMedia(ctx, func(media.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(staging); err == nil {
		t.Fatal("abandoned staging survived the next mutation")
	}
}

func TestMediaStagingRefusesAnImageBeyondItsLimit(t *testing.T) {
	store := mediaFixture(t)
	err := store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		_, err := tx.Stage(context.Background(), mediaPayload("too many bytes"), 4)
		return err
	})
	if err == nil {
		t.Fatal("an oversized image was staged")
	}
	entries, listErr := os.ReadDir(filepath.Join(store.options.Root, "media"))
	if listErr != nil || len(entries) != 0 {
		t.Fatalf("a refused staging left %v behind (%v)", entries, listErr)
	}
}

// A media claim is shared, so several contexts hold one at once and it blocks
// nothing but a change to the image it names.
func TestSharedMediaReservationsAreVisibleAndNeverConflict(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	other, sources := fixture(t)
	_ = other
	publish(t, store, "second", sources)
	scope := prerequisites.SetupContext{}
	publishControllerState(t, store, scope, completeControllerState(syntheticControllerState(t, scope)))
	for _, name := range []string{"example", "second"} {
		err := store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []prerequisites.HostReservation{{
				Context: name, Kind: "media", Service: "media",
				Keys: []string{managedos.MediaReservationKey("demo.iso")}, Shared: true,
			}})
		})
		if err != nil {
			t.Fatalf("%s reservation failed: %#v", name, diagnostics.Of(err))
		}
	}
	err := store.ReadMedia(ctx, func(view media.View) error {
		frozen, err := view.Frozen(ctx)
		if err != nil {
			return err
		}
		if !slices.Equal(frozen, []string{"demo.iso"}) {
			t.Fatalf("frozen = %v", frozen)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMediaRefusesAStoreThatWasNeverInitialized(t *testing.T) {
	store, _ := fixture(t)
	if err := store.ReadMedia(context.Background(), func(media.View) error { return nil }); err == nil {
		t.Fatal("an absent store produced a media view")
	}
}
