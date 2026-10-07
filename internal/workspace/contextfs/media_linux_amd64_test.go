//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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

// claimStage claims a stage under the exclusive root lock, as media add does
// before it acquires anything.
func claimStage(t *testing.T, store *Store, name string) media.Stage {
	t.Helper()
	var stage media.Stage
	err := store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		var err error
		stage, err = tx.Stage(context.Background(), name, "")
		return err
	})
	if err != nil {
		t.Fatalf("media staging failed: %#v", diagnostics.Of(err))
	}
	t.Cleanup(func() { stage.Close() })
	return stage
}

// fillStage fills a stage holding no root lock, as media add acquires.
func fillStage(t *testing.T, store *Store, name, data string) media.Stage {
	t.Helper()
	stage := claimStage(t, store, name)
	if _, err := stage.Fill(context.Background(), mediaPayload(data), managedos.MaxMediaBytes); err != nil {
		t.Fatalf("media staging write failed: %#v", diagnostics.Of(err))
	}
	return stage
}

func publishStage(store *Store, name, data string, stage media.Stage, replace bool) error {
	return publishEntry(store, stage, managedos.MediaEntry{Name: name, Size: int64(len(data)), SHA256: mediaDigest(data)}, replace)
}

// publishEntry publishes a stage with a record stating entry's name, size and
// digest, whatever bytes the stage holds.
func publishEntry(store *Store, stage media.Stage, entry managedos.MediaEntry, replace bool) error {
	return store.MutateMedia(context.Background(), func(tx media.Transaction) error {
		entry.Source, entry.Added = "file:///images/"+entry.Name, "2026-09-15T09:00:00Z"
		record, err := managedos.EncodeMediaRecord(entry)
		if err != nil {
			return err
		}
		return tx.Publish(context.Background(), entry.Name, stage, record, replace)
	})
}

// substituteStage renames another private file over an image's stage, as any
// process of the store's owner can while no root lock is held.
func substituteStage(t *testing.T, store *Store, name, data string) {
	t.Helper()
	other := filepath.Join(filepath.Dir(store.options.Root), "substitute")
	if err := os.WriteFile(other, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, filepath.Join(store.options.Root, "media", mediaStageName(name))); err != nil {
		t.Fatal(err)
	}
}

// expectMedia asserts the complete entries a read lists, by name and digest,
// each holding exactly the bytes its record states.
func expectMedia(t *testing.T, store *Store, want map[string]string) {
	t.Helper()
	if err := mediaLists(store, want); err != nil {
		t.Fatalf("media read: %v", err)
	}
}

// mediaLists refuses unless a read lists exactly want, by name and digest,
// and every listed image holds the bytes its record states: a read lists a
// short image rather than refusing it, so a record published over short bytes
// fails here.
func mediaLists(store *Store, want map[string]string) error {
	var images []media.Image
	if err := store.ReadMedia(context.Background(), func(view media.View) error {
		var err error
		images, err = view.Entries(context.Background())
		return err
	}); err != nil {
		return err
	}
	if err := checkpointIntactImages(images); err != nil {
		return err
	}
	listed := map[string]string{}
	for _, image := range images {
		listed[image.Name] = image.SHA256
	}
	if !maps.Equal(listed, want) {
		return fmt.Errorf("entries = %+v", images)
	}
	return nil
}

func addMedia(t *testing.T, store *Store, name, data string, replace bool) {
	t.Helper()
	stage := fillStage(t, store, name, data)
	if err := publishStage(store, name, data, stage, replace); err != nil {
		t.Fatalf("media publication failed: %#v", diagnostics.Of(err))
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("closing a published stage failed: %#v", diagnostics.Of(err))
	}
}

