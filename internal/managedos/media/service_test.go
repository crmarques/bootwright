package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	staged   []byte
	// published records the exact publication the service asked for.
	published  []byte
	replaced   bool
	deleted    string
	stageError error
	writes     int
}

func (s *fakeStore) ReadMedia(ctx context.Context, callback func(View) error) error {
	return callback(s)
}

func (s *fakeStore) MutateMedia(ctx context.Context, callback func(Transaction) error) error {
	return callback(s)
}

func (s *fakeStore) Entries(context.Context) ([]managedos.MediaEntry, error) { return s.entries, nil }
func (s *fakeStore) Names(context.Context) ([]string, error)                 { return s.occupied, nil }
func (s *fakeStore) Frozen(context.Context) ([]string, error)                { return s.frozen, nil }

func (s *fakeStore) Digest(_ context.Context, name string) (string, error) {
	return s.digests[name], nil
}

func (s *fakeStore) Stage(_ context.Context, payload Payload, limit int64) (Staged, error) {
	if s.stageError != nil {
		return Staged{}, s.stageError
	}
	data, err := io.ReadAll(payload)
	if err != nil {
		return Staged{}, err
	}
	s.staged = data
	s.writes++
	sum := sha256.Sum256(data)
	return Staged{ID: "pending-" + strings.Repeat("0", 32), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func (s *fakeStore) Publish(_ context.Context, name string, staged Staged, record []byte, replace bool) error {
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
}

func (c *fakeConfirmer) Confirm(_ context.Context, action, _ string) error {
	c.calls++
	c.action = action
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
	if entry.Source != "file:///images/demo.iso" || entry.Added != "2026-09-15T09:00:00Z" || store.replaced {
		t.Fatalf("published record = %+v replaced=%t", entry, store.replaced)
	}
}

func TestAddRefusesAnImageWhoseBytesDoNotMatchTheExpectedDigest(t *testing.T) {
	store := &fakeStore{}
	service := newService(store, &fakeAcquirer{data: "other bytes"}, nil)
	_, err := service.Add(context.Background(), AddMediaRequest{
		Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: digestOf("installer bytes"),
	})
	expectMediaFailure(t, err)
	if store.published != nil {
		t.Fatal("a mismatched image was published")
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
		"frozen by context": {frozen, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true}},
		"store is full":     {full, AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SkipConfirmation: true}},
	} {
		t.Run(name, func(t *testing.T) {
			acquirer := &fakeAcquirer{data: "installer bytes"}
			_, err := newService(test.store, acquirer, nil).Add(context.Background(), test.request)
			expectMediaFailure(t, err)
			if acquirer.opens != 0 || test.store.writes != 0 {
				t.Fatalf("refusal acquired %d sources and staged %d images", acquirer.opens, test.store.writes)
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
}

func TestAnUnconfiguredServiceIsUnavailable(t *testing.T) {
	var service Service
	if _, err := service.List(context.Background(), ListMediaRequest{}); err == nil {
		t.Fatal("an unconfigured media service answered")
	}
}

func mustFail(_ *MutationResult, err error) error { return err }
