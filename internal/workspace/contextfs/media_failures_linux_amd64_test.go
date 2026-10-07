//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

// canceledSource serves two bytes, then cancels its acquisition and fails the
// way a body read fails once its connection is torn down.
type canceledSource struct {
	cancel context.CancelFunc
	served bool
}

func (s *canceledSource) Read(buffer []byte) (int, error) {
	if !s.served {
		s.served = true
		return copy(buffer, "in"), nil
	}
	s.cancel()
	return 0, errors.New("read tcp: use of closed network connection")
}

func (*canceledSource) Close() error { return nil }

// A copy canceled mid-body is reported as the cancellation, never as a source
// that could not be read in full, and leaves nothing behind.
func TestAFillCanceledMidBodyReportsTheCancellation(t *testing.T) {
	store := mediaFixture(t)
	stage := claimStage(t, store, "demo.iso")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := stage.Fill(ctx, &canceledSource{cancel: cancel}, managedos.MaxMediaBytes)
	if !errors.Is(err, context.Canceled) || len(diagnostics.Of(err)) != 0 {
		t.Fatalf("a canceled fill reported %v (%#v), want the cancellation", err, diagnostics.Of(err))
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("closing a canceled stage: %v", err)
	}
	if left := mediaDirectory(t, store); len(left) != 0 {
		t.Fatalf("a canceled fill left %v", left)
	}
}

// checkReporter runs during on each check event of a listing.
type checkReporter struct {
	during func(media.ProgressEvent)
}

func (r *checkReporter) ReportProgress(_ context.Context, event media.ProgressEvent) {
	if r.during != nil {
		r.during(event)
	}
}

func checksumListing(t *testing.T, store *Store, during func(media.ProgressEvent)) *media.ListResult {
	t.Helper()
	listed, err := media.New(store, &mediaAcquirer{}, nil, mediaClock{}).Reporting(&checkReporter{during: during}, nil).
		List(context.Background(), media.ListMediaRequest{Checksums: true})
	if err != nil || len(listed.Media) != 2 {
		t.Fatalf("checksum listing = %+v (%#v)", listed, diagnostics.Of(err))
	}
	return listed
}

func plainListing(t *testing.T, store *Store) *media.ListResult {
	t.Helper()
	listed, err := media.New(store, &mediaAcquirer{}, nil, mediaClock{}).List(context.Background(), media.ListMediaRequest{})
	if err != nil || len(listed.Media) != 2 {
		t.Fatalf("plain listing = %+v (%#v)", listed, diagnostics.Of(err))
	}
	return listed
}

func runningCheck(event media.ProgressEvent, name string) bool {
	return event.Check && event.Status == "running" && event.Label == "Verify "+name
}

func expectFailedRow(t *testing.T, row media.MediaRow, name, failure string) {
	t.Helper()
	if row.Name != name || row.Verified != "failed" || row.Failure != failure || row.Computed != "" {
		t.Fatalf("row = %+v, want %s failed with %q", row, name, failure)
	}
}

// While a checksum listing reads its images it holds no root lock, so an
// exclusive mutation taken during each check succeeds.
func TestAChecksumListingHoldsNoRootLockWhileItHashes(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	mutations := 0
	listed := checksumListing(t, store, func(event media.ProgressEvent) {
		if event.Status != "running" {
			return
		}
		mutations++
		if err := store.MutateMedia(context.Background(), func(media.Transaction) error { return nil }); err != nil {
			t.Errorf("a mutation during a check was refused: %#v", diagnostics.Of(err))
		}
	})
	if mutations != 2 || listed.Media[0].Verified != "ok" || listed.Media[1].Verified != "ok" {
		t.Fatalf("mutations %d, listing = %+v", mutations, listed.Media)
	}
}

// An image deleted while it is read is listed failed, and the others verify.
func TestAnImageDeletedWhileItIsHashedIsListedFailed(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	listed := checksumListing(t, store, func(event media.ProgressEvent) {
		if !runningCheck(event, "demo.iso") {
			return
		}
		if _, err := mediaService(store, &mediaAcquirer{}).Delete(context.Background(), media.DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true}); err != nil {
			t.Errorf("a deletion during a check was refused: %#v", diagnostics.Of(err))
		}
	})
	expectFailedRow(t, listed.Media[0], "demo.iso", imageReplaced)
	if listed.Media[1].Verified != "ok" {
		t.Fatalf("the other image = %+v", listed.Media[1])
	}
}

// An image extended while it is read is listed failed, not a mismatch, and
// the others verify.
func TestAnImageChangedWhileItIsHashedIsListedFailed(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	listed := checksumListing(t, store, func(event media.ProgressEvent) {
		if !runningCheck(event, "demo.iso") {
			return
		}
		file, err := os.OpenFile(filepath.Join(store.options.Root, "media", "demo.iso"), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if _, err := file.WriteString(" appended"); err != nil {
			t.Error(err)
		}
	})
	expectFailedRow(t, listed.Media[0], "demo.iso", imageChanged)
	if listed.Media[1].Verified != "ok" {
		t.Fatalf("the other image = %+v", listed.Media[1])
	}
}

// replaceRecord writes data in place of an image's record, as an actor other
// than Bootwright could.
func replaceRecord(t *testing.T, store *Store, name, data string) {
	t.Helper()
	path := filepath.Join(store.options.Root, "media", name+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// A record that does not decode lists its image failed, with only its name,
// beside every other image, with or without checksums.
func TestAnUndecodableRecordIsListedFailedBesideTheOthers(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	replaceRecord(t, store, "demo.iso", "{}\n")
	plain := plainListing(t, store)
	expectFailedRow(t, plain.Media[0], "demo.iso", recordUndecodable)
	if plain.Media[0].SHA256 != "" || plain.Media[1].Verified != "" {
		t.Fatalf("plain listing = %+v", plain.Media)
	}
	checked := checksumListing(t, store, nil)
	expectFailedRow(t, checked.Media[0], "demo.iso", recordUndecodable)
	if checked.Media[0].SHA256 != "" || checked.Media[1].Verified != "ok" {
		t.Fatalf("checksum listing = %+v", checked.Media)
	}
}

// A record whose file is not private lists its image failed.
func TestAnUnsafeRecordIsListedFailed(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	if err := os.Chmod(filepath.Join(store.options.Root, "media", "demo.iso.json"), 0644); err != nil {
		t.Fatal(err)
	}
	plain := plainListing(t, store)
	expectFailedRow(t, plain.Media[0], "demo.iso", recordUnreadable)
	if plain.Media[0].SHA256 != "" || plain.Media[1].Verified != "" {
		t.Fatalf("plain listing = %+v", plain.Media)
	}
}

// An image whose file is not private keeps its record's fields and is listed
// failed; checksums never read it, and every other image still verifies.
func TestAnUnsafeImageIsListedFailedBesideTheOthers(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	if err := os.Chmod(filepath.Join(store.options.Root, "media", "demo.iso"), 0644); err != nil {
		t.Fatal(err)
	}
	plain := plainListing(t, store)
	expectFailedRow(t, plain.Media[0], "demo.iso", imageUnsafe)
	if plain.Media[0].SHA256 != mediaDigest("installer bytes") || plain.Media[0].Size != 15 || plain.Media[1].Verified != "" {
		t.Fatalf("plain listing = %+v", plain.Media)
	}
	checked := checksumListing(t, store, nil)
	expectFailedRow(t, checked.Media[0], "demo.iso", imageUnsafe)
	if checked.Media[1].Verified != "ok" {
		t.Fatalf("checksum listing = %+v", checked.Media)
	}
}
