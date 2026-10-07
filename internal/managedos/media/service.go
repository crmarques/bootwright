package media

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
)

// MaxMediaName is the longest media name a request may carry, published with
// the service so a driving adapter checks it without depending on the domain
// rule it comes from.
const MaxMediaName = managedos.MaxMediaName

// MaxMediaOrigin is the longest credential-free origin a record carries,
// published with the service for the same reason.
const MaxMediaOrigin = managedos.MaxMediaOrigin

// Service manages the host-wide installer media store. It selects no context:
// one store serves every context on the host, and a context-scoped artifact
// server is what publishes derived content to a consumer.
type Service struct {
	store     Store
	acquirer  Acquirer
	confirmer Confirmer
	clock     Clock
	progress  Reporter
	presenter Presenter
}

func New(store Store, acquirer Acquirer, confirmer Confirmer, clock Clock) Service {
	return Service{store: store, acquirer: acquirer, confirmer: confirmer, clock: clock}
}

// Reporting returns the service reporting its progress through progress and
// showing each change it confirms through presenter. A nil one reports or
// shows nothing.
func (s Service) Reporting(progress Reporter, presenter Presenter) Service {
	s.progress, s.presenter = progress, presenter
	return s
}

// report stops at cancellation rather than claiming a step whose outcome is no
// longer observable.
func (s Service) report(ctx context.Context, event ProgressEvent, status, detail string) {
	if s.progress == nil || ctx.Err() != nil {
		return
	}
	event.Status, event.Detail = status, detail
	s.progress.ReportProgress(ctx, event)
}

func (s Service) present(ctx context.Context, change Change) error {
	if s.presenter == nil {
		return nil
	}
	return s.presenter.PresentMediaChange(ctx, change)
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.store == nil || s.acquirer == nil || s.clock == nil {
		return availability.ErrNotImplemented
	}
	return nil
}

// Add acquires one image, proves its bytes and publishes it with its record.
// Confirmation and every refusal precede acquisition, so a declined
// replacement never downloads anything, and the prompt holds no root lock. The
// origin the record carries is decided first, so a source the record cannot
// carry is refused before anything is confirmed, claimed or acquired.
// Acquisition holds none either: an image may take hours to arrive, and every
// other store command on the host would refuse while it did. Admission is
// therefore proved again, against the store as it then stands, to claim a stage
// and to publish it.
func (s Service) Add(ctx context.Context, request AddMediaRequest) (*MutationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	source, expected, err := addition(request)
	if err != nil {
		return nil, err
	}
	origin, err := s.origin(request.Name, source)
	if err != nil {
		return nil, err
	}
	stage, replacing, err := s.claim(ctx, request, expected, origin)
	if stage != nil {
		// Close's own failure is ignored: the outcome to report is the
		// publication or the refusal that preceded it, and a stage Close could
		// not remove is stale, so the next media mutation removes it.
		defer stage.Close()
	}
	if err != nil {
		return nil, err
	}
	entry, err := s.obtain(ctx, stage, request.Name, source, origin, expected)
	if err != nil {
		return nil, err
	}
	record, err := managedos.EncodeMediaRecord(entry)
	if err != nil {
		return nil, err
	}
	publication := ProgressEvent{Step: "publish", Label: "Publish " + request.Name, Position: 2, Total: 2}
	s.report(ctx, publication, "running", "")
	err = s.store.MutateMedia(ctx, func(tx Transaction) error {
		if err := revalidate(ctx, tx, request.Name, replacing); err != nil {
			return err
		}
		return tx.Publish(ctx, entry.Name, stage, record, replacing)
	})
	if err != nil {
		s.report(ctx, publication, "failed", "")
		return nil, unpublished(ctx, err, stage, record, request.Name, expected)
	}
	s.report(ctx, publication, "done", "")
	outcome := "stored"
	if replacing {
		outcome = "replaced"
	}
	return &MutationResult{Name: entry.Name, Size: entry.Size, SHA256: entry.SHA256, Outcome: outcome}, nil
}

