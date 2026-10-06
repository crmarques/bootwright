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

// Service manages the host-wide installer media store. It selects no context:
// one store serves every context on the host, and a context-scoped artifact
// server is what publishes derived content to a consumer.
type Service struct {
	store     Store
	acquirer  Acquirer
	confirmer Confirmer
	clock     Clock
}

func New(store Store, acquirer Acquirer, confirmer Confirmer, clock Clock) Service {
	return Service{store: store, acquirer: acquirer, confirmer: confirmer, clock: clock}
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
// replacement never downloads anything, and the prompt holds no root lock.
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
	stage, replacing, err := s.claim(ctx, request, expected)
	if stage != nil {
		// Close's own failure is ignored: the outcome to report is the
		// publication or the refusal that preceded it, and a stage Close could
		// not remove is stale, so the next media mutation removes it.
		defer stage.Close()
	}
	if err != nil {
		return nil, err
	}
	entry, err := s.obtain(ctx, stage, request.Name, source, expected)
	if err != nil {
		return nil, err
	}
	record, err := managedos.EncodeMediaRecord(entry)
	if err != nil {
		return nil, err
	}
	err = s.store.MutateMedia(ctx, func(tx Transaction) error {
		if err := revalidate(ctx, tx, request.Name, replacing); err != nil {
			return err
		}
		return tx.Publish(ctx, entry.Name, stage, record, replacing)
	})
	if err != nil {
		return nil, unpublished(ctx, err, stage, record, request.Name, expected)
	}
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

// observed is what a media confirmation confirms about one name: whether it is
// occupied, the record published for it and, for a deletion, whether a stage is
// retained for it.
type observed struct {
	occupied bool
	listed   bool
	retained bool
	entry    managedos.MediaEntry
}

func observe(ctx context.Context, view View, name string, occupied bool) (observed, error) {
	entry, listed, err := view.Entry(ctx, name)
	return observed{occupied: occupied, listed: listed, entry: entry}, err
}

// claim admits the add and claims its stage. Without --yes a shared hold
// admits it first and a replacement is confirmed with no root lock held; the
// exclusive hold that claims the stage then refuses, claiming nothing, when
// what that hold observed changed meanwhile, whether or not a prompt ran, since
// a name occupied since would otherwise be replaced unconfirmed.
func (s Service) claim(ctx context.Context, request AddMediaRequest, expected string) (Stage, bool, error) {
	var confirmed *observed
	if !request.SkipConfirmation {
		seen, err := s.confirmReplacement(ctx, request.Name)
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
			current, err := observe(ctx, tx, request.Name, occupied)
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
// replace an image, confirms that with no root lock held.
func (s Service) confirmReplacement(ctx context.Context, name string) (observed, error) {
	var seen observed
	err := s.store.ReadMedia(ctx, func(view View) error {
		occupied, err := admissible(ctx, view, name)
		if err != nil {
			return err
		}
		if occupied && s.confirmer == nil {
			return failure("replacing a stored image requires confirmation", "review the image and repeat with --yes")
		}
		seen, err = observe(ctx, view, name, occupied)
		return err
	})
	if err != nil || !seen.occupied {
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
	frozen, err := view.Frozen(ctx)
	if err != nil {
		return false, err
	}
	if slices.Contains(frozen, name) {
		return false, frozenFailure(name)
	}
	return true, nil
}

// obtain fills a claimed stage from its source, or proves that an adopted stage
// still holds the bytes it was retained with. Either runs outside every store
// transaction, so it holds no root lock.
func (s Service) obtain(ctx context.Context, stage Stage, name string, source Source, expected string) (managedos.MediaEntry, error) {
	retained, adopted := stage.Retained()
	if !adopted {
		return s.acquire(ctx, stage, name, source, expected)
	}
	staged, err := stage.Verify(ctx)
	if err != nil {
		return managedos.MediaEntry{}, err
	}
	if staged.Size != retained.Size || staged.SHA256 != retained.SHA256 || staged.SHA256 != expected {
		return managedos.MediaEntry{}, failure("the image retained for "+name+" no longer holds the bytes it verified, so it was removed",
			"repeat the command to acquire it again")
	}
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
func (s Service) acquire(ctx context.Context, stage Stage, name string, source Source, expected string) (managedos.MediaEntry, error) {
	acquisition, err := s.acquirer.Open(ctx, source)
	if err != nil {
		return managedos.MediaEntry{}, err
	}
	defer acquisition.Payload.Close()
	staged, err := stage.Fill(ctx, acquisition.Payload, managedos.MaxMediaBytes)
	if err != nil {
		return managedos.MediaEntry{}, err
	}
	if expected != "" && staged.SHA256 != expected {
		return managedos.MediaEntry{}, failure("the acquired image does not match its expected digest",
			"verify the source and its published digest, then repeat the command")
	}
	return managedos.MediaEntry{
		Name:   name,
		Size:   staged.Size,
		SHA256: staged.SHA256,
		Source: acquisition.Origin,
		Added:  s.now(),
	}, nil
}

func (s Service) now() string {
	return s.clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00")
}

// List reports the store's inventory. Without checksums it reads records and
// file metadata alone; with them it reads every image in full and marks an
// entry whose bytes no longer match its record.
func (s Service) List(ctx context.Context, request ListMediaRequest) (*ListResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	result := &ListResult{Media: []MediaRow{}}
	err := s.store.ReadMedia(ctx, func(view View) error {
		entries, err := view.Entries(ctx)
		if err != nil {
			return err
		}
		frozen, err := view.Frozen(ctx)
		if err != nil {
			return err
		}
		rows := make([]MediaRow, 0, len(entries))
		for _, entry := range entries {
			row := MediaRow{
				Name: entry.Name, Size: entry.Size, SHA256: entry.SHA256,
				Source: entry.Source, Added: entry.Added, Frozen: slices.Contains(frozen, entry.Name),
			}
			if request.Checksums {
				digest, err := view.Digest(ctx, entry.Name)
				if err != nil {
					return err
				}
				row.Verified = "mismatch"
				if digest == entry.SHA256 {
					row.Verified = "ok"
				}
			}
			rows = append(rows, row)
		}
		slices.SortFunc(rows, func(x, y MediaRow) int { return strings.Compare(x.Name, y.Name) })
		result.Media = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Delete removes one image and its record, and a stage retained for it. An
// image any context reserves is refused, because a frozen operation still needs
// exactly those bytes. Without --yes a shared hold admits the deletion, the
// prompt holds no root lock, and the exclusive hold that deletes refuses when
// what was confirmed changed meanwhile.
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
	err := s.store.MutateMedia(ctx, func(tx Transaction) error {
		current, err := deletable(ctx, tx, request.Name)
		if err != nil {
			return err
		}
		if confirmed != nil && current != *confirmed {
			return changedFailure(request.Name, "while its deletion was being confirmed")
		}
		return tx.Delete(ctx, request.Name)
	})
	if err != nil {
		return nil, err
	}
	return &MutationResult{Name: request.Name, Outcome: "deleted"}, nil
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
	return seen, s.confirmer.Confirm(ctx, "media delete", name)
}

// deletable proves the store holds an image or a retained stage by that name
// that no context reserves, and reports what a confirmation confirms.
func deletable(ctx context.Context, view View, name string) (observed, error) {
	occupied, err := view.Names(ctx)
	if err != nil {
		return observed{}, err
	}
	retained, err := view.Retained(ctx)
	if err != nil {
		return observed{}, err
	}
	if !slices.Contains(occupied, name) && !slices.Contains(retained, name) {
		return observed{}, failure("the media store holds no image with that name", "list the store with bootwright media list")
	}
	frozen, err := view.Frozen(ctx)
	if err != nil {
		return observed{}, err
	}
	if slices.Contains(frozen, name) {
		return observed{}, frozenFailure(name)
	}
	seen, err := observe(ctx, view, name, slices.Contains(occupied, name))
	seen.retained = slices.Contains(retained, name)
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

func frozenFailure(name string) error {
	return failure("image "+name+" is reserved by a context operation",
		"destroy or complete the operation that froze it before changing this image")
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, "", remediation)
}