func mediaDirectory(t *testing.T, store *Store) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.options.Root, "media"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
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
		if len(entries) != 1 || entries[0].Name != "demo.iso" || entries[0].Size != 15 || entries[0].Observed != 15 || entries[0].SHA256 != mediaDigest("installer bytes") {
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
		if len(entries) != 1 || entries[0].Size != 12 || entries[0].Observed != 12 || entries[0].SHA256 != mediaDigest("second image") {
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
	stage := fillStage(t, store, "demo.iso", "second")
	if err := publishStage(store, "demo.iso", "second", stage, false); err == nil {
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

// An interrupted publication leaves an image without its record, and the
// record's temporary file. Neither is adopted: the name stays occupied until a
// later publication replaces it, and the next mutation removes the temporary
// file.
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
	stage := claimStage(t, store, "demo.iso")
	if _, err := stage.Fill(context.Background(), mediaPayload("too many bytes"), 4); err == nil {
		t.Fatal("an oversized image was staged")
	}
	if err := publishStage(store, "demo.iso", "too many bytes", stage, false); err == nil {
		t.Fatal("a stage whose write failed was published")
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("discarding a refused stage failed: %#v", diagnostics.Of(err))
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a refused staging left %v behind", entries)
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
		reservations, err := view.Reservations(ctx)
		if err != nil {
			return err
		}
		if len(reservations) != 1 || !slices.Equal(reservations["demo.iso"], []string{"example", "second"}) {
			t.Fatalf("reservations = %v", reservations)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// diagnosedSource serves its bytes, then fails with the cause it names itself.
type diagnosedSource struct {
	data  *bytes.Reader
	cause error
}

func (s diagnosedSource) Read(buffer []byte) (int, error) {
	if n, _ := s.data.Read(buffer); n > 0 {
		return n, nil
	}
	return 0, s.cause
}

func (diagnosedSource) Close() error { return nil }

// A source that names why it failed, such as a download that met its transfer
// deadline, keeps that cause rather than a generic one.
func TestAFillKeepsTheSourcesOwnDiagnostic(t *testing.T) {
	store := mediaFixture(t)
	stage := claimStage(t, store, "demo.iso")
	cause := diagnostics.NewFailureWithRemediation("media.store", "the download from images.example did not finish within its 6-hour transfer deadline", "", "copy the image locally")
	_, err := stage.Fill(context.Background(), diagnosedSource{data: bytes.NewReader([]byte("installer")), cause: cause}, managedos.MaxMediaBytes)
	want := diagnostics.Of(cause)[0]
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != want.Message || reported[0].Remediation != want.Remediation {
		t.Fatalf("the fill reported %#v, want the source's own %#v", reported, want)
	}
}

func TestMediaRefusesAStoreThatWasNeverInitialized(t *testing.T) {
	store, _ := fixture(t)
	if err := store.ReadMedia(context.Background(), func(media.View) error { return nil }); err == nil {
		t.Fatal("an absent store produced a media view")
	}
}

func TestAMissingMediaCallbackIsRefusedAndCreatesNothing(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	for name, call := range map[string]func() error{
		"read":     func() error { return store.ReadMedia(ctx, nil) },
		"mutation": func() error { return store.MutateMedia(ctx, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			reported := diagnostics.Of(call())
			if len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, name+" callback is missing") {
				t.Fatalf("refusal = %#v, want context.state naming the missing %s callback", reported, name)
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(store.options.Root, mediaContainer)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused mutation left the media directory (%v)", err)
	}
}

// mediaSource serves an image and runs first once, on its first read, so a
// test can act while an acquisition is in flight.
type mediaSource struct {
	data  *bytes.Reader
	first func()
	once  sync.Once
}

func (s *mediaSource) Read(buffer []byte) (int, error) {
	if s.first != nil {
		s.once.Do(s.first)
	}
	return s.data.Read(buffer)
}

func (*mediaSource) Close() error { return nil }

type mediaAcquirer struct {
	source func() media.Payload
	origin string
	opens  atomic.Int32
}

func (a *mediaAcquirer) Origin(media.Source) (string, error) {
	if a.origin == "" {
		return "file:///images/source.iso", nil
	}
	return a.origin, nil
}

func (a *mediaAcquirer) Open(context.Context, media.Source) (media.Acquisition, error) {
	a.opens.Add(1)
	return media.Acquisition{Payload: a.source()}, nil
}

type mediaClock struct{}

func (mediaClock) Now() time.Time { return time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC) }

func mediaService(store *Store, acquirer *mediaAcquirer) media.Service {
	return media.New(store, acquirer, nil, mediaClock{})
}

func expectMediaRefusal(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %#v, want media.store naming %q", reported, message)
	}
}

// A download may take hours, so media add holds no root lock while it
// acquires: another command takes the exclusive root lock mid-download, and
// finds nothing of the image under any name.
func TestMediaAddHoldsNoRootLockWhileItAcquires(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	var during []error
	acquirer := &mediaAcquirer{source: func() media.Payload {
		return &mediaSource{data: bytes.NewReader([]byte("installer bytes")), first: func() {
			during = append(during, store.MutateMedia(ctx, func(tx media.Transaction) error {
				names, err := tx.Names(ctx)
				if err == nil && len(names) != 0 {
					err = fmt.Errorf("an image in acquisition is visible as %v", names)
				}
				return err
			}))
			_, err := store.View(ctx)
			during = append(during, err)
		}}
	}}
	result, err := mediaService(store, acquirer).Add(ctx, media.AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: mediaDigest("installer bytes"),
	})
	if err != nil {
		t.Fatalf("add: %#v", diagnostics.Of(err))
	}
	if len(during) != 2 || during[0] != nil || during[1] != nil {
		t.Fatalf("store commands during the download = %v", during)
	}
	if result.Outcome != "stored" || result.SHA256 != mediaDigest("installer bytes") {
		t.Fatalf("result = %+v", result)
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, []string{"demo.iso", "demo.iso.json"}) {
		t.Fatalf("media directory = %v", entries)
	}
}

// addMeetingALock adds data as name while a command takes the root lock during
// the download and still holds it when the image has arrived, so the add's
// publication meets that lock.
func addMeetingALock(t *testing.T, store *Store, name, data, pin string) error {
	t.Helper()
	ctx := context.Background()
	locked, unlock, held := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	acquirer := &mediaAcquirer{source: func() media.Payload {
		return &mediaSource{data: bytes.NewReader([]byte(data)), first: func() {
			go func() {
				held <- store.MutateMedia(ctx, func(media.Transaction) error {
					close(locked)
					<-unlock
					return nil
				})
			}()
			<-locked
		}}
	}}
	_, err := mediaService(store, acquirer).Add(ctx, media.AddMediaRequest{Name: name, SourceFile: "/images/" + name, SHA256: pin, SkipConfirmation: true})
	close(unlock)
	if err := <-held; err != nil {
		t.Fatalf("the command holding the root lock failed: %#v", diagnostics.Of(err))
	}
	return err
}

// An unpinned add whose publication meets another command's lock refuses with
// that lock's lifecycle.lease, removes its stage and publishes nothing.
func TestMediaAddThatCannotRetakeTheRootLockPublishesNothing(t *testing.T) {
	store := mediaFixture(t)
	err := addMeetingALock(t, store, "demo.iso", "installer bytes", "")
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.lease" {
		t.Fatalf("publication under a held root lock = %#v", reported)
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a refused publication left %v behind", entries)
	}
}

// Two adds of one name contend for one stage. The first to claim it acquires
// and publishes; the second refuses by name before it acquires anything, while
// an add of another name proceeds.
func TestConcurrentMediaAddsOfOneNameRefuseTheSecondBeforeItAcquires(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	first := &mediaAcquirer{source: func() media.Payload {
		return &mediaSource{data: bytes.NewReader([]byte("first image")), first: func() {
			close(started)
			<-release
		}}
	}}
	done := make(chan error, 1)
	go func() {
		_, err := mediaService(store, first).Add(ctx, media.AddMediaRequest{Name: "demo.iso", SourceFile: "/images/first.iso"})
		done <- err
	}()
	<-started
	second := &mediaAcquirer{source: func() media.Payload { return mediaPayload("second image") }}
	_, err := mediaService(store, second).Add(ctx, media.AddMediaRequest{Name: "demo.iso", SourceFile: "/images/second.iso", SkipConfirmation: true})
	expectMediaRefusal(t, err, "already acquiring image demo.iso")
	other := &mediaAcquirer{source: func() media.Payload { return mediaPayload("other image") }}
	if _, err := mediaService(store, other).Add(ctx, media.AddMediaRequest{Name: "other.iso", SourceFile: "/images/other.iso"}); err != nil {
		t.Fatalf("an add of another name failed: %#v", diagnostics.Of(err))
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the first add failed: %#v", diagnostics.Of(err))
	}
	if second.opens.Load() != 0 {
		t.Fatal("the refused add acquired its source")
	}
	err = store.ReadMedia(ctx, func(view media.View) error {
		entries, err := view.Entries(ctx)
		if err == nil && (len(entries) != 2 || entries[0].Name != "demo.iso" || entries[0].SHA256 != mediaDigest("first image") ||
			entries[0].Observed != entries[0].Size || entries[1].Observed != entries[1].Size) {
			err = fmt.Errorf("entries = %+v", entries)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries := mediaDirectory(t, store); len(entries) != 4 {
		t.Fatalf("media directory = %v", entries)
	}
}

// An add whose process dies leaves its stage behind with no lock held on it.
// The stage is never visible as an image, and the next media mutation removes
// it, including a new add of the same name.
func TestAnAbandonedStageIsInvisibleAndTheNextAddRemovesIt(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	stage := claimStage(t, store, "demo.iso")
	if _, err := stage.Fill(ctx, mediaPayload("partial"), managedos.MaxMediaBytes); err != nil {
		t.Fatal(err)
	}
	// The kernel releases a dead owner's descriptors, and its lock with them,
	// without removing anything.
	dead := stage.(*mediaStage)
	dead.release()
	dead.closed = true
	if entries := mediaDirectory(t, store); !slices.Equal(entries, []string{mediaStageName("demo.iso")}) {
		t.Fatalf("media directory = %v", entries)
	}
	listing, err := mediaService(store, &mediaAcquirer{}).List(ctx, media.ListMediaRequest{})
	if err != nil || len(listing.Media) != 0 {
		t.Fatalf("an abandoned stage was listed: %+v (%v)", listing, err)
	}
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	result, err := mediaService(store, acquirer).Add(ctx, media.AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: mediaDigest("installer bytes"),
	})
	if err != nil || result.Outcome != "stored" {
		t.Fatalf("add after an abandoned stage = %+v (%#v)", result, diagnostics.Of(err))
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, []string{"demo.iso", "demo.iso.json"}) {
		t.Fatalf("media directory = %v", entries)
	}
}

// A live stage is held by its owner, so another mutation's pruning keeps it.
func TestALiveStageSurvivesAnotherMediaMutation(t *testing.T) {
	store := mediaFixture(t)
	stage := claimStage(t, store, "demo.iso")
	addMedia(t, store, "other.iso", "other image", false)
	if _, err := os.Lstat(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso"))); err != nil {
		t.Fatalf("another mutation removed a live stage: %v", err)
	}
	if _, err := stage.Fill(context.Background(), mediaPayload("installer bytes"), managedos.MaxMediaBytes); err != nil {
		t.Fatal(err)
	}
	if err := publishStage(store, "other.iso", "installer bytes", stage, true); err == nil {
		t.Fatal("a stage was published under a name it did not claim")
	}
	if err := publishStage(store, "demo.iso", "installer bytes", stage, false); err != nil {
		t.Fatalf("publication: %#v", diagnostics.Of(err))
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if entries := mediaDirectory(t, store); len(entries) != 4 || slices.Contains(entries, mediaStageName("demo.iso")) {
		t.Fatalf("media directory = %v", entries)
	}
}

// A stage sits in the media directory while no root lock is held, so
// publication proves the stage is still the file its owner filled. A stage
// replaced meanwhile is refused before a replacement removes the image it would
// supersede.
func TestASubstitutedStageNeverReplacesTheStoredImage(t *testing.T) {
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "old image", false)
	stored := map[string][]byte{}
	for _, name := range []string{"demo.iso", "demo.iso.json"} {
		data, err := os.ReadFile(filepath.Join(store.options.Root, "media", name))
		if err != nil {
			t.Fatal(err)
		}
		stored[name] = data
	}
	stage := fillStage(t, store, "demo.iso", "new image")
	substituteStage(t, store, "demo.iso", "bad image")
	if err := publishStage(store, "demo.iso", "new image", stage, true); err == nil {
		t.Fatal("a substituted stage was published")
	}
	for name, want := range stored {
		data, err := os.ReadFile(filepath.Join(store.options.Root, "media", name))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("stored %s = %q (%v), want %q", name, data, err, want)
		}
	}
	expectMedia(t, store, map[string]string{"demo.iso": mediaDigest("old image")})
}

// A stage replaced, or rewritten in place at its size, after publication
// proved it but before its rename, is caught once renamed: its bytes are never
// given a record.
func TestAStageChangedDuringItsRenameIsNeverRecorded(t *testing.T) {
	for name, change := range map[string]func(*testing.T, *Store){
		"substituted":        func(t *testing.T, store *Store) { substituteStage(t, store, "demo.iso", "bad image") },
		"rewritten in place": func(t *testing.T, store *Store) { rewriteInPlace(t, store, "demo.iso", "bad image") },
	} {
		t.Run(name, func(t *testing.T) {
			store := mediaFixture(t)
			stage := fillStage(t, store, "demo.iso", "new image")
			fired := false
			store.fail = func(point string) error {
				if point == "before-media-rename" && !fired {
					fired = true
					change(t, store)
				}
				return nil
			}
			err := publishStage(store, "demo.iso", "new image", stage, false)
			store.fail = nil
			if !fired {
				t.Fatal("the publication never reached its rename; the case proves nothing")
			}
			if err == nil {
				t.Fatal("a stage changed during its rename was published")
			}
			if _, err := os.Lstat(filepath.Join(store.options.Root, "media", "demo.iso.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("changed bytes were given a record: %v", err)
			}
			expectMedia(t, store, map[string]string{})
		})
	}
}

// Publication proves the stage unchanged since Fill measured its bytes: bytes
// appended since are refused, never published under the image name.
func TestAStageThatGrewAfterItWasFilledIsNeverPublished(t *testing.T) {
	store := mediaFixture(t)
	stage := fillStage(t, store, "demo.iso", "installer bytes")
	file, err := os.OpenFile(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("!"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publishStage(store, "demo.iso", "installer bytes", stage, false); err == nil {
		t.Fatal("a stage that grew after it was filled was published")
	}
	expectMedia(t, store, map[string]string{})
	if err := stage.Close(); err != nil {
		t.Fatalf("discarding a refused stage failed: %#v", diagnostics.Of(err))
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a refused publication left %v behind", entries)
	}
}

// rewriteInPlace writes data over the start of an image's stage, at the size
// it holds, until the stage's status shows the write: a filesystem may stamp a
// write with a clock too coarse to tell it from the one before.
func rewriteInPlace(t *testing.T, store *Store, name, data string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(store.options.Root, "media", mediaStageName(name)), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var before syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &before); err != nil {
		t.Fatal(err)
	}
	after := before
	for deadline := time.Now().Add(time.Second); after.Mtim == before.Mtim && after.Ctim == before.Ctim; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a write in place never changed the stage's status")
		}
		if _, err := file.WriteAt([]byte(data), 0); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Fstat(int(file.Fd()), &after); err != nil {
			t.Fatal(err)
		}
		if after.Size != before.Size {
			t.Fatalf("the write in place changed the stage's size from %d to %d", before.Size, after.Size)
		}
	}
}

// rewrittenPublication is the media store with an image's stage rewritten in
// place, at its size, just before an add's second media mutation, the one
// that publishes: after Fill or Verify measured the stage's bytes. With locked,
// another command then holds the root lock during that mutation.
type rewrittenPublication struct {
	*Store
	t          *testing.T
	name, data string
	locked     bool
	count      int
}

func (s *rewrittenPublication) MutateMedia(ctx context.Context, callback func(media.Transaction) error) error {
	s.count++
	if s.count == 2 {
		rewriteInPlace(s.t, s.Store, s.name, s.data)
		if s.locked {
			release := holdRootLock(s.t, s.Store)
			defer release()
		}
	}
	return s.Store.MutateMedia(ctx, callback)
}

func expectChangedStageRefused(t *testing.T, rewritten *rewrittenPublication, err error) {
	t.Helper()
	if rewritten.count < 2 {
		t.Fatal("the add never reached its publication; the case proves nothing")
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "context.state" ||
		!strings.Contains(reported[0].Message, "changed before publication") {
		t.Fatalf("publication of a stage rewritten in place = %#v", reported)
	}
}

// Publication proves the stage unchanged since Fill measured its bytes, by the
// status taken then: bytes rewritten in place at the same size before the
// publication are refused, and the add publishes no image and no record.
func TestAStageRewrittenInPlaceAfterItWasFilledIsNeverPublished(t *testing.T) {
	store := mediaFixture(t)
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	rewritten := &rewrittenPublication{Store: store, t: t, name: "demo.iso", data: "INSTALLER BYTES"}
	_, err := media.New(rewritten, acquirer, nil, mediaClock{}).Add(context.Background(), pinnedAdd("demo.iso", "installer bytes"))
	expectChangedStageRefused(t, rewritten, err)
	expectMedia(t, store, map[string]string{})
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a refused publication left %v behind", entries)
	}
}

// A pinned add whose publication meets another command's lock retains only a
// stage unchanged since Fill measured its bytes: one rewritten in place at its
// size is removed, and the lock's refusal stands alone, never promising that a
// repetition publishes it without acquiring it again.
func TestAPinnedAddNeverRetainsAStageRewrittenInPlaceAfterItWasFilled(t *testing.T) {
	store := mediaFixture(t)
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	rewritten := &rewrittenPublication{Store: store, t: t, name: "demo.iso", data: "INSTALLER BYTES", locked: true}
	_, err := media.New(rewritten, acquirer, nil, mediaClock{}).Add(context.Background(), pinnedAdd("demo.iso", "installer bytes"))
	if rewritten.count < 2 {
		t.Fatal("the add never reached its publication; the case proves nothing")
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.lease" ||
		strings.Contains(reported[0].Message, "verified but not published") ||
		strings.Contains(reported[0].Remediation, "without acquiring it again") {
		t.Fatalf("publication of a rewritten stage under a held root lock = %#v", reported)
	}
	expectMedia(t, store, map[string]string{})
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a rewritten stage left %v behind", entries)
	}
}

// Publication installs only a record that describes the bytes Fill wrote, so a
// record naming another digest or another size is refused.
func TestMediaPublicationRefusesARecordThatDoesNotDescribeItsStage(t *testing.T) {
	data := "installer bytes"
	for _, entry := range []managedos.MediaEntry{
		{Name: "demo.iso", Size: int64(len(data)), SHA256: mediaDigest("different bytes")},
		{Name: "demo.iso", Size: int64(len(data)) + 1, SHA256: mediaDigest(data)},
	} {
		store := mediaFixture(t)
		stage := fillStage(t, store, "demo.iso", data)
		if err := publishEntry(store, stage, entry, false); err == nil {
			t.Fatalf("a record stating %d bytes with digest %s was published for other bytes", entry.Size, entry.SHA256)
		}
		expectMedia(t, store, map[string]string{})
		if err := stage.Close(); err != nil {
			t.Fatalf("discarding a refused stage failed: %#v", diagnostics.Of(err))
		}
		if entries := mediaDirectory(t, store); len(entries) != 0 {
			t.Fatalf("a refused publication left %v behind", entries)
		}
	}
}

// The bounds admit a full store with every stage retained beside its record:
// 64 images beside their records, with 16 retained replacements of them, still
// list, and only a seventeenth stage is refused, naming how to discard a
// retained one. An image's own retained stage never counts against its claim.
func TestMediaStagingRefusesBeyondItsBound(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	for index := range managedos.MaxMediaEntries {
		addMedia(t, store, fmt.Sprintf("image-%02d.iso", index), "installer bytes", false)
	}
	for index := range maxStagedMedia {
		retainStage(t, store, fmt.Sprintf("image-%02d.iso", index), "replacement bytes")
	}
	if entries := mediaDirectory(t, store); len(entries) != 2*managedos.MaxMediaEntries+2*maxStagedMedia {
		t.Fatalf("media directory holds %d entries", len(entries))
	}
	listing, err := mediaService(store, &mediaAcquirer{}).List(ctx, media.ListMediaRequest{})
	if err != nil {
		t.Fatalf("a full store with every stage retained cannot be listed: %#v", diagnostics.Of(err))
	}
	if len(listing.Media) != managedos.MaxMediaEntries {
		t.Fatalf("a full store with every stage retained listed %d images", len(listing.Media))
	}
	claim := func(name string) error {
		return store.MutateMedia(ctx, func(tx media.Transaction) error {
			stage, err := tx.Stage(ctx, name, "")
			if err == nil {
				stage.Close()
			}
			return err
		})
	}
	err = claim("overflow.iso")
	expectMediaRefusal(t, err, "maximum number of images")
	if remedy := diagnostics.Of(err)[0].Remediation; !strings.Contains(remedy, "bootwright media delete") {
		t.Fatalf("the bound's remedy names no way to discard a retained stage: %q", remedy)
	}
	if err := claim("image-00.iso"); err != nil {
		t.Fatalf("an image's own retained stage counted against its claim: %#v", diagnostics.Of(err))
	}
}

// retainStage leaves data retained for name, as a pinned add whose
// publication met another command's lock does.
func retainStage(t *testing.T, store *Store, name, data string) managedos.MediaEntry {
	t.Helper()
	stage := fillStage(t, store, name, data)
	entry := managedos.MediaEntry{Name: name, Size: int64(len(data)), SHA256: mediaDigest(data), Source: "https://example.test/" + name, Added: "2026-09-25T09:00:00Z"}
	record, err := managedos.EncodeMediaRecord(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Retain(context.Background(), record); err != nil {
		t.Fatalf("retaining %s failed: %#v", name, diagnostics.Of(err))
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	return entry
}

func sortedNames(names ...string) []string {
	slices.Sort(names)
	return names
}

// retainedFiles names the stage retained for name and its record.
func retainedFiles(name string) []string {
	return []string{mediaStageName(name), mediaStageName(name) + ".json"}
}

// holdRootLock takes the root lock through a descriptor of its own, as another
// Bootwright command holds it, and returns its release.
func holdRootLock(t *testing.T, store *Store) func() {
	t.Helper()
	holder, err := os.Open(store.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder.Close()
		t.Fatalf("the root lock could not be taken: %v", err)
	}
	return func() { unlockAndClose(holder) }
}

// lockedPublication is the media store with another command holding the root
// lock during one of its media mutations, counted from one.
type lockedPublication struct {
	*Store
	t         *testing.T
	at, count int
}

func (s *lockedPublication) MutateMedia(ctx context.Context, callback func(media.Transaction) error) error {
	s.count++
	if s.count == s.at {
		release := holdRootLock(s.t, s.Store)
		defer release()
	}
	return s.Store.MutateMedia(ctx, callback)
}

func pinnedAdd(name, data string) media.AddMediaRequest {
	return media.AddMediaRequest{Name: name, SourceFile: "/images/" + name, SHA256: mediaDigest(data), SkipConfirmation: true}
}

// A released stage is unlocked explicitly: its lock belongs to the open file
// description, which a descriptor another process shares keeps open, so a close
// alone would leave the abandoned stage held and every prune would keep it.
func TestAReleasedStageIsUnlockedEvenWhileAnotherDescriptorSharesIt(t *testing.T) {
	store := mediaFixture(t)
	stage := fillStage(t, store, "demo.iso", "partial").(*mediaStage)
	shared, _, errno := syscall.Syscall(syscall.SYS_FCNTL, stage.file.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	t.Cleanup(func() { syscall.Close(int(shared)) })
	stage.release()
	stage.closed = true
	if err := store.MutateMedia(context.Background(), func(media.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("a released stage survived the next mutation: %v", entries)
	}
}

// A media name whose record name would pass one 255-byte file name is refused
// before anything is acquired or created.
func TestAnOverLongMediaNameIsRefusedBeforeItIsAcquired(t *testing.T) {
	store := mediaFixture(t)
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	_, err := mediaService(store, acquirer).Add(context.Background(), pinnedAdd(strings.Repeat("a", 247)+".iso", "installer bytes"))
	expectMediaRefusal(t, err, "not a portable ISO basename")
	if acquirer.opens.Load() != 0 {
		t.Fatal("an over-long name was acquired")
	}
	if _, err := os.Lstat(filepath.Join(store.options.Root, "media")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an over-long name created the media directory: %v", err)
	}
}

// mediaPrompt runs other store commands while the operator is asked.
type mediaPrompt struct {
	store  *Store
	during []error
}

func (p *mediaPrompt) Confirm(ctx context.Context, _, _ string) error {
	p.during = append(p.during, p.store.MutateMedia(ctx, func(media.Transaction) error { return nil }))
	_, err := p.store.View(ctx)
	p.during = append(p.during, err)
	return nil
}

// A media confirmation holds no root lock, so another command's exclusive and
// shared holds both succeed while it waits, and the confirmed replacement and
// deletion then complete.
func TestMediaConfirmationsHoldNoRootLock(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "first", false)
	prompt := &mediaPrompt{store: store}
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("second image") }}
	service := media.New(store, acquirer, prompt, mediaClock{})
	replaced, err := service.Add(ctx, media.AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
	if err != nil || replaced.Outcome != "replaced" {
		t.Fatalf("confirmed replacement = %+v (%#v)", replaced, diagnostics.Of(err))
	}
	deleted, err := service.Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso"})
	if err != nil || deleted.Outcome != "deleted" {
		t.Fatalf("confirmed deletion = %+v (%#v)", deleted, diagnostics.Of(err))
	}
	if len(prompt.during) != 4 || slices.ContainsFunc(prompt.during, func(err error) bool { return err != nil }) {
		t.Fatalf("store commands during the prompts = %v", prompt.during)
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("media directory = %v", entries)
	}
}

// An image whose bytes no longer match its record is listed as a mismatch,
// and a confirmation reviews that image from its record alone, so its
// replacement and its deletion both still complete.
func TestADamagedImageStaysReplaceableAndDeletableWithConfirmation(t *testing.T) {
	ctx := context.Background()
	for _, verb := range []string{"add", "delete"} {
		t.Run(verb, func(t *testing.T) {
			store := mediaFixture(t)
			addMedia(t, store, "demo.iso", "first", false)
			file, err := os.OpenFile(filepath.Join(store.options.Root, "media", "demo.iso"), os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("!"); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if listed, err := mediaService(store, &mediaAcquirer{}).List(ctx, media.ListMediaRequest{}); err != nil || len(listed.Media) != 1 || listed.Media[0].Verified != "mismatch" {
				t.Fatalf("a damaged image was listed as %+v (%#v)", listed, diagnostics.Of(err))
			}
			acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("second image") }}
			service := media.New(store, acquirer, &mediaPrompt{store: store}, mediaClock{})
			if verb == "add" {
				_, err = service.Add(ctx, media.AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
			} else {
				_, err = service.Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso"})
			}
			if err != nil {
				t.Fatalf("a confirmed %s of a damaged image: %#v", verb, diagnostics.Of(err))
			}
		})
	}
}

// A pinned add whose publication meets another command's lock keeps the stage
// it verified beside the record it would have published, and says that
// repeating the command publishes it. Other media commands keep the pair and
// never list it.
func TestAPinnedAddThatCannotRetakeTheRootLockRetainsItsStage(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	err := addMeetingALock(t, store, "demo.iso", "installer bytes", mediaDigest("installer bytes"))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.lease" ||
		!strings.Contains(reported[0].Remediation, "without acquiring it again") {
		t.Fatalf("publication under a held root lock = %#v", reported)
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, retainedFiles("demo.iso")) {
		t.Fatalf("media directory = %v", entries)
	}
	want, err := managedos.EncodeMediaRecord(managedos.MediaEntry{
		Name: "demo.iso", Size: 15, SHA256: mediaDigest("installer bytes"), Source: "file:///images/source.iso", Added: "2026-09-26T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := os.ReadFile(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")+".json"))
	if err != nil || !bytes.Equal(record, want) {
		t.Fatalf("retained record = %q (%v), want %q", record, err, want)
	}
	other := &mediaAcquirer{source: func() media.Payload { return mediaPayload("other image") }}
	if _, err := mediaService(store, other).Add(ctx, pinnedAdd("other.iso", "other image")); err != nil {
		t.Fatalf("an add of another image: %#v", diagnostics.Of(err))
	}
	expectMedia(t, store, map[string]string{"other.iso": mediaDigest("other image")})
	if entries := mediaDirectory(t, store); !slices.Equal(entries, sortedNames(append(retainedFiles("demo.iso"), "other.iso", "other.iso.json")...)) {
		t.Fatalf("media directory = %v", entries)
	}
}

// A pinned add can retain its stage while the media mutation whose lock it met
// is pruning: that prune listed the directory before the record existed, and
// reaches the stage only once the add has released it. It still keeps the pair,
// so the repeated add publishes the image without acquiring it again, as the
// refusal promised.
func TestARetentionThatFinishesDuringAPruneKeepsItsPair(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	started, proceed, added := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	acquirer := &mediaAcquirer{source: func() media.Payload {
		return &mediaSource{data: bytes.NewReader([]byte("installer bytes")), first: func() {
			close(started)
			<-proceed
		}}
	}}
	go func() {
		_, err := mediaService(store, acquirer).Add(ctx, pinnedAdd("demo.iso", "installer bytes"))
		added <- err
	}()
	<-started
	var retention error
	var listed []string
	paused := false
	pruning := New(store.options)
	pruning.fail = func(point string) error {
		if point == string(checkpointBeforeMediaStagingPrune) && !paused {
			paused = true
			close(proceed)
			retention = <-added
			listed = mediaDirectory(t, store)
		}
		return nil
	}
	if err := pruning.MutateMedia(ctx, func(media.Transaction) error { return nil }); err != nil || !paused {
		t.Fatalf("the pruning mutation (paused %v) failed: %#v", paused, diagnostics.Of(err))
	}
	if reported := diagnostics.Of(retention); len(reported) != 1 || reported[0].Code != "lifecycle.lease" ||
		!strings.Contains(reported[0].Remediation, "without acquiring it again") {
		t.Fatalf("the add meeting the pruning lock = %#v", reported)
	}
	if !slices.Equal(listed, retainedFiles("demo.iso")) {
		t.Fatalf("while the prune was under way the media directory = %v", listed)
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, retainedFiles("demo.iso")) {
		t.Fatalf("after the prune the media directory = %v, want the retained pair", entries)
	}
	repeated := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	if result, err := mediaService(store, repeated).Add(ctx, pinnedAdd("demo.iso", "installer bytes")); err != nil || result.Outcome != "stored" {
		t.Fatalf("repeated add = %+v (%#v)", result, diagnostics.Of(err))
	}
	if opens := repeated.opens.Load(); opens != 0 {
		t.Fatalf("the repeated add acquired its image %d times", opens)
	}
}

// The repeated add adopts the retained stage, re-reads it and publishes it
// with the source it was retained with; its own source is never opened.
func TestARepeatedAddPublishesARetainedStageWithoutAcquiring(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	retained := retainStage(t, store, "demo.iso", "installer bytes")
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }, origin: "file:///images/elsewhere.iso"}
	result, err := mediaService(store, acquirer).Add(ctx, pinnedAdd("demo.iso", "installer bytes"))
	if err != nil || result.Outcome != "stored" || result.SHA256 != retained.SHA256 {
		t.Fatalf("repeated add = %+v (%#v)", result, diagnostics.Of(err))
	}
	if acquirer.opens.Load() != 0 {
		t.Fatal("the repeated add acquired its image again")
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, []string{"demo.iso", "demo.iso.json"}) {
		t.Fatalf("media directory = %v", entries)
	}
	data, err := os.ReadFile(filepath.Join(store.options.Root, "media", "demo.iso.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := managedos.DecodeMediaRecord(data, "demo.iso")
	if err != nil || entry.Source != retained.Source || entry.Added != "2026-09-26T09:00:00Z" {
		t.Fatalf("published record = %+v (%v)", entry, err)
	}
}

// A repeated add whose own publication meets another command's lock keeps the
// stage it adopted, so a later repetition still publishes it.
func TestARepeatedAddWhosePublicationMeetsALockAgainKeepsTheRetainedStage(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	retainStage(t, store, "demo.iso", "installer bytes")
	record, err := os.ReadFile(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	locked := &lockedPublication{Store: store, t: t, at: 2}
	_, err = media.New(locked, acquirer, nil, mediaClock{}).Add(ctx, pinnedAdd("demo.iso", "installer bytes"))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.lease" || !strings.Contains(reported[0].Remediation, "without acquiring it again") {
		t.Fatalf("a repeated publication under a held root lock = %#v", reported)
	}
	if entries := mediaDirectory(t, store); !slices.Equal(entries, retainedFiles("demo.iso")) {
		t.Fatalf("media directory = %v", entries)
	}
	kept, err := os.ReadFile(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")+".json"))
	if err != nil || !bytes.Equal(kept, record) {
		t.Fatalf("the retained record changed: %q (%v)", kept, err)
	}
	if _, err := mediaService(store, acquirer).Add(ctx, pinnedAdd("demo.iso", "installer bytes")); err != nil || acquirer.opens.Load() != 0 {
		t.Fatalf("the later repetition = %#v, acquisitions %d", diagnostics.Of(err), acquirer.opens.Load())
	}
	expectMedia(t, store, map[string]string{"demo.iso": mediaDigest("installer bytes")})
}

// An add of the image with another pin or none removes its retained stage
// before it claims, and acquires and publishes afresh.
func TestAnAddWithAnotherPinOrNoneDiscardsTheRetainedStage(t *testing.T) {
	for name, pin := range map[string]string{"another pin": mediaDigest("new image"), "no pin": ""} {
		t.Run(name, func(t *testing.T) {
			store := mediaFixture(t)
			retainStage(t, store, "demo.iso", "installer bytes")
			acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("new image") }}
			request := pinnedAdd("demo.iso", "new image")
			request.SHA256 = pin
			if _, err := mediaService(store, acquirer).Add(context.Background(), request); err != nil {
				t.Fatalf("add: %#v", diagnostics.Of(err))
			}
			if acquirer.opens.Load() != 1 {
				t.Fatalf("the add acquired %d times", acquirer.opens.Load())
			}
			expectMedia(t, store, map[string]string{"demo.iso": mediaDigest("new image")})
			if entries := mediaDirectory(t, store); !slices.Equal(entries, []string{"demo.iso", "demo.iso.json"}) {
				t.Fatalf("media directory = %v", entries)
			}
		})
	}
}

// A retained stage rewritten in place, same size, since it was retained is
// re-read before publication: the add refuses, publishes nothing and removes
// the pair.
func TestARetainedStageWhoseBytesChangedIsNeverPublished(t *testing.T) {
	store := mediaFixture(t)
	retainStage(t, store, "demo.iso", "installer bytes")
	file, err := os.OpenFile(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("INSTALLER BYTES"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	_, err = mediaService(store, acquirer).Add(context.Background(), pinnedAdd("demo.iso", "installer bytes"))
	expectMediaRefusal(t, err, "no longer holds the bytes it verified")
	expectMedia(t, store, map[string]string{})
	if entries := mediaDirectory(t, store); len(entries) != 0 || acquirer.opens.Load() != 0 {
		t.Fatalf("media directory = %v, acquisitions %d", entries, acquirer.opens.Load())
	}
}

// An adopted stage is proved unchanged since Verify re-read it: bytes rewritten
// in place at the same size after that re-read are refused and the add
// publishes no image and no record. The pair stays retained, and the
// repetition's own re-read finds the other bytes and removes it.
func TestAnAdoptedStageRewrittenInPlaceAfterItsVerificationIsNeverPublished(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	retainStage(t, store, "demo.iso", "installer bytes")
	acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload("installer bytes") }}
	rewritten := &rewrittenPublication{Store: store, t: t, name: "demo.iso", data: "INSTALLER BYTES"}
	_, err := media.New(rewritten, acquirer, nil, mediaClock{}).Add(ctx, pinnedAdd("demo.iso", "installer bytes"))
	expectChangedStageRefused(t, rewritten, err)
	expectMedia(t, store, map[string]string{})
	if entries := mediaDirectory(t, store); !slices.Equal(entries, retainedFiles("demo.iso")) || acquirer.opens.Load() != 0 {
		t.Fatalf("media directory = %v, acquisitions %d", entries, acquirer.opens.Load())
	}
	_, err = mediaService(store, acquirer).Add(ctx, pinnedAdd("demo.iso", "installer bytes"))
	expectMediaRefusal(t, err, "no longer holds the bytes it verified")
	if entries := mediaDirectory(t, store); len(entries) != 0 || acquirer.opens.Load() != 0 {
		t.Fatalf("media directory = %v, acquisitions %d", entries, acquirer.opens.Load())
	}
}

// media delete removes a retained stage with its record, refuses by name while
// an add holds that stage, and keeps it while the image is frozen.
func TestMediaDeleteDiscardsARetainedStage(t *testing.T) {
	ctx := context.Background()
	remove := func(store *Store) (*media.MutationResult, error) {
		return mediaService(store, &mediaAcquirer{}).Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true})
	}
	t.Run("retained only", func(t *testing.T) {
		store := mediaFixture(t)
		retainStage(t, store, "demo.iso", "installer bytes")
		result, err := remove(store)
		if err != nil || result.Outcome != "deleted" {
			t.Fatalf("delete = %+v (%#v)", result, diagnostics.Of(err))
		}
		if entries := mediaDirectory(t, store); len(entries) != 0 {
			t.Fatalf("media directory = %v", entries)
		}
	})
	t.Run("held by an adopting add", func(t *testing.T) {
		store := mediaFixture(t)
		retainStage(t, store, "demo.iso", "installer bytes")
		var adopter media.Stage
		if err := store.MutateMedia(ctx, func(tx media.Transaction) error {
			var err error
			adopter, err = tx.Stage(ctx, "demo.iso", mediaDigest("installer bytes"))
			return err
		}); err != nil {
			t.Fatalf("adoption: %#v", diagnostics.Of(err))
		}
		_, err := remove(store)
		expectMediaRefusal(t, err, "already acquiring image demo.iso")
		if err := adopter.Close(); err != nil {
			t.Fatal(err)
		}
		if entries := mediaDirectory(t, store); !slices.Equal(entries, retainedFiles("demo.iso")) {
			t.Fatalf("media directory = %v", entries)
		}
	})
	t.Run("frozen", func(t *testing.T) {
		store := mediaFixture(t)
		addMedia(t, store, "demo.iso", "first", false)
		retainStage(t, store, "demo.iso", "installer bytes")
		scope := prerequisites.SetupContext{}
		publishControllerState(t, store, scope, completeControllerState(syntheticControllerState(t, scope)))
		if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []prerequisites.HostReservation{{
				Context: "example", Kind: "media", Service: "media",
				Keys: []string{managedos.MediaReservationKey("demo.iso")}, Shared: true,
			}})
		}); err != nil {
			t.Fatalf("reservation: %#v", diagnostics.Of(err))
		}
		_, err := remove(store)
		expectMediaRefusal(t, err, "image demo.iso is reserved by context example")
		want := sortedNames(append(retainedFiles("demo.iso"), "demo.iso", "demo.iso.json")...)
		if entries := mediaDirectory(t, store); !slices.Equal(entries, want) {
			t.Fatalf("media directory = %v", entries)
		}
	})
}