// addition refuses what an add names before any store is read, and returns its
// one source and its normalized digest pin.
func addition(request AddMediaRequest) (Source, string, error) {
	if !managedos.ValidMediaName(request.Name) {
		return Source{}, "", failure("the media name is not a portable ISO basename",
			"name the image with ASCII letters, digits, dot, underscore or dash and a lowercase .iso suffix, in at most "+
				strconv.Itoa(MaxMediaName)+" bytes")
	}
	source, err := selectSource(request)
	if err != nil {
		return Source{}, "", err
	}
	expected, ok := managedos.NormalizeMediaDigest(request.SHA256)
	if !ok {
		return Source{}, "", failure("the supplied digest is not a SHA-256 content pin", "supply 64 hexadecimal digits, optionally prefixed by sha256:")
	}
	if source.URL != "" && expected == "" {
		return Source{}, "", failure("a downloaded image requires its expected digest", "repeat the command with --sha256 <digest>")
	}
	return source, expected, nil
}

// origin is the credential-free origin the acquirer will record for the source,
// refused when a record cannot carry it. The cause is named, never the origin
// itself, which may be over-long or carry a control character.
func (s Service) origin(name string, source Source) (string, error) {
	origin, err := s.acquirer.Origin(source)
	if err != nil {
		return "", err
	}
	if managedos.ValidMediaOrigin(origin) {
		return origin, nil
	}
	cause := "carries a control character, leading or trailing white space or invalid UTF-8"
	switch {
	case origin == "":
		cause = "is empty"
	case len(origin) > MaxMediaOrigin:
		cause = "is longer than " + strconv.Itoa(MaxMediaOrigin) + " bytes"
	}
	remedy := "copy the image to a shorter path without control characters and add the copy with --from-file"
	if source.URL != "" {
		remedy = "name a URL whose scheme, host and path fit in " + strconv.Itoa(MaxMediaOrigin) + " bytes, because its query is never recorded"
	}
	return "", failure("the origin of image "+name+" "+cause+", so its record cannot carry it", remedy)
}

// observed is what a media confirmation confirms about one name: whether it is
// occupied, the record published for it and, for a deletion, whether a stage is
// retained for it, or, for a replacement, the origin its record would carry.
type observed struct {
	occupied bool
	listed   bool
	retained bool
	entry    managedos.MediaEntry
	origin   string
}

func observe(ctx context.Context, view View, name string, occupied bool) (observed, error) {
	entry, listed, err := view.Entry(ctx, name)
	return observed{occupied: occupied, listed: listed, entry: entry}, err
}

func (o observed) change(action, name string) Change {
	return Change{Action: action, Name: name, Stored: o.occupied, Readable: o.listed, Entry: o.entry, Retained: o.retained, NewOrigin: o.origin}
}

// replacement observes what an add over a name confirms: the stored image and,
// when the name is occupied, the origin the replacing record would carry.
func replacement(ctx context.Context, view View, name string, occupied bool, expected, origin string) (observed, error) {
	seen, err := observe(ctx, view, name, occupied)
	if err != nil || !occupied {
		return seen, err
	}
	seen.origin, err = recorded(ctx, view, name, expected, origin)
	return seen, err
}

// recorded is the origin an add's record would carry: the source of the
// name's retained stage when the pin adopts that stage, and otherwise the
// origin of the add's own source.
func recorded(ctx context.Context, view View, name, expected, origin string) (string, error) {
	retained, err := view.Retained(ctx)
	if err != nil {
		return "", err
	}
	for _, entry := range retained {
		if entry.Name == name && expected != "" && entry.SHA256 == expected {
			return entry.Source, nil
		}
	}
	return origin, nil
}

