// Package storecontract is the shared contract suite for media.Store. Every
// implementation's tests run it, the in-memory doubles included, so a double
// cannot accept what the Workspace adapter refuses. No production package
// imports it.
package storecontract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

// Within provides one fresh store that holds no image and fails the test when
// it cannot.
type Within func(t *testing.T) media.Store

const (
	image    = "demo.iso"
	other    = "other.iso"
	original = "installer bytes"
	changed  = "replacement installer bytes"
)

// Verify holds one implementation to every clause, each against its own store.
// It follows the protocol a media add follows: a stage is claimed and a
// publication made under the store's exclusive hold, while the stage is
// filled, verified or retained holding none.
func Verify(t *testing.T, within Within) {
	t.Helper()
	for _, clause := range []struct {
		name  string
		check func(*testing.T, media.Store)
	}{
		{"a published image is an entry the view reads back", aPublishedImageIsAnEntryTheViewReadsBack},
		{"a live stage holds its name", aLiveStageHoldsItsName},
		{"a closed stage never published or retained leaves nothing", aClosedStageLeavesNothing},
		{"a stage is filled within its limit", aStageIsFilledWithinItsLimit},
		{"a publication needs a filled stage of its image and a record of its bytes", aPublicationNeedsAFilledStageAndItsRecord},
		{"a publication replaces an image only when asked", aPublicationReplacesOnlyWhenAsked},
		{"a retaining stage is listed and a pinned stage adopts it", aRetainingStageIsListedAndAdopted},
		{"any other pin discards a retained stage", anyOtherPinDiscardsARetainedStage},
		{"a deletion removes the image, its record and its retained stage", aDeletionRemovesEverything},
	} {
		t.Run(clause.name, func(t *testing.T) {
			store := within(t)
			if store == nil {
				t.Fatal("the runner provided no store")
			}
			clause.check(t, store)
		})
	}
}

func aPublishedImageIsAnEntryTheViewReadsBack(t *testing.T, store media.Store) {
	stage := claim(t, store, image, "")
	if staged := fill(t, stage, original); staged.Size != int64(len(original)) || staged.SHA256 != digest(original) {
		t.Fatalf("the stage reported %+v for %q", staged, original)
	}
	succeeds(t, publish(store, image, stage, original, false), "a publication")
	closes(t, stage)
	holds(t, store, []managedos.MediaEntry{entry(image, original)}, nil)
}

func aLiveStageHoldsItsName(t *testing.T, store media.Store) {
	first := claim(t, store, image, "")
	refuses(t, store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		_, err := tx.Stage(context.Background(), image, "")
		return err
	}), "a second stage of a live stage's name")
	closes(t, claim(t, store, other, ""))
	closes(t, first)
	closes(t, claim(t, store, image, ""))
	holds(t, store, nil, nil)
}

func aClosedStageLeavesNothing(t *testing.T, store media.Store) {
	stage := claim(t, store, image, "")
	fill(t, stage, original)
	closes(t, stage)
	holds(t, store, nil, nil)
	closes(t, claim(t, store, image, ""))
}

func aStageIsFilledWithinItsLimit(t *testing.T, store media.Store) {
	stage := claim(t, store, image, "")
	if staged, err := stage.Fill(context.Background(), payload(original), int64(len(original))-1); err == nil {
		t.Fatalf("a stage filled beyond its limit reported %+v", staged)
	}
	closes(t, stage)
	holds(t, store, nil, nil)
}

func aPublicationNeedsAFilledStageAndItsRecord(t *testing.T, store media.Store) {
	stage := claim(t, store, image, "")
	refuses(t, publish(store, image, stage, original, false), "a publication of an unfilled stage")
	fill(t, stage, original)
	refuses(t, publish(store, other, stage, original, false), "a publication of another image's stage")
	refuses(t, publish(store, image, stage, changed, false), "a publication whose record states other bytes")
	holds(t, store, nil, nil)
	succeeds(t, publish(store, image, stage, original, false), "a publication of the filled stage")
	closes(t, stage)
	refuses(t, publish(store, image, stage, original, true), "a publication of a closed stage")
	holds(t, store, []managedos.MediaEntry{entry(image, original)}, nil)
}

func aPublicationReplacesOnlyWhenAsked(t *testing.T, store media.Store) {
	add(t, store, image, original)
	stage := claim(t, store, image, "")
	fill(t, stage, changed)
	refuses(t, publish(store, image, stage, changed, false), "a publication over a stored image")
	holds(t, store, []managedos.MediaEntry{entry(image, original)}, nil)
	succeeds(t, publish(store, image, stage, changed, true), "a replacement")
	closes(t, stage)
	holds(t, store, []managedos.MediaEntry{entry(image, changed)}, nil)
}

func aRetainingStageIsListedAndAdopted(t *testing.T, store media.Store) {
	retain(t, store, image, original)
	holds(t, store, nil, []managedos.MediaEntry{entry(image, original)})
	adopted := claim(t, store, image, digest(original))
	retained, found := adopted.Retained()
	if !found || retained != entry(image, original) {
		t.Fatalf("a pinned stage adopted %+v (%t), want %+v", retained, found, entry(image, original))
	}
	staged, err := adopted.Verify(context.Background())
	if err != nil || staged.Size != int64(len(original)) || staged.SHA256 != digest(original) {
		t.Fatalf("the adopted stage verified as %+v (%v)", staged, err)
	}
	succeeds(t, publish(store, image, adopted, original, false), "a publication of the adopted stage")
	closes(t, adopted)
	holds(t, store, []managedos.MediaEntry{entry(image, original)}, nil)
}

