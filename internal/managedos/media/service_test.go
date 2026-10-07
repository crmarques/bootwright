package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
)

type fakeStore struct {
	entries []managedos.MediaEntry
	// observed is the size an image's bytes have now, when it differs from
	// its record.
	observed     map[string]int64
	occupied     []string
	reservations map[string][]string
	digests      map[string]string
	// retained is the entry a stage kept for each image, and retainedBytes
	// what that stage holds now.
	retained      map[string]managedos.MediaEntry
	retainedBytes map[string][]byte
	staged        []byte
	// published records the exact publication the service asked for.
	published  []byte
	replaced   bool
	deleted    string
	stageError error
	// verifyError fails each adopted stage's re-read, and digestError each
	// full read of a stored image.
	verifyError error
	digestError error
	stages      int
	writes      int
	closed      int
	// kept records each record a stage was retained with.
	kept [][]byte
	// mutations counts MutateMedia calls, and busyAt makes that call meet
	// another command's lock.
	mutations int
	busyAt    int
	// locked is true while a callback holds the store's root lock.
	locked bool
	// duringFill changes the store while an image is being acquired, and
	// afterRead once a read has released the root lock.
	duringFill func(*fakeStore)
	afterRead  func(*fakeStore)
	// live names every image a stage that is still open holds.
	live map[string]bool
	// failures lists an image the store cannot read, by name, with its cause.
	failures map[string]string
	// holdError fails the hold of an image. holdLocked and hashLocked record
	// whether the root lock was held at each hold and each full read, and
	// heldClosed counts every handle released.
	holdError  map[string]error
	holdLocked []bool
	hashLocked []bool
	heldClosed int
}

// failedImage is the cause a store gives for one image it cannot read.
type failedImage struct{ reason string }

func (e failedImage) Error() string      { return e.reason }
func (failedImage) Is(target error) bool { return target == ErrImageFailed }
func imageFailed(reason string) error    { return failedImage{reason: reason} }

// fakeHeld is one image a listing held.
type fakeHeld struct {
	store  *fakeStore
	name   string
	closed bool
}

func (h *fakeHeld) Digest(ctx context.Context) (string, error) {
	h.store.hashLocked = append(h.store.hashLocked, h.store.locked)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if h.store.digestError != nil {
		return "", h.store.digestError
	}
	return h.store.digests[h.name], nil
}

func (h *fakeHeld) Close() error {
	if !h.closed {
		h.closed = true
		h.store.heldClosed++
	}
	return nil
}

func (s *fakeStore) ReadMedia(ctx context.Context, callback func(View) error) error {
	s.locked = true
	err := callback(s)
	s.locked = false
	if s.afterRead != nil {
		s.afterRead(s)
	}
	return err
}

func (s *fakeStore) MutateMedia(ctx context.Context, callback func(Transaction) error) error {
	s.mutations++
	if s.mutations == s.busyAt {
		return fmt.Errorf("root lock held: %w", ErrBusy)
	}
	s.locked = true
	defer func() { s.locked = false }()
	return callback(s)
}

func (s *fakeStore) Entries(context.Context) ([]Image, error) {
	images := []Image{}
	for _, entry := range s.entries {
		size, changed := s.observed[entry.Name]
		if !changed {
			size = entry.Size
		}
		images = append(images, Image{MediaEntry: entry, Observed: size})
	}
	for name, failure := range s.failures {
		images = append(images, Image{MediaEntry: managedos.MediaEntry{Name: name}, Failure: failure})
	}
	return images, nil
}

func (s *fakeStore) Names(context.Context) ([]string, error) { return s.occupied, nil }
func (s *fakeStore) Reservations(context.Context) (map[string][]string, error) {
	return s.reservations, nil
}

func (s *fakeStore) Hold(_ context.Context, name string) (Held, error) {
	s.holdLocked = append(s.holdLocked, s.locked)
	if err := s.holdError[name]; err != nil {
		return nil, err
	}
	return &fakeHeld{store: s, name: name}, nil
}

func (s *fakeStore) Entry(_ context.Context, name string) (managedos.MediaEntry, bool, error) {
	for _, entry := range s.entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return managedos.MediaEntry{}, false, nil
}

func (s *fakeStore) Retained(context.Context) ([]managedos.MediaEntry, error) {
	kept := []managedos.MediaEntry{}
	for _, entry := range s.retained {
		kept = append(kept, entry)
	}
	slices.SortFunc(kept, func(x, y managedos.MediaEntry) int { return strings.Compare(x.Name, y.Name) })
	return kept, nil
}

// Stage refuses a name a live stage holds, and adopts an image's retained stage
// only for its recorded digest, otherwise discarding it, as the store's
// contract requires.
func (s *fakeStore) Stage(_ context.Context, name, pin string) (Stage, error) {
	s.stages++
	if s.stageError != nil {
		return nil, s.stageError
	}
	if s.live[name] {
		return nil, errors.New("another bootwright media add is already acquiring image " + name)
	}
	if s.live == nil {
		s.live = map[string]bool{}
	}
	s.live[name] = true
	if entry, found := s.retained[name]; found {
		if pin != "" && entry.SHA256 == pin {
			return &fakeStage{store: s, name: name, adopted: &entry}, nil
		}
		delete(s.retained, name)
	}
	return &fakeStage{store: s, name: name}, nil
}

// fakeStage refuses to be filled under the root lock, so every test that adds
// an image proves that acquisition runs outside it.
type fakeStage struct {
	store     *fakeStore
	name      string
	data      []byte
	staged    *Staged
	adopted   *managedos.MediaEntry
	verified  bool
	mismatch  bool
	retained  bool
	published bool
	closed    bool
}

func (f *fakeStage) Fill(_ context.Context, payload Payload, limit int64) (Staged, error) {
	if f.store.locked {
		return Staged{}, errors.New("the image was acquired while the root lock was held")
	}
	if f.closed || f.adopted != nil || f.staged != nil {
		return Staged{}, errors.New("media stage is closed, adopted or already filled")
	}
	if f.store.duringFill != nil {
		f.store.duringFill(f.store)
	}
	data, err := io.ReadAll(payload)
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) > limit {
		return Staged{}, errors.New("the image exceeds the media store's size limit")
	}
	f.data = data
	f.store.staged = data
	f.store.writes++
	sum := sha256.Sum256(data)
	f.staged = &Staged{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	return *f.staged, nil
}

func (f *fakeStage) Retained() (managedos.MediaEntry, bool) {
	if f.adopted == nil {
		return managedos.MediaEntry{}, false
	}
	return *f.adopted, true
}

