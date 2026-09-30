package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
)

type fakeStore struct {
	entries  []managedos.MediaEntry
	occupied []string
	frozen   []string
	digests  map[string]string
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
	stages     int
	writes     int
	closed     int
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

func (s *fakeStore) Entries(context.Context) ([]managedos.MediaEntry, error) { return s.entries, nil }
func (s *fakeStore) Names(context.Context) ([]string, error)                 { return s.occupied, nil }
func (s *fakeStore) Frozen(context.Context) ([]string, error)                { return s.frozen, nil }

func (s *fakeStore) Digest(_ context.Context, name string) (string, error) {
	return s.digests[name], nil
}

func (s *fakeStore) Entry(_ context.Context, name string) (managedos.MediaEntry, bool, error) {
	for _, entry := range s.entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return managedos.MediaEntry{}, false, nil
}

func (s *fakeStore) Retained(context.Context) ([]string, error) {
	names := []string{}
	for name := range s.retained {
		names = append(names, name)
	}
	return names, nil
}

// Stage adopts an image's retained stage only for its recorded digest, and
// otherwise discards it, as the store's contract requires.
func (s *fakeStore) Stage(_ context.Context, name, pin string) (Stage, error) {
	s.stages++
	if s.stageError != nil {
		return nil, s.stageError
	}
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
	store    *fakeStore
	name     string
	staged   *Staged
	adopted  *managedos.MediaEntry
	verified bool
	closed   bool
}

func (f *fakeStage) Fill(_ context.Context, payload Payload, limit int64) (Staged, error) {
	if f.store.locked {
		return Staged{}, errors.New("the image was acquired while the root lock was held")
	}
	if f.store.duringFill != nil {
		f.store.duringFill(f.store)
	}
	data, err := io.ReadAll(payload)
	if err != nil {
		return Staged{}, err
	}
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
	data := f.store.retainedBytes[f.name]
	sum := sha256.Sum256(data)
	f.staged = &Staged{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	f.verified = true
	return *f.staged, nil
}

func (f *fakeStage) Retain(_ context.Context, record []byte) error {
	if f.staged == nil || f.closed {
		return errors.New("an unfilled or closed stage was retained")
	}
	f.store.kept = append(f.store.kept, record)
	return nil
}

func (f *fakeStage) Close() error {
	if !f.closed {
		f.closed = true
		f.store.closed++
	}
	return nil
}

func (s *fakeStore) Publish(_ context.Context, name string, stage Stage, record []byte, replace bool) error {
	filled, ok := stage.(*fakeStage)
	if !ok || filled.staged == nil || filled.closed || filled.name != name || filled.adopted != nil && !filled.verified {
		return errors.New("the publication named no filled stage of this image")
	}
	s.published, s.replaced = record, replace
	return nil
}

func (s *fakeStore) Delete(_ context.Context, name string) error {
	s.deleted = name
	return nil
}

type fakePayload struct{ *bytes.Reader }

func (fakePayload) Close() error { return nil }

type fakeAcquirer struct {
	data   string
	origin string
	err    error
	opens  int
}

func (a *fakeAcquirer) Open(context.Context, Source) (Acquisition, error) {
	a.opens++
	if a.err != nil {
		return Acquisition{}, a.err
	}
	return Acquisition{Payload: fakePayload{bytes.NewReader([]byte(a.data))}, Origin: a.origin}, nil
}

type fakeConfirmer struct {
	calls  int
	action string
	err    error
	// store, when set, is observed at each prompt, and during changes it.
	store  *fakeStore
	locked []bool
	during func(*fakeStore)
}

func (c *fakeConfirmer) Confirm(_ context.Context, action, _ string) error {
	c.calls++
	c.action = action
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
		"the replaced image was frozen":  {[]string{"demo.iso"}, func(s *fakeStore) { s.frozen = []string{"demo.iso"} }},
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
	frozen := &fakeStore{occupied: []string{"demo.iso"}, frozen: []string{"demo.iso"}}
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
		"frozen":           func(s *fakeStore) { s.frozen = []string{"demo.iso"} },
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
				store.duringFill = func(s *fakeStore) { s.occupied = []string{"demo.iso"}; s.frozen = []string{"demo.iso"} }
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

func TestListReportsReservationsAndOptionalVerification(t *testing.T) {
	entry := managedos.MediaEntry{Name: "demo.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///demo.iso", Added: "2026-09-15T09:00:00Z"}
	other := managedos.MediaEntry{Name: "alt.iso", Size: 4, SHA256: digestOf("data"), Source: "file:///alt.iso", Added: "2026-09-15T09:00:00Z"}
	store := &fakeStore{
		entries: []managedos.MediaEntry{entry, other}, frozen: []string{"demo.iso"},
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
	frozen := &fakeStore{occupied: []string{"demo.iso"}, frozen: []string{"demo.iso"}}
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