func anyOtherPinDiscardsARetainedStage(t *testing.T, store media.Store) {
	for _, pin := range []string{"", digest(changed)} {
		retain(t, store, image, original)
		stage := claim(t, store, image, pin)
		if retained, found := stage.Retained(); found {
			t.Fatalf("a stage pinned to %q adopted %+v", pin, retained)
		}
		closes(t, stage)
		holds(t, store, nil, nil)
	}
}

func aDeletionRemovesEverything(t *testing.T, store media.Store) {
	add(t, store, image, original)
	retain(t, store, image, changed)
	add(t, store, other, changed)
	holds(t, store, []managedos.MediaEntry{entry(image, original), entry(other, changed)}, []managedos.MediaEntry{entry(image, changed)})
	succeeds(t, store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		return tx.Delete(context.Background(), image)
	}), "a deletion")
	holds(t, store, []managedos.MediaEntry{entry(other, changed)}, nil)
}

type body struct{ *bytes.Reader }

func (body) Close() error { return nil }

func payload(data string) media.Payload { return body{bytes.NewReader([]byte(data))} }

func digest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func entry(name, data string) managedos.MediaEntry {
	return managedos.MediaEntry{Name: name, Size: int64(len(data)), SHA256: digest(data), Source: "file:///images/" + name, Added: "2026-09-15T09:00:00Z"}
}

func record(t *testing.T, name, data string) []byte {
	t.Helper()
	encoded, err := managedos.EncodeMediaRecord(entry(name, data))
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func claim(t *testing.T, store media.Store, name, pin string) media.Stage {
	t.Helper()
	var stage media.Stage
	if err := store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		var err error
		stage, err = tx.Stage(context.Background(), name, pin)
		return err
	}); err != nil || stage == nil {
		t.Fatalf("a stage of %s pinned to %q = %v (%v)", name, pin, stage, err)
	}
	t.Cleanup(func() { stage.Close() })
	return stage
}

func fill(t *testing.T, stage media.Stage, data string) media.Staged {
	t.Helper()
	staged, err := stage.Fill(context.Background(), payload(data), managedos.MaxMediaBytes)
	if err != nil {
		t.Fatalf("a stage could not be filled: %v", err)
	}
	return staged
}

func publish(store media.Store, name string, stage media.Stage, data string, replace bool) error {
	return store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		encoded, err := managedos.EncodeMediaRecord(entry(name, data))
		if err != nil {
			return err
		}
		return tx.Publish(context.Background(), name, stage, encoded, replace)
	})
}

func add(t *testing.T, store media.Store, name, data string) {
	t.Helper()
	stage := claim(t, store, name, "")
	fill(t, stage, data)
	succeeds(t, publish(store, name, stage, data, false), "a publication of "+name)
	closes(t, stage)
}

// retain keeps a filled stage of name beside its record, as a pinned add
// whose publication met another command's lock does.
func retain(t *testing.T, store media.Store, name, data string) {
	t.Helper()
	stage := claim(t, store, name, digest(data))
	fill(t, stage, data)
	succeeds(t, stage.Retain(context.Background(), record(t, name, data)), "a retention")
	closes(t, stage)
}

func closes(t *testing.T, stage media.Stage) {
	t.Helper()
	if err := stage.Close(); err != nil {
		t.Fatalf("a stage could not be closed: %v", err)
	}
}

// holds asserts what a read of the store reports: its complete entries by
// name, each observed at the size its record states, each image's occupied
// name, record and digest, and each retained stage as the entry it was
// retained with, in name order.
func holds(t *testing.T, store media.Store, entries []managedos.MediaEntry, retained []managedos.MediaEntry) {
	t.Helper()
	ctx := context.Background()
	if err := store.ReadMedia(ctx, func(view media.View) error {
		images, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		listed := make([]managedos.MediaEntry, 0, len(images))
		for _, image := range images {
			if image.Observed != image.Size {
				t.Fatalf("image %s is observed at %d bytes, want the %d its record states", image.Name, image.Observed, image.Size)
			}
			listed = append(listed, image.MediaEntry)
		}
		sorted := func(x, y managedos.MediaEntry) int { return strings.Compare(x.Name, y.Name) }
		if !slices.Equal(slices.SortedFunc(slices.Values(listed), sorted), slices.SortedFunc(slices.Values(entries), sorted)) {
			t.Fatalf("entries = %+v, want %+v", listed, entries)
		}
		names, err := view.Names(ctx)
		if err != nil {
			return err
		}
		want := []string{}
		for _, stored := range entries {
			want = append(want, stored.Name)
			published, found, err := view.Entry(ctx, stored.Name)
			if err != nil || !found || published != stored {
				t.Fatalf("the record of %s = %+v %t (%v), want %+v", stored.Name, published, found, err, stored)
			}
			if sum, err := view.Digest(ctx, stored.Name); err != nil || sum != stored.SHA256 {
				t.Fatalf("the digest of %s = %s (%v), want %s", stored.Name, sum, err, stored.SHA256)
			}
		}
		if !slices.Equal(slices.Sorted(slices.Values(names)), slices.Sorted(slices.Values(want))) {
			t.Fatalf("names = %v, want %v", names, want)
		}
		kept, err := view.Retained(ctx)
		if err != nil {
			return err
		}
		if !slices.Equal(kept, slices.SortedFunc(slices.Values(retained), sorted)) {
			t.Fatalf("retained = %+v, want %+v", kept, retained)
		}
		return nil
	}); err != nil {
		t.Fatalf("a media read failed: %v", err)
	}
}

func succeeds(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v", what, err)
	}
}

func refuses(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded", what)
	}
}