func (f *fakeStage) Verify(context.Context) (Staged, error) {
	if f.store.locked || f.adopted == nil {
		return Staged{}, errors.New("a stage that was not adopted, or under the root lock, was verified")
	}
	if f.store.verifyError != nil {
		return Staged{}, f.store.verifyError
	}
	data := f.store.retainedBytes[f.name]
	sum := sha256.Sum256(data)
	f.staged = &Staged{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	f.verified = true
	f.mismatch = f.staged.Size != f.adopted.Size || f.staged.SHA256 != f.adopted.SHA256
	return *f.staged, nil
}

// Retain keeps a filled stage beside the record that describes it, so the view
// lists it and a stage pinned to its digest adopts it.
func (f *fakeStage) Retain(_ context.Context, record []byte) error {
	if f.staged == nil || f.closed || f.published || f.retained || f.mismatch {
		return errors.New("media stage is not a filled, unpublished stage")
	}
	f.store.kept = append(f.store.kept, record)
	if f.adopted != nil {
		f.retained = true
		return nil
	}
	entry, err := managedos.DecodeStagedMediaRecord(record)
	if err != nil || entry.Name != f.name || entry.Size != f.staged.Size || entry.SHA256 != f.staged.SHA256 {
		return errors.New("media retention does not describe this stage")
	}
	if f.store.retained == nil {
		f.store.retained, f.store.retainedBytes = map[string]managedos.MediaEntry{}, map[string][]byte{}
	}
	f.store.retained[f.name], f.store.retainedBytes[f.name] = entry, f.data
	f.retained = true
	return nil
}

// Close releases the stage's name, and discards an adopted stage whose bytes
// Verify found changed.
func (f *fakeStage) Close() error {
	if !f.closed {
		f.closed = true
		f.store.closed++
		delete(f.store.live, f.name)
		if f.mismatch {
			delete(f.store.retained, f.name)
			delete(f.store.retainedBytes, f.name)
		}
	}
	return nil
}

// Publish installs a filled stage as the image its record describes, over a
// stored image only when replacing, and drops the retained stage it adopted.
func (s *fakeStore) Publish(_ context.Context, name string, stage Stage, record []byte, replace bool) error {
	filled, ok := stage.(*fakeStage)
	if !ok || filled.staged == nil || filled.closed || filled.published || filled.mismatch || filled.name != name || filled.adopted != nil && !filled.verified {
		return errors.New("the publication named no filled stage of this image")
	}
	entry, err := managedos.DecodeMediaRecord(record, name)
	if err != nil || entry.Size != filled.staged.Size || entry.SHA256 != filled.staged.SHA256 {
		return errors.New("media publication does not describe a filled stage of this store")
	}
	if !replace && slices.Contains(s.occupied, name) {
		return errors.New("the media name is already occupied")
	}
	s.published, s.replaced = record, replace
	s.remove(name)
	s.entries, s.occupied = append(s.entries, entry), append(s.occupied, name)
	if s.digests == nil {
		s.digests = map[string]string{}
	}
	s.digests[name] = entry.SHA256
	if filled.adopted != nil {
		delete(s.retained, name)
		delete(s.retainedBytes, name)
	}
	filled.published = true
	delete(s.live, name)
	return nil
}

// Delete removes the image, its record and the stage retained for it.
func (s *fakeStore) Delete(_ context.Context, name string) error {
	s.deleted = name
	s.remove(name)
	delete(s.retained, name)
	delete(s.retainedBytes, name)
	return nil
}

func (s *fakeStore) remove(name string) {
	s.entries = slices.DeleteFunc(s.entries, func(entry managedos.MediaEntry) bool { return entry.Name == name })
	s.occupied = slices.DeleteFunc(s.occupied, func(occupied string) bool { return occupied == name })
	delete(s.digests, name)
}

type fakePayload struct{ *bytes.Reader }

func (fakePayload) Close() error { return nil }

// fakeAcquirer reports origin as each source's origin, or the source itself as
// a URL when origin is empty, and records the source it opened.
type fakeAcquirer struct {
	data   string
	origin string
	err    error
	opens  int
	opened Source
	// readErr fails the payload once its bytes are read, and onOpen runs as
	// the source opens.
	readErr error
	onOpen  func()
}

func (a *fakeAcquirer) Origin(source Source) (string, error) {
	switch {
	case a.origin != "":
		return a.origin, nil
	case source.Path != "":
		return "file://" + source.Path, nil
	}
	return source.URL, nil
}

func (a *fakeAcquirer) Open(_ context.Context, source Source) (Acquisition, error) {
	a.opens++
	a.opened = source
	if a.onOpen != nil {
		a.onOpen()
	}
	if a.err != nil {
		return Acquisition{}, a.err
	}
	if a.readErr != nil {
		return Acquisition{Payload: failingPayload{data: bytes.NewReader([]byte(a.data)), err: a.readErr}}, nil
	}
	return Acquisition{Payload: fakePayload{bytes.NewReader([]byte(a.data))}}, nil
}

// failingPayload serves its bytes, then fails.
type failingPayload struct {
	data *bytes.Reader
	err  error
}

func (p failingPayload) Read(buffer []byte) (int, error) {
	if n, _ := p.data.Read(buffer); n > 0 {
		return n, nil
	}
	return 0, p.err
}

func (failingPayload) Close() error { return nil }

type fakeConfirmer struct {
	calls  int
	action string
	err    error
	// store, when set, is observed at each prompt, and during changes it.
	store  *fakeStore
	locked []bool
	during func(*fakeStore)
	// log, when set, records each prompt in order.
	log *[]string
}

func (c *fakeConfirmer) Confirm(_ context.Context, action, _ string) error {
	c.calls++
	c.action = action
	if c.log != nil {
		*c.log = append(*c.log, "confirm "+action)
	}
	if c.store != nil {
		c.locked = append(c.locked, c.store.locked)
	}
	if c.during != nil {
		c.during(c.store)
	}
	return c.err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC) }

func newService(store *fakeStore, acquirer *fakeAcquirer, confirmer *fakeConfirmer) Service {
	return New(store, acquirer, confirmer, fixedClock{})
}

func digestOf(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func expectMediaFailure(t *testing.T, err error) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" {
		t.Fatalf("unexpected error: %#v", reported)
	}
}

func TestAddPublishesTheAcquiredImageWithItsProvenance(t *testing.T) {
	store := &fakeStore{digests: map[string]string{}}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}
	result, err := newService(store, acquirer, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: digestOf("installer bytes"),
	})
	if err != nil {
		t.Fatalf("add: %#v", diagnostics.Of(err))
	}
	if result.Outcome != "stored" || result.Name != "demo.iso" || result.Size != 15 || result.SHA256 != digestOf("installer bytes") {
		t.Fatalf("result = %+v", result)
	}
	entry, err := managedos.DecodeMediaRecord(store.published, "demo.iso")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source != "file:///images/demo.iso" || entry.Added != "2026-09-15T09:00:00Z" || store.replaced || store.closed != 1 {
		t.Fatalf("published record = %+v replaced=%t closed stages=%d", entry, store.replaced, store.closed)
	}
}

func TestAddRefusesAnImageWhoseBytesDoNotMatchTheExpectedDigest(t *testing.T) {
	store := &fakeStore{}
	service := newService(store, &fakeAcquirer{data: "other bytes"}, nil)
	_, err := service.Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: digestOf("installer bytes"),
	})
	expectMediaFailure(t, err)
	if store.published != nil || store.closed != 1 {
		t.Fatalf("a mismatched image was published or its stage kept: closed %d", store.closed)
	}
}

// Acquisition holds no root lock, so the store may change while an image
// arrives. Publication re-proves admission and refuses a change rather than
// publishing what was never admitted or confirmed.
func TestAddRefusesWhatChangedInTheStoreWhileItAcquired(t *testing.T) {
	for name, test := range map[string]struct {
		occupied []string
		change   func(*fakeStore)
	}{
		"the replaced image was deleted": {[]string{"demo.iso"}, func(s *fakeStore) { s.occupied = nil }},
		"the replaced image was frozen":  {[]string{"demo.iso"}, func(s *fakeStore) { s.reservations = map[string][]string{"demo.iso": {"example"}} }},
		"the name was published":         {nil, func(s *fakeStore) { s.occupied = []string{"demo.iso"} }},
		"the store filled up": {nil, func(s *fakeStore) {
			for index := range managedos.MaxMediaEntries {
				s.occupied = append(s.occupied, string(rune('a'+index%26))+string(rune('a'+index/26))+"-full.iso")
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{occupied: test.occupied, duringFill: test.change}
			acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}
			_, err := newService(store, acquirer, nil).Add(context.Background(), AddMediaRequest{
				Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true,
			})
			expectMediaFailure(t, err)
			if store.writes != 1 || store.published != nil || store.closed != 1 {
				t.Fatalf("writes %d, published %q, closed stages %d", store.writes, store.published, store.closed)
			}
		})
	}
}

