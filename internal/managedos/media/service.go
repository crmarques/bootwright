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
// replacement never downloads anything.
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
	var result *MutationResult
	err = s.store.MutateMedia(ctx, func(tx Transaction) error {
		published, err := s.publish(ctx, tx, request, source, expected)
		result = published
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) publish(ctx context.Context, tx Transaction, request AddMediaRequest, source Source, expected string) (*MutationResult, error) {
	occupied, err := tx.Names(ctx)
	if err != nil {
		return nil, err
	}
	replacing := slices.Contains(occupied, request.Name)
	if !replacing && len(occupied) >= managedos.MaxMediaEntries {
		return nil, failure("the media store already holds its maximum number of images",
			"delete an image this host no longer installs from")
	}
	if replacing {
		frozen, err := tx.Frozen(ctx)
		if err != nil {
			return nil, err
		}
		if slices.Contains(frozen, request.Name) {
			return nil, frozenFailure(request.Name)
		}
		if !request.SkipConfirmation {
			if s.confirmer == nil {
				return nil, failure("replacing a stored image requires confirmation", "review the image and repeat with --yes")
			}
			if err := s.confirmer.Confirm(ctx, "media replace", request.Name); err != nil {
				return nil, err
			}
		}
	}
	acquisition, err := s.acquirer.Open(ctx, source)
	if err != nil {
		return nil, err
	}
	defer acquisition.Payload.Close()
	staged, err := tx.Stage(ctx, acquisition.Payload, managedos.MaxMediaBytes)
	if err != nil {
		return nil, err
	}
	if expected != "" && staged.SHA256 != expected {
		return nil, failure("the acquired image does not match its expected digest",
			"verify the source and its published digest, then repeat the command")
	}
	entry := managedos.MediaEntry{
		Name:   request.Name,
		Size:   staged.Size,
		SHA256: staged.SHA256,
		Source: acquisition.Origin,
		Added:  s.clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00"),
	}
	record, err := managedos.EncodeMediaRecord(entry)
	if err != nil {
		return nil, err
	}
	if err := tx.Publish(ctx, entry.Name, staged, record, replacing); err != nil {
		return nil, err
	}
	outcome := "stored"
	if replacing {
		outcome = "replaced"
	}
	return &MutationResult{Name: entry.Name, Size: entry.Size, SHA256: entry.SHA256, Outcome: outcome}, nil
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