// claim admits the add and claims its stage. Without --yes a shared hold
// admits it first and a replacement is confirmed with no root lock held; the
// exclusive hold that claims the stage then refuses, claiming nothing, when
// what that hold observed changed meanwhile, whether or not a prompt ran, since
// a name occupied since would otherwise be replaced unconfirmed.
func (s Service) claim(ctx context.Context, request AddMediaRequest, expected, origin string) (Stage, bool, error) {
	var confirmed *observed
	if !request.SkipConfirmation {
		seen, err := s.confirmReplacement(ctx, request.Name, expected, origin)
		if err != nil {
			return nil, false, err
		}
		confirmed = &seen
	}
	var stage Stage
	replacing := false
	err := s.store.MutateMedia(ctx, func(tx Transaction) error {
		occupied, err := admissible(ctx, tx, request.Name)
		if err != nil {
			return err
		}
		if confirmed != nil {
			current, err := replacement(ctx, tx, request.Name, occupied, expected, origin)
			if err != nil {
				return err
			}
			if current != *confirmed {
				while := "while this add was being admitted"
				if confirmed.occupied {
					while = "while its replacement was being confirmed"
				}
				return changedFailure(request.Name, while)
			}
		}
		replacing = occupied
		stage, err = tx.Stage(ctx, request.Name, expected)
		return err
	})
	return stage, replacing, err
}

// confirmReplacement admits an add under a shared hold and, when it would
// replace an image, shows that image and the origin the replacing record would
// carry, and confirms its replacement with no root lock held.
func (s Service) confirmReplacement(ctx context.Context, name, expected, origin string) (observed, error) {
	var seen observed
	err := s.store.ReadMedia(ctx, func(view View) error {
		occupied, err := admissible(ctx, view, name)
		if err != nil {
			return err
		}
		if occupied && s.confirmer == nil {
			return failure("replacing a stored image requires confirmation", "review the image and repeat with --yes")
		}
		seen, err = replacement(ctx, view, name, occupied, expected, origin)
		return err
	})
	if err != nil || !seen.occupied {
		return seen, err
	}
	if err := s.present(ctx, seen.change(ReplaceChange, name)); err != nil {
		return seen, err
	}
	return seen, s.confirmer.Confirm(ctx, "media replace", name)
}

// revalidate re-proves admission under the lock that publishes. Only this
// invocation's stage can publish the name, so the name's occupancy changes only
// when another command deletes it, and a lifecycle operation may have frozen it
// meanwhile; any change refuses rather than publishing what was not confirmed.
func revalidate(ctx context.Context, view View, name string, replacing bool) error {
	occupied, err := admissible(ctx, view, name)
	if err != nil {
		return err
	}
	if occupied != replacing {
		return failure("image "+name+" changed in the media store while it was being acquired",
			"review the store with bootwright media list, then repeat the command")
	}
	return nil
}

func admissible(ctx context.Context, view View, name string) (bool, error) {
	occupied, err := view.Names(ctx)
	if err != nil {
		return false, err
	}
	replacing := slices.Contains(occupied, name)
	if !replacing && len(occupied) >= managedos.MaxMediaEntries {
		return false, failure("the media store already holds its maximum number of images",
			"delete an image this host no longer installs from")
	}
	if !replacing {
		return false, nil
	}
	reservations, err := view.Reservations(ctx)
	if err != nil {
		return false, err
	}
	if holders := reservations[name]; len(holders) != 0 {
		return false, frozenFailure(name, holders)
	}
	return true, nil
}