func TestAddRefusesBeforeAcquiringAnythingItCannotPublish(t *testing.T) {
	frozen := &fakeStore{occupied: []string{"demo.iso"}, reservations: map[string][]string{"demo.iso": {"example"}}}
	full := &fakeStore{}
	for index := range managedos.MaxMediaEntries {
		full.occupied = append(full.occupied, string(rune('a'+index%26))+"-full.iso")
	}
	for name, test := range map[string]struct {
		store   *fakeStore
		request AddMediaRequest
	}{
		"invalid name":      {&fakeStore{}, AddMediaRequest{Name: "bad name.iso", SourceFile: "/images/demo.iso"}},
		"no source":         {&fakeStore{}, AddMediaRequest{Name: "demo.iso"}},
		"two sources":       {&fakeStore{}, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SourceURL: "https://example.test/demo.iso"}},
		"url without pin":   {&fakeStore{}, AddMediaRequest{Name: "demo.iso", SourceURL: "https://example.test/demo.iso"}},
		"malformed digest":  {&fakeStore{}, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: "abc"}},
		"over-long name":    {&fakeStore{}, AddMediaRequest{Name: strings.Repeat("a", 247) + ".iso", SourceFile: "/images/demo.iso"}},
		"frozen by context": {frozen, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true}},
		"store is full":     {full, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true}},
	} {
		t.Run(name, func(t *testing.T) {
			acquirer := &fakeAcquirer{data: "installer bytes"}
			_, err := newService(test.store, acquirer, nil).Add(context.Background(), test.request)
			expectMediaFailure(t, err)
			if acquirer.opens != 0 || test.store.writes != 0 || test.store.stages != 0 {
				t.Fatalf("refusal acquired %d sources, claimed %d stages and staged %d images", acquirer.opens, test.store.stages, test.store.writes)
			}
		})
	}
}

func TestReplacingAStoredImageConfirmsBeforeItDownloadsAnything(t *testing.T) {
	store := &fakeStore{occupied: []string{"demo.iso"}}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}
	confirmer := &fakeConfirmer{err: errors.New("declined")}
	if _, err := newService(store, acquirer, confirmer).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"}); err == nil {
		t.Fatal("a declined replacement succeeded")
	}
	if confirmer.calls != 1 || confirmer.action != "media replace" || acquirer.opens != 0 {
		t.Fatalf("confirmation = %+v, acquisitions = %d", confirmer, acquirer.opens)
	}
	accepted := &fakeConfirmer{}
	result, err := newService(store, acquirer, accepted).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
	if err != nil || result.Outcome != "replaced" || !store.replaced {
		t.Fatalf("result = %+v (%v)", result, err)
	}
}

// A prompt waits on the operator, so it holds no root lock: every other store
// command on the host would otherwise refuse until the operator answered.
func TestMediaConfirmationsPromptWithNoRootLockHeld(t *testing.T) {
	store := &fakeStore{occupied: []string{"demo.iso"}}
	confirmer := &fakeConfirmer{store: store}
	service := newService(store, &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}, confirmer)
	if _, err := service.Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"}); err != nil {
		t.Fatalf("replacing add: %#v", diagnostics.Of(err))
	}
	if _, err := service.Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"}); err != nil {
		t.Fatalf("delete: %#v", diagnostics.Of(err))
	}
	if len(confirmer.locked) != 2 || confirmer.locked[0] || confirmer.locked[1] {
		t.Fatalf("root lock held at each prompt = %v", confirmer.locked)
	}
}

// What the prompt confirmed is proved again by the exclusive hold that acts on
// it, which refuses, claiming or deleting nothing, when it changed meanwhile.
// A name occupied between the holds of an add that asked nothing is refused
// too, since it would otherwise be replaced unconfirmed.
func TestAnEntryThatChangedWhileItWasConfirmedIsRefused(t *testing.T) {
	published := managedos.MediaEntry{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Source: "file:///old.iso", Added: "2026-09-15T09:00:00Z"}
	republished := published
	republished.SHA256, republished.Added = digestOf("new"), "2026-09-15T10:00:00Z"
	changes := map[string]func(*fakeStore){
		"deleted":          func(s *fakeStore) { s.occupied, s.entries = nil, nil },
		"frozen":           func(s *fakeStore) { s.reservations = map[string][]string{"demo.iso": {"example"}} },
		"digest changed":   func(s *fakeStore) { s.entries = []managedos.MediaEntry{republished} },
		"record withdrawn": func(s *fakeStore) { s.entries = nil },
	}
	for name, change := range changes {
		for _, verb := range []string{"add", "delete"} {
			t.Run(verb+"/"+name, func(t *testing.T) {
				store := &fakeStore{occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published}}
				acquirer := &fakeAcquirer{data: "installer bytes"}
				confirmer := &fakeConfirmer{store: store, during: change}
				service := newService(store, acquirer, confirmer)
				var err error
				if verb == "add" {
					_, err = service.Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
				} else {
					_, err = service.Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
				}
				expectMediaFailure(t, err)
				if confirmer.calls != 1 || store.stages != 0 || store.deleted != "" || acquirer.opens != 0 {
					t.Fatalf("prompts %d, stages %d, deleted %q, acquisitions %d", confirmer.calls, store.stages, store.deleted, acquirer.opens)
				}
			})
		}
	}
	t.Run("delete/re-published", func(t *testing.T) {
		store := &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": published}}
		confirmer := &fakeConfirmer{store: store, during: func(s *fakeStore) {
			s.occupied, s.entries = []string{"demo.iso"}, []managedos.MediaEntry{republished}
		}}
		_, err := newService(store, &fakeAcquirer{}, confirmer).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
		expectMediaFailure(t, err)
		if store.deleted != "" {
			t.Fatalf("a re-published image was deleted unconfirmed")
		}
	})
	t.Run("add/published while admitted", func(t *testing.T) {
		store := &fakeStore{afterRead: func(s *fakeStore) { s.occupied, s.entries = []string{"demo.iso"}, []managedos.MediaEntry{published} }}
		acquirer := &fakeAcquirer{data: "installer bytes"}
		confirmer := &fakeConfirmer{store: store}
		_, err := newService(store, acquirer, confirmer).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
		expectMediaFailure(t, err)
		if confirmer.calls != 0 || store.stages != 0 || acquirer.opens != 0 || !strings.Contains(diagnostics.Of(err)[0].Message, "while this add was being admitted") {
			t.Fatalf("prompts %d, stages %d, acquisitions %d: %#v", confirmer.calls, store.stages, acquirer.opens, diagnostics.Of(err))
		}
	})
}

// A pinned add whose publication meets another command's lock keeps the stage
// it verified, beside the record it would have published; an unpinned add, or
// a publication refused for any other reason, keeps nothing.
func TestAPinnedAddRetainsItsStageOnlyWhenItsPublicationMeetsALock(t *testing.T) {
	for name, test := range map[string]struct {
		pin    string
		busyAt int
		frozen bool
		kept   bool
	}{
		"pinned, lock held":     {digestOf("installer bytes"), 2, false, true},
		"unpinned, lock held":   {"", 2, false, false},
		"pinned, image frozen":  {digestOf("installer bytes"), 0, true, false},
		"pinned, claim refused": {digestOf("installer bytes"), 1, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{busyAt: test.busyAt}
			if test.frozen {
				store.duringFill = func(s *fakeStore) {
					s.occupied = []string{"demo.iso"}
					s.reservations = map[string][]string{"demo.iso": {"example"}}
				}
			}
			_, err := newService(store, &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}, nil).Add(context.Background(), AddMediaRequest{
				Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: test.pin, SkipConfirmation: true,
			})
			if err == nil {
				t.Fatal("an add whose publication was refused succeeded")
			}
			if !test.kept {
				if len(store.kept) != 0 {
					t.Fatalf("retained %q", store.kept)
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(store.kept) != 1 || len(reported) != 1 || reported[0].Code != "lifecycle.lease" || !strings.Contains(reported[0].Remediation, "without acquiring it again") {
				t.Fatalf("retained %d records, refusal %#v", len(store.kept), reported)
			}
			entry, err := managedos.DecodeStagedMediaRecord(store.kept[0])
			if err != nil || entry.Name != "demo.iso" || entry.SHA256 != test.pin || entry.Source != "file:///images/demo.iso" {
				t.Fatalf("retained record = %+v (%v)", entry, err)
			}
		})
	}
}

