package media

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
)

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
// replacement never downloads anything. Acquisition holds no root lock: an
// image may take hours to arrive, and every other store command on the host
// would refuse while it did. Admission is therefore proved twice, once to claim
// a stage and again, against the store as it now stands, to publish it.
func (s Service) Add(ctx context.Context, request AddMediaRequest) (*MutationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if !managedos.ValidMediaName(request.Name) {
		return nil, failure("the media name is not a portable ISO basename",
			"name the image with ASCII letters, digits, dot, underscore or dash and a lowercase .iso suffix")
	}
	source, err := selectSource(request)
	if err != nil {
		return nil, err
	}
	expected, ok := managedos.NormalizeMediaDigest(request.SHA256)
	if !ok {
		return nil, failure("the supplied digest is not a SHA-256 content pin", "supply 64 hexadecimal digits, optionally prefixed by sha256:")
	}
	if source.URL != "" && expected == "" {
		return nil, failure("a downloaded image requires its expected digest", "repeat the command with --sha256 <digest>")
	}
	var stage Stage
	replacing := false
	err = s.store.MutateMedia(ctx, func(tx Transaction) error {
		var err error
		if replacing, err = s.admit(ctx, tx, request); err != nil {
			return err
		}
		stage, err = tx.Stage(ctx, request.Name)
		return err
	})
	if stage != nil {
		// Close's own failure is ignored: the outcome to report is the
		// publication or the refusal that preceded it, and a stage Close could
		// not remove is stale, so the next media mutation removes it.
		defer stage.Close()
	}
	if err != nil {
		return nil, err
	}
	entry, err := s.acquire(ctx, stage, request.Name, source, expected)
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
		return nil, err
	}
	outcome := "stored"
	if replacing {
		outcome = "replaced"
	}
	return &MutationResult{Name: entry.Name, Size: entry.Size, SHA256: entry.SHA256, Outcome: outcome}, nil
}

// admit proves the store can take the image and confirms a replacement. It
// reports whether the name is occupied, which is what was confirmed.
func (s Service) admit(ctx context.Context, tx Transaction, request AddMediaRequest) (bool, error) {
	replacing, err := admissible(ctx, tx, request.Name)
	if err != nil || !replacing || request.SkipConfirmation {
		return replacing, err
	}
	if s.confirmer == nil {
		return false, failure("replacing a stored image requires confirmation", "review the image and repeat with --yes")
	}
	return true, s.confirmer.Confirm(ctx, "media replace", request.Name)
}

// revalidate re-proves admission under the lock that publishes. Only this
// invocation's stage can publish the name, so the name's occupancy changes only
// when another command deletes it, and a lifecycle operation may have frozen it
// meanwhile; any change refuses rather than publishing what was not confirmed.
func revalidate(ctx context.Context, tx Transaction, name string, replacing bool) error {
	occupied, err := admissible(ctx, tx, name)
	if err != nil {
		return err
	}
	if occupied != replacing {
		return failure("image "+name+" changed in the media store while it was being acquired",
			"review the store with bootwright media list, then repeat the command")
	}
	return nil
}

func admissible(ctx context.Context, tx Transaction, name string) (bool, error) {
	occupied, err := tx.Names(ctx)
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
	frozen, err := tx.Frozen(ctx)
	if err != nil {
		return false, err
	}
	if slices.Contains(frozen, name) {
		return false, frozenFailure(name)
	}
	return true, nil
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
		Added:  s.clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00"),
	}, nil
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

// Delete removes one image and its record. An image any context reserves is
// refused, because a frozen operation still needs exactly those bytes.
func (s Service) Delete(ctx context.Context, request DeleteMediaRequest) (*MutationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if !managedos.ValidMediaName(request.Name) {
		return nil, failure("the media name is not a portable ISO basename", "name an image this store holds")
	}
	var result *MutationResult
	err := s.store.MutateMedia(ctx, func(tx Transaction) error {
		occupied, err := tx.Names(ctx)
		if err != nil {
			return err
		}
		if !slices.Contains(occupied, request.Name) {
			return failure("the media store holds no image with that name", "list the store with bootwright media list")
		}
		frozen, err := tx.Frozen(ctx)
		if err != nil {
			return err
		}
		if slices.Contains(frozen, request.Name) {
			return frozenFailure(request.Name)
		}
		if !request.SkipConfirmation {
			if s.confirmer == nil {
				return failure("deleting a stored image requires confirmation", "review the image and repeat with --yes")
			}
			if err := s.confirmer.Confirm(ctx, "media delete", request.Name); err != nil {
				return err
			}
		}
		if err := tx.Delete(ctx, request.Name); err != nil {
			return err
		}
		result = &MutationResult{Name: request.Name, Outcome: "deleted"}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func selectSource(request AddMediaRequest) (Source, error) {
	file, url := strings.TrimSpace(request.SourceFile), strings.TrimSpace(request.SourceURL)
	if (file == "") == (url == "") {
		return Source{}, failure("exactly one media source is required", "supply --from-file or --from-url")
	}
	return Source{Path: file, URL: url}, nil
}

func frozenFailure(name string) error {
	return failure("image "+name+" is reserved by a context operation",
		"destroy or complete the operation that froze it before changing this image")
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, "", remediation)
}