// obtain fills a claimed stage from its source, or proves that an adopted stage
// still holds the bytes it was retained with. Either runs outside every store
// transaction, so it holds no root lock.
func (s Service) obtain(ctx context.Context, stage Stage, name string, source Source, origin, expected string) (managedos.MediaEntry, error) {
	retained, adopted := stage.Retained()
	if !adopted {
		return s.acquire(ctx, stage, name, source, origin, expected)
	}
	verification := ProgressEvent{Step: "verify-retained", Label: "Verify the image retained for " + name, Position: 1, Total: 2}
	s.report(ctx, verification, "running", retained.Source)
	staged, err := stage.Verify(ctx)
	if err != nil {
		s.report(ctx, verification, "failed", "")
		return managedos.MediaEntry{}, err
	}
	if staged.Size != retained.Size || staged.SHA256 != retained.SHA256 || staged.SHA256 != expected {
		s.report(ctx, verification, "failed", "")
		return managedos.MediaEntry{}, failure("the image retained for "+name+" from "+retained.Source+" no longer holds the bytes it verified "+
			digests(retained.SHA256, staged.SHA256)+", so it was removed",
			"repeat the command to acquire it again")
	}
	s.report(ctx, verification, "done", bytesSummary(staged.Size))
	return managedos.MediaEntry{Name: name, Size: staged.Size, SHA256: staged.SHA256, Source: retained.Source, Added: s.now()}, nil
}

// unpublished reports a publication that did not happen. A pinned add whose
// publication met another command's lock first retains its verified stage, so
// repeating the command re-verifies it and publishes it without acquiring it
// again; a stage it cannot retain is removed like any other, and the lock's
// refusal stands. The repetition promises no publication outright, because a
// retained stage rewritten meanwhile fails that re-verification and is removed.
func unpublished(ctx context.Context, err error, stage Stage, record []byte, name, expected string) error {
	if !errors.Is(err, ErrBusy) || expected == "" || stage.Retain(ctx, record) != nil {
		return err
	}
	return diagnostics.NewFailureWithRemediation("lifecycle.lease",
		"another Bootwright command holds the media store, so image "+name+" was verified but not published", "",
		"repeat this command once that command finishes: it re-verifies the image it retained and publishes it without acquiring it again; "+
			"or discard it with bootwright media delete --name "+name)
}

// acquire fills the stage from the source and proves the bytes against the
// expected digest. It runs outside every store transaction, so it holds no
// root lock.
func (s Service) acquire(ctx context.Context, stage Stage, name string, source Source, origin, expected string) (managedos.MediaEntry, error) {
	acquisition := ProgressEvent{Step: "acquire", Label: "Acquire " + name, Position: 1, Total: 2}
	s.report(ctx, acquisition, "running", origin)
	opened, err := s.acquirer.Open(ctx, source)
	if err != nil {
		s.report(ctx, acquisition, "failed", "")
		return managedos.MediaEntry{}, err
	}
	defer opened.Payload.Close()
	staged, err := stage.Fill(ctx, opened.Payload, managedos.MaxMediaBytes)
	if err != nil {
		s.report(ctx, acquisition, "failed", "")
		return managedos.MediaEntry{}, err
	}
	if expected != "" && staged.SHA256 != expected {
		s.report(ctx, acquisition, "failed", "")
		return managedos.MediaEntry{}, failure("image "+name+" acquired from "+origin+" does not match its expected digest "+digests(expected, staged.SHA256),
			"compare both with the publisher's checksum list, then repeat the command with the digest it gives for that image")
	}
	s.report(ctx, acquisition, "done", bytesSummary(staged.Size))
	return managedos.MediaEntry{
		Name:   name,
		Size:   staged.Size,
		SHA256: staged.SHA256,
		Source: origin,
		Added:  s.now(),
	}, nil
}

func bytesSummary(size int64) string {
	return strconv.FormatInt(size, 10) + " bytes"
}

// digests names the digest a check expected and the one it computed.
func digests(expected, computed string) string {
	return "(expected sha256:" + expected + ", computed sha256:" + computed + ")"
}

func (s Service) now() string {
	return s.clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00")
}