// An adopted stage is re-read and published with the source it was retained
// with; its source is never opened. One whose bytes changed is refused, and an
// add with another pin discards it and acquires afresh.
func TestAnAdoptedStagePublishesWithoutOpeningItsSource(t *testing.T) {
	retained := managedos.MediaEntry{Name: "demo.iso", Size: 15, SHA256: digestOf("installer bytes"), Source: "https://example.test/demo.iso", Added: "2026-09-14T09:00:00Z"}
	fixture := func(data string) *fakeStore {
		return &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": retained}, retainedBytes: map[string][]byte{"demo.iso": []byte(data)}}
	}
	store := fixture("installer bytes")
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}
	result, err := newService(store, acquirer, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: retained.SHA256,
	})
	if err != nil || result.Outcome != "stored" || acquirer.opens != 0 {
		t.Fatalf("adoption = %+v (%#v), acquisitions %d", result, diagnostics.Of(err), acquirer.opens)
	}
	entry, err := managedos.DecodeMediaRecord(store.published, "demo.iso")
	if err != nil || entry.Source != retained.Source || entry.Added != "2026-09-15T09:00:00Z" || entry.SHA256 != retained.SHA256 {
		t.Fatalf("published record = %+v (%v)", entry, err)
	}
	changed := fixture("INSTALLER BYTES")
	_, err = newService(changed, acquirer, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: retained.SHA256,
	})
	expectMediaFailure(t, err)
	if changed.published != nil || acquirer.opens != 0 {
		t.Fatalf("a retained stage whose bytes changed was published or re-acquired")
	}
	unpinned := fixture("installer bytes")
	if _, err := newService(unpinned, acquirer, nil).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"}); err != nil {
		t.Fatalf("an unpinned add over a retained stage: %#v", diagnostics.Of(err))
	}
	if acquirer.opens != 1 || len(unpinned.retained) != 0 {
		t.Fatalf("an unpinned add adopted a retained stage: acquisitions %d, retained %v", acquirer.opens, unpinned.retained)
	}
}

// The refusal of a pinned add whose publication met another command's lock
// promises only what repeating it does: re-verifying the image it retained
// before publishing it without acquiring it again. A retained stage rewritten
// meanwhile fails that re-verification and is removed, and its refusal says
// so, so the next repetition acquires the image again rather than refusing
// the same way.
func TestARetainedStageRefusalPromisesOnlyWhatItsRepetitionDoes(t *testing.T) {
	request := AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: digestOf("installer bytes"), SkipConfirmation: true}
	store := &fakeStore{busyAt: 2}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}
	_, err := newService(store, acquirer, nil).Add(context.Background(), request)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.lease" ||
		!strings.Contains(reported[0].Remediation, "it re-verifies the image it retained and publishes it without acquiring it again") {
		t.Fatalf("the lock-refused publication reported %#v", reported)
	}
	store.retainedBytes["demo.iso"] = []byte("INSTALLER BYTES")
	_, err = newService(store, acquirer, nil).Add(context.Background(), request)
	reported = diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" ||
		reported[0].Message != "the image retained for demo.iso from file:///images/demo.iso no longer holds the bytes it verified (expected sha256:"+
			digestOf("installer bytes")+", computed sha256:"+digestOf("INSTALLER BYTES")+"), so it was removed" ||
		reported[0].Remediation != "repeat the command to acquire it again" {
		t.Fatalf("the repetition over a rewritten stage reported %#v", reported)
	}
	if _, kept := store.retained["demo.iso"]; kept || store.published != nil || acquirer.opens != 1 {
		t.Fatalf("the rewritten stage was kept, published or acquired again: retained %v, acquisitions %d", store.retained, acquirer.opens)
	}
	if result, err := newService(store, acquirer, nil).Add(context.Background(), request); err != nil || result.Outcome != "stored" || acquirer.opens != 2 {
		t.Fatalf("the next repetition = %+v (%#v), acquisitions %d", result, diagnostics.Of(err), acquirer.opens)
	}
}

func TestListReportsReservationsAndOptionalVerification(t *testing.T) {
	entry := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	other := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{
		entries: []managedos.MediaEntry{entry, other}, reservations: map[string][]string{"demo.iso": {"example"}},
		digests: map[string]string{"demo.iso": entry.SHA256, "alt.iso": digestOf("changed")},
	}
	service := newService(store, &fakeAcquirer{}, nil)
	plain, err := service.List(context.Background(), ListMediaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Media) != 2 || plain.Media[0].Name != "alt.iso" || plain.Media[0].Verified != "" {
		t.Fatalf("plain listing = %+v", plain.Media)
	}
	verified, err := service.List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Media[0].Verified != "mismatch" || verified.Media[1].Verified != "ok" {
		t.Fatalf("verified listing = %+v", verified.Media)
	}
	if verified.Media[0].Frozen || !verified.Media[1].Frozen {
		t.Fatalf("reservations = %+v", verified.Media)
	}
}

func TestDeleteRemovesOnlyAnUnreservedImageTheStoreHolds(t *testing.T) {
	frozen := &fakeStore{occupied: []string{"demo.iso"}, reservations: map[string][]string{"demo.iso": {"example"}}}
	expectMediaFailure(t, mustFail(newService(frozen, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true})))
	if frozen.deleted != "" {
		t.Fatal("a reserved image was deleted")
	}
	absent := &fakeStore{}
	expectMediaFailure(t, mustFail(newService(absent, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true})))
	store := &fakeStore{occupied: []string{"demo.iso"}}
	confirmer := &fakeConfirmer{}
	result, err := newService(store, &fakeAcquirer{}, confirmer).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
	if err != nil || result.Outcome != "deleted" || store.deleted != "demo.iso" || confirmer.action != "media delete" {
		t.Fatalf("result = %+v store = %+v (%v)", result, store, err)
	}
	retained := &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": {Name: "demo.iso"}}}
	result, err = newService(retained, &fakeAcquirer{}, &fakeConfirmer{}).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
	if err != nil || result.Outcome != "deleted" || retained.deleted != "demo.iso" {
		t.Fatalf("deleting a retained stage = %+v (%#v)", result, diagnostics.Of(err))
	}
	overlong := &fakeStore{occupied: []string{strings.Repeat("a", 247) + ".iso"}}
	expectMediaFailure(t, mustFail(newService(overlong, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: strings.Repeat("a", 247) + ".iso", SkipConfirmation: true})))
	if overlong.deleted != "" {
		t.Fatal("an over-long name was deleted")
	}
}

func TestAnUnconfiguredServiceIsUnavailable(t *testing.T) {
	var service Service
	if _, err := service.List(context.Background(), ListMediaRequest{}); err == nil {
		t.Fatal("an unconfigured media service answered")
	}
}

func mustFail(_ *MutationResult, err error) error { return err }

// A record cannot carry an origin over its bound or with a control character,
// so the add refuses before it confirms, claims or acquires anything, names
// the cause and never prints the origin itself.
func refusesTheOriginBeforeAnythingIsClaimed(t *testing.T, origin, cause string) {
	t.Helper()
	for _, request := range []AddMediaRequest{
		{Name: "demo.iso", SourceFile: "/images/demo.iso"},
		{Name: "demo.iso", SourceURL: "https://example.test/demo.iso?X-Amz-Signature=abc", SHA256: digestOf("installer bytes")},
	} {
		store := &fakeStore{occupied: []string{"demo.iso"}}
		acquirer := &fakeAcquirer{data: "installer bytes", origin: origin}
		confirmer := &fakeConfirmer{}
		_, err := newService(store, acquirer, confirmer).Add(context.Background(), request)
		expectMediaFailure(t, err)
		reported := diagnostics.Of(err)[0]
		remedy := "copy the image to a shorter path without control characters and add the copy with --from-file"
		if request.SourceURL != "" {
			remedy = "name a URL whose scheme, host and path fit in 512 bytes, because its query is never recorded"
		}
		if reported.Message != "the origin of image demo.iso "+cause+", so its record cannot carry it" || reported.Remediation != remedy {
			t.Fatalf("the refusal of %+v = %#v", request, reported)
		}
		if store.stages != 0 || acquirer.opens != 0 || confirmer.calls != 0 {
			t.Fatalf("the refused origin claimed %d stages, opened %d sources and prompted %d times", store.stages, acquirer.opens, confirmer.calls)
		}
	}
}

func TestAnOverLongOriginRefusesBeforeAnythingIsClaimed(t *testing.T) {
	origin := "file:///" + strings.Repeat("a", MaxMediaOrigin-len("file:///")+1)
	if len(origin) != 513 {
		t.Fatalf("the origin holds %d bytes, want 513", len(origin))
	}
	refusesTheOriginBeforeAnythingIsClaimed(t, origin, "is longer than 512 bytes")
}