// A retained record whose stage is gone is removed by the next media mutation.
func TestAPrunedRetainedRecordWithoutItsStageIsRemoved(t *testing.T) {
	store := mediaFixture(t)
	retainStage(t, store, "demo.iso", "installer bytes")
	if err := os.Remove(filepath.Join(store.options.Root, "media", mediaStageName("demo.iso"))); err != nil {
		t.Fatal(err)
	}
	if err := store.MutateMedia(context.Background(), func(media.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("media directory = %v", entries)
	}
}

// A record torn by a killed retention does not make a retained pair, so the
// next media mutation removes the record and its abandoned stage.
func TestATornRetainedRecordLeavesItsStageAbandoned(t *testing.T) {
	store := mediaFixture(t)
	retainStage(t, store, "demo.iso", "installer bytes")
	record := filepath.Join(store.options.Root, "media", mediaStageName("demo.iso")+".json")
	info, err := os.Stat(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(record, info.Size()/2); err != nil {
		t.Fatal(err)
	}
	listing, err := mediaService(store, &mediaAcquirer{}).List(context.Background(), media.ListMediaRequest{})
	if err != nil || len(listing.Media) != 0 {
		t.Fatalf("listing = %+v (%v)", listing, err)
	}
	if err := store.MutateMedia(context.Background(), func(media.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if entries := mediaDirectory(t, store); len(entries) != 0 {
		t.Fatalf("media directory = %v", entries)
	}
}

// An image shortened after its publication is listed at the size its bytes
// have now rather than refusing the whole listing, so the listing marks it a
// mismatch and still lists every other image.
func TestAShortImageIsListedAsAMismatchNotARefusal(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	addMedia(t, store, "other.iso", "other bytes", false)
	if err := os.Truncate(filepath.Join(store.options.Root, "media", "demo.iso"), 4); err != nil {
		t.Fatal(err)
	}
	err := store.ReadMedia(ctx, func(view media.View) error {
		images, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		observed := map[string][2]int64{}
		for _, image := range images {
			observed[image.Name] = [2]int64{image.Size, image.Observed}
		}
		if want := map[string][2]int64{"demo.iso": {15, 4}, "other.iso": {11, 11}}; !maps.Equal(observed, want) {
			t.Fatalf("the record and observed sizes = %v, want %v", observed, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read: %#v", diagnostics.Of(err))
	}
	listing, err := media.New(store, &mediaAcquirer{}, nil, mediaClock{}).List(ctx, media.ListMediaRequest{})
	if err != nil || len(listing.Media) != 2 {
		t.Fatalf("listing = %+v (%#v)", listing, diagnostics.Of(err))
	}
	if demo, other := listing.Media[0], listing.Media[1]; demo.Name != "demo.iso" || demo.Verified != "mismatch" || other.Name != "other.iso" || other.Verified != "" {
		t.Fatalf("listing = %+v", listing.Media)
	}
	checked, err := media.New(store, &mediaAcquirer{}, nil, mediaClock{}).List(ctx, media.ListMediaRequest{Checksums: true})
	if err != nil || len(checked.Media) != 2 || checked.Media[0].Verified != "mismatch" || checked.Media[0].Computed != mediaDigest("inst") ||
		checked.Media[1].Verified != "ok" || checked.Media[1].Computed != mediaDigest("other bytes") {
		t.Fatalf("checked listing = %+v (%#v)", checked, diagnostics.Of(err))
	}
}

// A record published over short bytes is torn, and a read lists it rather
// than refusing, so every helper that proves a publication converged checks
// the bytes against the record itself: none takes the torn image for the one
// it published.
func TestThePublicationChecksRefuseATornImage(t *testing.T) {
	ctx := context.Background()
	store := mediaFixture(t)
	addMedia(t, store, "demo.iso", "installer bytes", false)
	want := map[string]string{"demo.iso": mediaDigest("installer bytes")}
	if err := mediaLists(store, want); err != nil {
		t.Fatalf("an intact image was refused: %v", err)
	}
	if err := os.Truncate(filepath.Join(store.options.Root, "media", "demo.iso"), 4); err != nil {
		t.Fatal(err)
	}
	torn := "image demo.iso holds 4 bytes, but its record states 15"
	if err := mediaLists(store, want); err == nil || err.Error() != torn {
		t.Errorf("the publication check took a torn image for its publication: %v", err)
	}
	if err := checkpointMediaHolds(ctx, store, want); err == nil || err.Error() != torn {
		t.Errorf("the checkpoint settled check took a torn image for its publication: %v", err)
	}
	if digest, _, err := checkpointMediaImage(ctx, store); err == nil || err.Error() != torn {
		t.Errorf("the checkpoint retry took a torn image for its publication: digest %q (%v)", digest, err)
	}
}