// List reports the store's inventory. Without checksums it reads records and
// file metadata alone under the shared lock, and marks an image whose size no
// longer matches its record. With them it opens every listed image under that
// lock, releases it, and then reads each image in full through the handle it
// opened as one check each, so no other command meets a held lock while it
// reads; it reports the digest it computed and marks an image whose bytes no
// longer match its record. An image the store cannot read is listed as failed
// with its cause. Neither a mismatched nor a failed image hides the rest of
// the store, and whether a context reserves an image is reported apart from
// whether it verified. Every handle is released however the listing ends.
func (s Service) List(ctx context.Context, request ListMediaRequest) (*ListResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	result := &ListResult{Media: []MediaRow{}, Checksums: request.Checksums}
	var images []Image
	var held []Held
	defer func() {
		for _, handle := range held {
			if handle != nil {
				handle.Close()
			}
		}
	}()
	err := s.store.ReadMedia(ctx, func(view View) error {
		listed, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		reservations, err := view.Reservations(ctx)
		if err != nil {
			return err
		}
		images = slices.SortedFunc(slices.Values(listed), func(x, y Image) int { return strings.Compare(x.Name, y.Name) })
		rows := make([]MediaRow, 0, len(images))
		for _, image := range images {
			reserving := slices.Sorted(slices.Values(reservations[image.Name]))
			if reserving == nil {
				reserving = []string{}
			}
			row := MediaRow{
				Name: image.Name, Size: image.Size, SHA256: image.SHA256, Source: image.Source, Added: image.Added,
				Frozen: len(reserving) != 0, ReservedBy: reserving,
			}
			if image.Failure != "" {
				row.Verified, row.Failure = "failed", image.Failure
			} else if image.Observed != image.Size {
				row.Verified = "mismatch"
			}
			rows = append(rows, row)
		}
		result.Media = rows
		if !request.Checksums {
			return nil
		}
		held = make([]Held, len(rows))
		for index := range rows {
			if rows[index].Verified == "failed" {
				continue
			}
			handle, err := view.Hold(ctx, rows[index].Name)
			reason, unreadable := imageFailureReason(err)
			switch {
			case unreadable:
				rows[index].Verified, rows[index].Failure = "failed", reason
			case err != nil:
				return err
			default:
				held[index] = handle
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !request.Checksums {
		return result, nil
	}
	for index := range result.Media {
		err := s.check(ctx, held[index], images[index], index+1, len(images), &result.Media[index])
		if held[index] != nil {
			held[index].Close()
			held[index] = nil
		}
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// check reads one held image in full, holding no store lock, and settles
// whether its bytes still match its record, reporting the digest it computed.
// An image the store could not read settles failed, naming why, and fails no
// other check.
func (s Service) check(ctx context.Context, handle Held, image Image, position, total int, row *MediaRow) error {
	verification := ProgressEvent{Check: true, Step: "verify", Label: "Verify " + image.Name, Position: position, Total: total}
	s.report(ctx, verification, "running", "")
	if row.Verified == "failed" {
		s.report(ctx, verification, "failed", row.Failure)
		return nil
	}
	digest, err := handle.Digest(ctx)
	if reason, unreadable := imageFailureReason(err); unreadable {
		row.Verified, row.Failure = "failed", reason
		s.report(ctx, verification, "failed", reason)
		return nil
	}
	if err != nil {
		s.report(ctx, verification, "failed", "")
		return err
	}
	row.Computed = digest
	switch {
	case digest != image.SHA256:
		row.Verified = "mismatch"
		s.report(ctx, verification, "failed", "sha256:"+digest+" differs from its record")
	case image.Observed != image.Size:
		row.Verified = "mismatch"
		s.report(ctx, verification, "failed", bytesSummary(image.Observed)+" differ from its record")
	default:
		row.Verified = "ok"
		s.report(ctx, verification, "ok", "matches its record")
	}
	return nil
}

// Delete removes one image and its record, and a stage retained for it, and
// reports the record it removed. An image any context reserves is refused,
// because a frozen operation still needs exactly those bytes. Without --yes a
// shared hold admits the deletion, the image is shown and the prompt holds no
// root lock, and the exclusive hold that deletes refuses when what was
// confirmed changed meanwhile.
func (s Service) Delete(ctx context.Context, request DeleteMediaRequest) (*MutationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if !managedos.ValidMediaName(request.Name) {
		return nil, failure("the media name is not a portable ISO basename",
			"name an image this store holds, in at most "+strconv.Itoa(MaxMediaName)+" bytes")
	}
	var confirmed *observed
	if !request.SkipConfirmation {
		seen, err := s.confirmDeletion(ctx, request.Name)
		if err != nil {
			return nil, err
		}
		confirmed = &seen
	}
	var deleted observed
	err := s.store.MutateMedia(ctx, func(tx Transaction) error {
		current, err := deletable(ctx, tx, request.Name)
		if err != nil {
			return err
		}
		if confirmed != nil && current != *confirmed {
			return changedFailure(request.Name, "while its deletion was being confirmed")
		}
		deleted = current
		return tx.Delete(ctx, request.Name)
	})
	if err != nil {
		return nil, err
	}
	result := &MutationResult{Name: request.Name, Outcome: "deleted"}
	if deleted.listed {
		result.Size, result.SHA256 = deleted.entry.Size, deleted.entry.SHA256
	}
	return result, nil
}

func (s Service) confirmDeletion(ctx context.Context, name string) (observed, error) {
	var seen observed
	err := s.store.ReadMedia(ctx, func(view View) error {
		var err error
		if seen, err = deletable(ctx, view, name); err != nil {
			return err
		}
		if s.confirmer == nil {
			return failure("deleting a stored image requires confirmation", "review the image and repeat with --yes")
		}
		return nil
	})
	if err != nil {
		return seen, err
	}
	if err := s.present(ctx, seen.change(DeleteChange, name)); err != nil {
		return seen, err
	}
	return seen, s.confirmer.Confirm(ctx, "media delete", name)
}

// deletable proves the store holds an image or a retained stage by that name
// that no context reserves, and reports what a confirmation confirms.
func deletable(ctx context.Context, view View, name string) (observed, error) {
	occupied, err := view.Names(ctx)
	if err != nil {
		return observed{}, err
	}
	listed, err := view.Retained(ctx)
	if err != nil {
		return observed{}, err
	}
	retained := slices.ContainsFunc(listed, func(entry managedos.MediaEntry) bool { return entry.Name == name })
	if !slices.Contains(occupied, name) && !retained {
		return observed{}, failure("the media store holds no image named "+name, "list the store with bootwright media list")
	}
	reservations, err := view.Reservations(ctx)
	if err != nil {
		return observed{}, err
	}
	if holders := reservations[name]; len(holders) != 0 {
		return observed{}, frozenFailure(name, holders)
	}
	seen, err := observe(ctx, view, name, slices.Contains(occupied, name))
	seen.retained = retained
	return seen, err
}

func selectSource(request AddMediaRequest) (Source, error) {
	file, url := strings.TrimSpace(request.SourceFile), strings.TrimSpace(request.SourceURL)
	if (file == "") == (url == "") {
		return Source{}, failure("exactly one media source is required", "supply --from-file or --from-url")
	}
	return Source{Path: file, URL: url}, nil
}

func changedFailure(name, while string) error {
	return failure("image "+name+" changed in the media store "+while,
		"review the store with bootwright media list, then repeat the command")
}

// frozenFailure names each context reserving the image. Only a completed
// destroy releases a context's reservation; a completed apply keeps it.
func frozenFailure(name string, contexts []string) error {
	holders := "context " + contexts[0]
	destroys := "bootwright destroy --context " + contexts[0]
	if len(contexts) > 1 {
		holders = "contexts " + strings.Join(contexts, ", ")
		destroys = "bootwright destroy --context " + strings.Join(contexts, " and bootwright destroy --context ")
	}
	return failure("image "+name+" is reserved by "+holders,
		"only a completed destroy releases a context's reservation, so repeat this command after "+destroys)
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, "", remediation)
}