func TestAControlCharacterOriginRefusesBeforeAnythingIsClaimed(t *testing.T) {
	refusesTheOriginBeforeAnythingIsClaimed(t, "file:///images/de\x1b[2Jmo.iso", "carries a control character, leading or trailing white space or invalid UTF-8")
}

// The record carries the origin the acquirer reports, without the query that
// may sign the URL; the download itself still requests it.
func TestTheRecordCarriesTheQueryFreeOrigin(t *testing.T) {
	store := &fakeStore{}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "https://example.test/images/demo.iso"}
	signed := "https://example.test/images/demo.iso?X-Amz-Signature=abc"
	if _, err := newService(store, acquirer, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: signed, SHA256: digestOf("installer bytes"),
	}); err != nil {
		t.Fatalf("add: %#v", diagnostics.Of(err))
	}
	entry, err := managedos.DecodeMediaRecord(store.published, "demo.iso")
	if err != nil || entry.Source != "https://example.test/images/demo.iso" {
		t.Fatalf("published record = %+v (%v)", entry, err)
	}
	if acquirer.opened.URL != signed {
		t.Fatalf("the download requested %q, want the signed URL", acquirer.opened.URL)
	}
}

func TestADigestMismatchNamesBothDigestsAndTheImage(t *testing.T) {
	store := &fakeStore{}
	acquirer := &fakeAcquirer{data: "other bytes", origin: "https://example.test/images/demo.iso"}
	_, err := newService(store, acquirer, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: "https://example.test/images/demo.iso", SHA256: digestOf("installer bytes"),
	})
	expectMediaFailure(t, err)
	reported := diagnostics.Of(err)[0]
	want := "image demo.iso acquired from https://example.test/images/demo.iso does not match its expected digest (expected sha256:" +
		digestOf("installer bytes") + ", computed sha256:" + digestOf("other bytes") + ")"
	if reported.Message != want || !strings.Contains(reported.Remediation, "the publisher's checksum list") || store.published != nil {
		t.Fatalf("the mismatch reported %#v", reported)
	}
}

// Only a completed destroy releases a context's reservation, so every refusal
// of a reserved image names each reserving context and its destroy.
func TestAReservedImageNamesEveryReservingContextAndItsDestroy(t *testing.T) {
	reserved := map[string][]string{"demo.iso": {"lab-a", "lab-b"}}
	refusals := map[string]func() error{
		"delete": func() error {
			store := &fakeStore{occupied: []string{"demo.iso"}, reservations: reserved}
			return mustFail(newService(store, &fakeAcquirer{}, &fakeConfirmer{}).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"}))
		},
		"replacing add": func() error {
			store := &fakeStore{occupied: []string{"demo.iso"}, reservations: reserved}
			return mustFail(newService(store, &fakeAcquirer{data: "installer bytes"}, &fakeConfirmer{}).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"}))
		},
		"reserved while acquiring": func() error {
			store := &fakeStore{occupied: []string{"demo.iso"}, duringFill: func(s *fakeStore) { s.reservations = reserved }}
			return mustFail(newService(store, &fakeAcquirer{data: "installer bytes"}, nil).Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true}))
		},
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			err := refusal()
			expectMediaFailure(t, err)
			reported := diagnostics.Of(err)[0]
			if reported.Message != "image demo.iso is reserved by contexts lab-a, lab-b" ||
				reported.Remediation != "only a completed destroy releases a context's reservation, so repeat this command after "+
					"bootwright destroy --context lab-a and bootwright destroy --context lab-b" {
				t.Fatalf("the refusal = %#v", reported)
			}
		})
	}
	store := &fakeStore{occupied: []string{"demo.iso"}, reservations: map[string][]string{"demo.iso": {"lab-a"}}}
	err := mustFail(newService(store, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true}))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "image demo.iso is reserved by context lab-a" ||
		!strings.HasSuffix(reported[0].Remediation, "after bootwright destroy --context lab-a") {
		t.Fatalf("the refusal of one reserving context = %#v", reported)
	}
}

func TestDeletingAnAbsentImageNamesIt(t *testing.T) {
	err := mustFail(newService(&fakeStore{}, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true}))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the media store holds no image named demo.iso" {
		t.Fatalf("the refusal = %#v", reported)
	}
}

// recordingReporter records every progress event, and observe sees each one as
// it is reported.
type recordingReporter struct {
	events  []ProgressEvent
	observe func(ProgressEvent)
}

func (r *recordingReporter) ReportProgress(_ context.Context, event ProgressEvent) {
	r.events = append(r.events, event)
	if r.observe != nil {
		r.observe(event)
	}
}

// recordingPresenter records every change it shows, in the log it shares with
// the confirmer, and whether the store's root lock was held at the time.
type recordingPresenter struct {
	changes []Change
	locked  []bool
	store   *fakeStore
	log     *[]string
	err     error
}

func (p *recordingPresenter) PresentMediaChange(_ context.Context, change Change) error {
	p.changes = append(p.changes, change)
	if p.store != nil {
		p.locked = append(p.locked, p.store.locked)
	}
	if p.log != nil {
		*p.log = append(*p.log, "present "+change.Action)
	}
	return p.err
}

func expectEvents(t *testing.T, got []ProgressEvent, want ...ProgressEvent) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("progress events\n got %+v\nwant %+v", got, want)
	}
}

func step(name, label, status, detail string, position int) ProgressEvent {
	return ProgressEvent{Step: name, Label: label, Detail: detail, Status: status, Position: position, Total: 2}
}

func check(label, status, detail string, position, total int) ProgressEvent {
	return ProgressEvent{Check: true, Step: "verify", Label: label, Detail: detail, Status: status, Position: position, Total: total}
}

// An add reports its acquisition, opened before the source and naming the
// origin the record carries, never the query, then its publication around the
// hold that publishes.
func TestAddReportsAcquisitionThenPublication(t *testing.T) {
	store := &fakeStore{}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "https://example.test/images/demo.iso"}
	reporter := &recordingReporter{}
	reporter.observe = func(event ProgressEvent) {
		if event.Step == "acquire" && event.Status == "running" && acquirer.opens != 0 {
			t.Errorf("the acquisition was reported after its source opened")
		}
		if event.Step == "publish" && event.Status == "running" && store.published != nil {
			t.Errorf("the publication was reported after it happened")
		}
	}
	_, err := newService(store, acquirer, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: "https://example.test/images/demo.iso?X-Amz-Signature=abc", SHA256: digestOf("installer bytes"),
	})
	if err != nil {
		t.Fatalf("add: %#v", diagnostics.Of(err))
	}
	expectEvents(t, reporter.events,
		step("acquire", "Acquire demo.iso", "running", "https://example.test/images/demo.iso", 1),
		step("acquire", "Acquire demo.iso", "done", "15 bytes", 1),
		step("publish", "Publish demo.iso", "running", "", 2),
		step("publish", "Publish demo.iso", "done", "", 2),
	)
}

// An adopted stage is re-read, not acquired, so its first step says so and
// names the origin it was retained from.
func TestAnAdoptedStageReportsItsVerification(t *testing.T) {
	retained := managedos.MediaEntry{Name: "demo.iso", Size: 15, SHA256: digestOf("installer bytes"), Source: "https://example.test/demo.iso", Added: "2026-09-14T09:00:00Z"}
	store := &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": retained}, retainedBytes: map[string][]byte{"demo.iso": []byte("installer bytes")}}
	reporter := &recordingReporter{}
	if _, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: "https://example.test/demo.iso", SHA256: retained.SHA256,
	}); err != nil {
		t.Fatalf("add: %#v", diagnostics.Of(err))
	}
	expectEvents(t, reporter.events,
		step("verify-retained", "Verify the image retained for demo.iso", "running", "https://example.test/demo.iso", 1),
		step("verify-retained", "Verify the image retained for demo.iso", "done", "15 bytes", 1),
		step("publish", "Publish demo.iso", "running", "", 2),
		step("publish", "Publish demo.iso", "done", "", 2),
	)
	changed := &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": retained}, retainedBytes: map[string][]byte{"demo.iso": []byte("INSTALLER BYTES")}}
	reporter = &recordingReporter{}
	_, err := newService(changed, &fakeAcquirer{}, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: "https://example.test/demo.iso", SHA256: retained.SHA256,
	})
	expectMediaFailure(t, err)
	expectEvents(t, reporter.events,
		step("verify-retained", "Verify the image retained for demo.iso", "running", "https://example.test/demo.iso", 1),
		step("verify-retained", "Verify the image retained for demo.iso", "failed", "", 1),
	)
}

// An acquisition that fails settles its own step failed, and nothing is
// published, so no publication step follows.
func TestAFailedAcquisitionSettlesItsStepFailed(t *testing.T) {
	refused := diagnostics.NewFailureWithRemediation("media.store", "the media endpoint images.example answered 404 Not Found", "", "name an image the endpoint serves")
	for name, acquirer := range map[string]*fakeAcquirer{
		"the source refused to open": {err: refused},
		"the source failed mid-read": {data: "installer", readErr: refused},
		"the digest did not match":   {data: "other bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			acquirer.origin = "https://example.test/images/demo.iso"
			reporter := &recordingReporter{}
			_, err := newService(&fakeStore{}, acquirer, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
				Name: "demo.iso", SourceURL: "https://example.test/images/demo.iso", SHA256: digestOf("installer bytes"),
			})
			if err == nil {
				t.Fatal("a failed acquisition succeeded")
			}
			expectEvents(t, reporter.events,
				step("acquire", "Acquire demo.iso", "running", "https://example.test/images/demo.iso", 1),
				step("acquire", "Acquire demo.iso", "failed", "", 1),
			)
		})
	}
}

// A publication that meets another command's lock, or finds the name it
// admitted published or reserved meanwhile, settles its own step failed rather
// than leaving it running above the refusal, and nothing follows it.
func TestAFailedPublicationSettlesItsStepFailed(t *testing.T) {
	for name, test := range map[string]struct {
		occupied []string
		busyAt   int
		change   func(*fakeStore)
		code     string
	}{
		"another command holds the store": {busyAt: 2, code: "lifecycle.lease"},
		"the name was published meanwhile": {
			change: func(s *fakeStore) { s.occupied = []string{"demo.iso"} }, code: "media.store",
		},
		"the replaced image was reserved meanwhile": {
			occupied: []string{"demo.iso"}, change: func(s *fakeStore) { s.reservations = map[string][]string{"demo.iso": {"lab-rhel"}} }, code: "media.store",
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{occupied: test.occupied, busyAt: test.busyAt, duringFill: test.change}
			reporter := &recordingReporter{}
			_, err := newService(store, &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
				Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: digestOf("installer bytes"), SkipConfirmation: true,
			})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != test.code || store.published != nil {
				t.Fatalf("a refused publication reported %#v, published %q", reported, store.published)
			}
			expectEvents(t, reporter.events,
				step("acquire", "Acquire demo.iso", "running", "file:///images/demo.iso", 1),
				step("acquire", "Acquire demo.iso", "done", "15 bytes", 1),
				step("publish", "Publish demo.iso", "running", "", 2),
				step("publish", "Publish demo.iso", "failed", "", 2),
			)
		})
	}
}

// An adopted stage that cannot be re-read settles its verification failed with
// the store's own cause, and nothing is published.
func TestAnAdoptedStageThatCannotBeReadSettlesItsVerificationFailed(t *testing.T) {
	retained := managedos.MediaEntry{Name: "demo.iso", Size: 15, SHA256: digestOf("installer bytes"), Source: "https://example.test/demo.iso", Added: "2026-09-14T09:00:00Z"}
	unreadable := errors.New("the retained image could not be read in full")
	store := &fakeStore{
		retained: map[string]managedos.MediaEntry{"demo.iso": retained}, retainedBytes: map[string][]byte{"demo.iso": []byte("installer bytes")},
		verifyError: unreadable,
	}
	reporter := &recordingReporter{}
	_, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: "https://example.test/demo.iso", SHA256: retained.SHA256,
	})
	if !errors.Is(err, unreadable) || store.published != nil {
		t.Fatalf("an unreadable adopted stage reported %v, published %q", err, store.published)
	}
	expectEvents(t, reporter.events,
		step("verify-retained", "Verify the image retained for demo.iso", "running", "https://example.test/demo.iso", 1),
		step("verify-retained", "Verify the image retained for demo.iso", "failed", "", 1),
	)
}

// An image the listing cannot read in full settles its check failed with the
// store's cause and is listed failed, rather than refusing the listing or
// reporting a verification it did not make.
func TestAnImageThatCannotBeReadSettlesItsCheckFailed(t *testing.T) {
	entry := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	changed := "its bytes changed while they were read"
	store := &fakeStore{entries: []managedos.MediaEntry{entry}, digestError: imageFailed(changed)}
	reporter := &recordingReporter{}
	listed, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil || len(listed.Media) != 1 {
		t.Fatalf("an unreadable image listed %+v (%v)", listed, err)
	}
	if row := listed.Media[0]; row.Verified != "failed" || row.Failure != changed || row.Computed != "" {
		t.Fatalf("the unreadable image = %+v", row)
	}
	expectEvents(t, reporter.events,
		check("Verify demo.iso", "running", "", 1, 1),
		check("Verify demo.iso", "failed", changed, 1, 1),
	)
}

// Checksums hold every image under the root lock and read each in full only
// after that lock is released, releasing every handle.
func TestListWithChecksumsHashesWithNoRootLockHeld(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	alt := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{entries: []managedos.MediaEntry{demo, alt}, digests: map[string]string{"demo.iso": demo.SHA256, "alt.iso": alt.SHA256}}
	listed, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil || listed.Media[0].Verified != "ok" || listed.Media[1].Verified != "ok" {
		t.Fatalf("listing = %+v (%v)", listed, err)
	}
	if !slices.Equal(store.holdLocked, []bool{true, true}) || !slices.Equal(store.hashLocked, []bool{false, false}) || store.heldClosed != 2 {
		t.Fatalf("holds under lock %v, reads under lock %v, closed %d", store.holdLocked, store.hashLocked, store.heldClosed)
	}
}

// An image the store cannot read is listed failed beside the others, with or
// without checksums; checksums neither hold nor read it but still settle its
// check failed with the cause.
func TestListReportsAFailedImageBesideTheOthers(t *testing.T) {
	good := managedos.MediaEntry{Name: "good.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///good.iso", Added: "2026-09-15T09:00:00Z"}
	malformed := "its record is malformed, not canonical or names another image"
	store := &fakeStore{
		entries: []managedos.MediaEntry{good}, failures: map[string]string{"bad.iso": malformed},
		digests: map[string]string{"good.iso": good.SHA256},
	}
	plain, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{})
	if err != nil || len(plain.Media) != 2 {
		t.Fatalf("plain listing = %+v (%v)", plain, err)
	}
	if bad := plain.Media[0]; bad.Name != "bad.iso" || bad.Verified != "failed" || bad.Failure != malformed || bad.SHA256 != "" {
		t.Fatalf("the failed image = %+v", bad)
	}
	if plain.Media[1].Verified != "" || plain.Media[1].Failure != "" {
		t.Fatalf("the good image = %+v", plain.Media[1])
	}
	reporter := &recordingReporter{}
	verified, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil || len(verified.Media) != 2 || verified.Media[0].Verified != "failed" || verified.Media[1].Verified != "ok" {
		t.Fatalf("verified listing = %+v (%v)", verified, err)
	}
	if len(store.holdLocked) != 1 {
		t.Fatalf("held %d images, want only the good one", len(store.holdLocked))
	}
	expectEvents(t, reporter.events,
		check("Verify bad.iso", "running", "", 1, 2),
		check("Verify bad.iso", "failed", malformed, 1, 2),
		check("Verify good.iso", "running", "", 2, 2),
		check("Verify good.iso", "ok", "matches its record", 2, 2),
	)
}

// A store failure while an image is read refuses the listing, and every
// handle the listing held is still released.
func TestAStoreFailureWhileHashingRefusesTheListing(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	alt := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	broken := errors.New("the media store could not be read")
	store := &fakeStore{entries: []managedos.MediaEntry{demo, alt}, digestError: broken}
	listed, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if !errors.Is(err, broken) || listed != nil {
		t.Fatalf("a store failure listed %+v (%v)", listed, err)
	}
	if len(store.holdLocked) != 2 || store.heldClosed != len(store.holdLocked) {
		t.Fatalf("held %d images, closed %d", len(store.holdLocked), store.heldClosed)
	}
}

// A canceled checksum listing refuses with the cancellation and releases every
// handle it held.
func TestACanceledChecksumListingClosesEveryHeldImage(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	alt := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{entries: []managedos.MediaEntry{demo, alt}, digests: map[string]string{"demo.iso": demo.SHA256, "alt.iso": alt.SHA256}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reporter := &recordingReporter{observe: func(event ProgressEvent) {
		if event.Status == "running" {
			cancel()
		}
	}}
	listed, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(ctx, ListMediaRequest{Checksums: true})
	if !errors.Is(err, context.Canceled) || listed != nil {
		t.Fatalf("a canceled listing listed %+v (%v)", listed, err)
	}
	if len(store.holdLocked) != 2 || store.heldClosed != 2 {
		t.Fatalf("held %d images, closed %d", len(store.holdLocked), store.heldClosed)
	}
}

// A hold the store refuses for the whole store refuses the listing and
// releases what was already held; one that concerns that image alone lists it
// failed.
func TestAHoldFailureClosesWhatWasHeld(t *testing.T) {
	a := managedos.MediaEntry{Name: "a.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///a.iso", Added: "2026-09-15T09:00:00Z"}
	b := managedos.MediaEntry{Name: "b.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///b.iso", Added: "2026-09-15T09:00:00Z"}
	broken := errors.New("the media directory was replaced")
	store := &fakeStore{
		entries: []managedos.MediaEntry{a, b}, holdError: map[string]error{"b.iso": broken},
		digests: map[string]string{"a.iso": a.SHA256, "b.iso": b.SHA256},
	}
	listed, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if !errors.Is(err, broken) || listed != nil || store.heldClosed != 1 || len(store.hashLocked) != 0 {
		t.Fatalf("a refused hold listed %+v (%v), closed %d, read %d", listed, err, store.heldClosed, len(store.hashLocked))
	}
	unopenable := "its file cannot be opened"
	store = &fakeStore{
		entries: []managedos.MediaEntry{a, b}, holdError: map[string]error{"b.iso": imageFailed(unopenable)},
		digests: map[string]string{"a.iso": a.SHA256, "b.iso": b.SHA256},
	}
	listed, err = newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil || listed.Media[0].Verified != "ok" || listed.Media[1].Verified != "failed" || listed.Media[1].Failure != unopenable || store.heldClosed != 1 {
		t.Fatalf("an unopenable image listed %+v (%v), closed %d", listed, err, store.heldClosed)
	}
}

// Reporting stops at cancellation rather than claiming a step whose outcome is
// no longer observable.
func TestNothingIsReportedAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reporter := &recordingReporter{}
	acquirer := &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso", onOpen: cancel}
	_, _ = newService(&fakeStore{}, acquirer, nil).Reporting(reporter, nil).Add(ctx, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true})
	expectEvents(t, reporter.events, step("acquire", "Acquire demo.iso", "running", "file:///images/demo.iso", 1))
	listing, stop := context.WithCancel(context.Background())
	defer stop()
	entry := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{entries: []managedos.MediaEntry{entry}, digests: map[string]string{"demo.iso": entry.SHA256}}
	reporter = &recordingReporter{observe: func(ProgressEvent) { stop() }}
	_, _ = newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(listing, ListMediaRequest{Checksums: true})
	expectEvents(t, reporter.events, check("Verify demo.iso", "running", "", 1, 1))
}

// Each image read in full is one check, in the listing's name order, which
// settles with what its digest proved.
func TestListWithChecksumsReportsOneCheckPerImage(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	alt := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{entries: []managedos.MediaEntry{demo, alt}, digests: map[string]string{"demo.iso": demo.SHA256, "alt.iso": digestOf("changed")}}
	reporter := &recordingReporter{}
	listed, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil {
		t.Fatal(err)
	}
	expectEvents(t, reporter.events,
		check("Verify alt.iso", "running", "", 1, 2),
		check("Verify alt.iso", "failed", "sha256:"+digestOf("changed")+" differs from its record", 1, 2),
		check("Verify demo.iso", "running", "", 2, 2),
		check("Verify demo.iso", "ok", "matches its record", 2, 2),
	)
	if !listed.Checksums || listed.Media[0].Computed != digestOf("changed") || listed.Media[1].Computed != demo.SHA256 {
		t.Fatalf("listing = %+v", listed)
	}
}

// Reading records and file metadata takes no time worth reporting, and neither
// does a deletion.
func TestListWithoutChecksumsReportsNothing(t *testing.T) {
	entry := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{entries: []managedos.MediaEntry{entry}, occupied: []string{"demo.iso"}, digests: map[string]string{"demo.iso": entry.SHA256}}
	reporter := &recordingReporter{}
	service := newService(store, &fakeAcquirer{}, &fakeConfirmer{}).Reporting(reporter, nil)
	listed, err := service.List(context.Background(), ListMediaRequest{})
	if err != nil || listed.Checksums || listed.Media[0].Computed != "" || listed.Media[0].Verified != "" {
		t.Fatalf("listing = %+v (%v)", listed, err)
	}
	if _, err := service.Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"}); err != nil {
		t.Fatalf("delete: %#v", diagnostics.Of(err))
	}
	expectEvents(t, reporter.events)
}

// An image whose size no longer matches its record is listed as a mismatch,
// with or without checksums, and every other image is still listed; with
// checksums it is read in full like any other and its computed digest shown.
func TestListMarksAShortEntryAndKeepsTheRest(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	other := managedos.MediaEntry{Name: "other.iso", Size: 5, SHA256: digestOf("other"), Source: "file:///other.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{
		entries: []managedos.MediaEntry{demo, other}, observed: map[string]int64{"demo.iso": 2},
		digests: map[string]string{"demo.iso": demo.SHA256, "other.iso": other.SHA256},
	}
	plain, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{})
	if err != nil || len(plain.Media) != 2 || plain.Media[0].Verified != "mismatch" || plain.Media[1].Verified != "" || plain.Media[0].Size != 4 {
		t.Fatalf("plain listing = %+v (%v)", plain, err)
	}
	reporter := &recordingReporter{}
	verified, err := newService(store, &fakeAcquirer{}, nil).Reporting(reporter, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil || len(verified.Media) != 2 || verified.Media[0].Verified != "mismatch" || verified.Media[0].Computed != demo.SHA256 ||
		verified.Media[1].Verified != "ok" || verified.Media[1].Computed != other.SHA256 {
		t.Fatalf("verified listing = %+v (%v)", verified, err)
	}
	expectEvents(t, reporter.events,
		check("Verify demo.iso", "running", "", 1, 2),
		check("Verify demo.iso", "failed", "2 bytes differ from its record", 1, 2),
		check("Verify other.iso", "running", "", 2, 2),
		check("Verify other.iso", "ok", "matches its record", 2, 2),
	)
}

// Whether a context reserves an image and whether its bytes verified are two
// facts, and neither hides the other.
func TestListReportsReservingContextsApartFromVerification(t *testing.T) {
	demo := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	other := managedos.MediaEntry{Name: "other.iso", Size: 5, SHA256: digestOf("other"), Source: "file:///other.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{
		entries:      []managedos.MediaEntry{demo, other},
		reservations: map[string][]string{"demo.iso": {"lab-sno", "lab-rhel"}},
		digests:      map[string]string{"demo.iso": demo.SHA256, "other.iso": digestOf("changed")},
	}
	listed, err := newService(store, &fakeAcquirer{}, nil).List(context.Background(), ListMediaRequest{Checksums: true})
	if err != nil {
		t.Fatal(err)
	}
	reserved, unreserved := listed.Media[0], listed.Media[1]
	if !slices.Equal(reserved.ReservedBy, []string{"lab-rhel", "lab-sno"}) || !reserved.Frozen || reserved.Verified != "ok" {
		t.Fatalf("the reserved image = %+v", reserved)
	}
	if unreserved.ReservedBy == nil || len(unreserved.ReservedBy) != 0 || unreserved.Frozen || unreserved.Verified != "mismatch" {
		t.Fatalf("the unreserved image = %+v", unreserved)
	}
}

// A confirmation first shows the stored image it would replace or delete, with
// no root lock held, and only then prompts.
func TestConfirmationsPresentTheStoredEntryFirst(t *testing.T) {
	published := managedos.MediaEntry{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Source: "https://example.test/old.iso", Added: "2026-09-15T09:00:00Z"}
	for name, test := range map[string]struct {
		store  *fakeStore
		delete bool
		want   Change
	}{
		"replace": {
			store: &fakeStore{occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published}},
			want:  Change{Action: ReplaceChange, Name: "demo.iso", Stored: true, Readable: true, Entry: published, NewOrigin: "file:///images/demo.iso"},
		},
		"delete": {
			store: &fakeStore{
				occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published},
				retained: map[string]managedos.MediaEntry{"demo.iso": published},
			},
			delete: true,
			want:   Change{Action: DeleteChange, Name: "demo.iso", Stored: true, Readable: true, Entry: published, Retained: true},
		},
		"delete with an unreadable record": {
			store:  &fakeStore{occupied: []string{"demo.iso"}},
			delete: true,
			want:   Change{Action: DeleteChange, Name: "demo.iso", Stored: true},
		},
		"delete of a retained stage alone": {
			store:  &fakeStore{retained: map[string]managedos.MediaEntry{"demo.iso": published}},
			delete: true,
			want:   Change{Action: DeleteChange, Name: "demo.iso", Retained: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var log []string
			presenter := &recordingPresenter{store: test.store, log: &log}
			confirmer := &fakeConfirmer{log: &log}
			service := newService(test.store, &fakeAcquirer{data: "installer bytes", origin: "file:///images/demo.iso"}, confirmer).Reporting(nil, presenter)
			var err error
			want := []string{"present replace", "confirm media replace"}
			if test.delete {
				_, err = service.Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
				want = []string{"present delete", "confirm media delete"}
			} else {
				_, err = service.Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
			}
			if err != nil {
				t.Fatalf("%s: %#v", name, diagnostics.Of(err))
			}
			if !slices.Equal(log, want) || len(presenter.changes) != 1 || presenter.changes[0] != test.want || slices.Contains(presenter.locked, true) {
				t.Fatalf("log %v, changes %+v, locked %v; want %v and %+v", log, presenter.changes, presenter.locked, want, test.want)
			}
		})
	}
}

// A replacement whose pin adopts the stage a lock-refused add retained
// publishes that stage with the source it was retained from, so its
// confirmation shows that source as the new one, not the source the add names;
// any other pin discards the stage, so it shows the add's own. A retained stage
// gone once the replacement was confirmed refuses, claiming and acquiring
// nothing, since the record would then carry a source it did not show.
func TestAReplacementShowsTheSourceItsRecordWillCarry(t *testing.T) {
	published := managedos.MediaEntry{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Source: "file:///old.iso", Added: "2026-09-15T09:00:00Z"}
	retained := managedos.MediaEntry{Name: "demo.iso", Size: 15, SHA256: digestOf("installer bytes"), Source: "https://mirror-a.example.test/demo.iso", Added: "2026-09-14T09:00:00Z"}
	named := "https://mirror-b.example.test/demo.iso"
	fixture := func() *fakeStore {
		return &fakeStore{
			occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published},
			retained: map[string]managedos.MediaEntry{"demo.iso": retained}, retainedBytes: map[string][]byte{"demo.iso": []byte("installer bytes")},
		}
	}
	for name, test := range map[string]struct {
		pin, data, want string
		opens           int
	}{
		"a pin that adopts the retained stage": {pin: retained.SHA256, want: retained.Source},
		"another pin":                          {pin: digestOf("other bytes"), data: "other bytes", want: named, opens: 1},
	} {
		t.Run(name, func(t *testing.T) {
			store := fixture()
			presenter := &recordingPresenter{}
			acquirer := &fakeAcquirer{data: test.data}
			result, err := newService(store, acquirer, &fakeConfirmer{}).Reporting(nil, presenter).Add(context.Background(), AddMediaRequest{
				Name: "demo.iso", SourceURL: named, SHA256: test.pin,
			})
			if err != nil || result.Outcome != "replaced" || acquirer.opens != test.opens {
				t.Fatalf("the replacement = %+v (%#v), acquisitions %d", result, diagnostics.Of(err), acquirer.opens)
			}
			entry, err := managedos.DecodeMediaRecord(store.published, "demo.iso")
			if err != nil || len(presenter.changes) != 1 || presenter.changes[0].NewOrigin != test.want || entry.Source != test.want {
				t.Fatalf("the confirmation showed %+v, the record carries %+v (%v); want the source %s", presenter.changes, entry, err, test.want)
			}
		})
	}
	store := fixture()
	acquirer := &fakeAcquirer{data: "installer bytes"}
	confirmer := &fakeConfirmer{store: store, during: func(s *fakeStore) { delete(s.retained, "demo.iso") }}
	_, err := newService(store, acquirer, confirmer).Reporting(nil, &recordingPresenter{}).Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceURL: named, SHA256: retained.SHA256,
	})
	expectMediaFailure(t, err)
	if store.stages != 0 || acquirer.opens != 0 || store.published != nil {
		t.Fatalf("a replacement whose retained stage went after its confirmation claimed %d stages, acquired %d times or published", store.stages, acquirer.opens)
	}
}

// A change that could not be shown is not confirmed: the command refuses
// without prompting, claiming, acquiring or deleting anything.
func TestAPresentationFailureRefusesWithoutPrompting(t *testing.T) {
	refused := errors.New("standard output is closed")
	published := managedos.MediaEntry{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Source: "file:///old.iso", Added: "2026-09-15T09:00:00Z"}
	for _, verb := range []string{"add", "delete"} {
		t.Run(verb, func(t *testing.T) {
			store := &fakeStore{occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published}}
			acquirer := &fakeAcquirer{data: "installer bytes"}
			confirmer := &fakeConfirmer{}
			service := newService(store, acquirer, confirmer).Reporting(nil, &recordingPresenter{err: refused})
			var err error
			if verb == "add" {
				_, err = service.Add(context.Background(), AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso"})
			} else {
				_, err = service.Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso"})
			}
			if !errors.Is(err, refused) || confirmer.calls != 0 || store.stages != 0 || acquirer.opens != 0 || store.deleted != "" {
				t.Fatalf("err %v, prompts %d, stages %d, acquisitions %d, deleted %q", err, confirmer.calls, store.stages, acquirer.opens, store.deleted)
			}
		})
	}
}

// A deletion reports the size and digest of the record it removed, whether or
// not it was confirmed, and only the name when no record could be read.
func TestADeletionReportsTheRecordItRemoved(t *testing.T) {
	published := managedos.MediaEntry{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Source: "file:///old.iso", Added: "2026-09-15T09:00:00Z"}
	for _, skip := range []bool{false, true} {
		store := &fakeStore{occupied: []string{"demo.iso"}, entries: []managedos.MediaEntry{published}}
		result, err := newService(store, &fakeAcquirer{}, &fakeConfirmer{}).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: skip})
		if err != nil || *result != (MutationResult{Name: "demo.iso", Size: 3, SHA256: digestOf("old"), Outcome: "deleted"}) {
			t.Fatalf("deletion with --yes %t = %+v (%v)", skip, result, err)
		}
	}
	unreadable := &fakeStore{occupied: []string{"demo.iso"}}
	result, err := newService(unreadable, &fakeAcquirer{}, nil).Delete(context.Background(), DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true})
	if err != nil || *result != (MutationResult{Name: "demo.iso", Outcome: "deleted"}) {
		t.Fatalf("deletion of an unreadable record = %+v (%v)", result, err)
	}
}
