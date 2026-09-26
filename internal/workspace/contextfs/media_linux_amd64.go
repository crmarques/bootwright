//go:build linux && amd64

package contextfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

const mediaContainer = "media"

// mediaArea is the host-wide installer media directory. Media is shared by
// every context, so it is coordinated by the root lock alone and never by a
// context lease.
type mediaArea struct {
	store  *Store
	root   *directory
	dir    *directory
	stored controllerStored
	active func() bool
	write  bool
}

func (a *mediaArea) available(ctx context.Context, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.active == nil || !a.active() || write && !a.write {
		return state("media capability is closed or read-only")
	}
	if a.dir == nil {
		return nil
	}
	return a.dir.verify()
}

// ReadMedia takes a coherent read of the store under the shared root lock. It
// creates, repairs and publishes nothing.
func (s *Store) ReadMedia(ctx context.Context, callback func(media.View) error) error {
	return s.withMedia(ctx, false, func(area *mediaArea) error { return callback(area) })
}

// MutateMedia holds the exclusive root lock for the whole callback, so an image
// cannot be published while another invocation reads the reservations that
// freeze it.
func (s *Store) MutateMedia(ctx context.Context, callback func(media.Transaction) error) error {
	return s.withMedia(ctx, true, func(area *mediaArea) error { return callback(area) })
}

func (s *Store) withMedia(ctx context.Context, write bool, callback func(*mediaArea) error) error {
	if callback == nil {
		return state("media callback is missing")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return mediaUnprepared()
	}
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	mode := lockShared
	if write {
		mode = lock
	}
	if err := mode(root); err != nil {
		return err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, exists, err := readRegistry(ctx, root)
	if err != nil {
		return safeError(err)
	}
	if !exists {
		return mediaUnprepared()
	}
	stored, err := readControllerStored(ctx, root, registry)
	if err != nil {
		return safeError(err)
	}
	active := true
	defer func() { active = false }()
	area := &mediaArea{store: s, root: root, stored: stored, active: func() bool { return active }, write: write}
	dir, err := openDirectory(root, mediaContainer)
	switch {
	case err == nil:
		defer dir.file.Close()
		area.dir = dir
	case !errors.Is(err, syscall.ENOENT):
		return safeError(err)
	case write:
		created, err := s.newDirectory(ctx, root, mediaContainer)
		if err != nil {
			return safeError(err)
		}
		defer created.file.Close()
		area.dir = created
	}
	if area.dir != nil && write {
		if err := area.pruneStaging(ctx); err != nil {
			return safeError(err)
		}
	}
	return safeError(callback(area))
}

func mediaUnprepared() error {
	return mediaFailure("this host has no Bootwright store to hold installer media", "run bootwright setup first")
}

// Names lists every occupied media name, so a publication never collides with
// bytes this store still holds, including an interrupted one.
func (a *mediaArea) Names(ctx context.Context) ([]string, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	if a.dir == nil {
		return []string{}, nil
	}
	entries, err := directoryNames(a.dir, 2*managedos.MaxMediaEntries+1)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry, ".json")
		if !managedos.ValidMediaName(name) || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// Entries lists every image whose record and bytes are both present. An
// interrupted publication is occupied but not complete, so it is not listed.
func (a *mediaArea) Entries(ctx context.Context) ([]managedos.MediaEntry, error) {
	names, err := a.Names(ctx)
	if err != nil {
		return nil, err
	}
	entries := []managedos.MediaEntry{}
	for _, name := range names {
		data, err := readBounded(ctx, a.dir, name+".json", managedos.MaxMediaRecord, true)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			return nil, safeError(err)
		}
		entry, err := managedos.DecodeMediaRecord(data, name)
		if err != nil {
			return nil, err
		}
		size, err := a.size(name)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			return nil, safeError(err)
		}
		if size != entry.Size {
			return nil, mediaFailure("image "+name+" no longer holds the number of bytes its record published",
				"replace or delete the image with bootwright media add or bootwright media delete")
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (a *mediaArea) size(name string) (int64, error) {
	file, err := openRelative(a.dir, name, pathHandle, 0)
	if err != nil {
		return 0, err
	}
	stat, err := statHandle(file)
	file.Close()
	if err != nil || !privateMediaFile(stat, a.dir) {
		return 0, state("media file type, owner, permissions or links is unsafe")
	}
	return stat.Size, nil
}

func privateMediaFile(stat syscall.Stat_t, parent *directory) bool {
	return stat.Dev == parent.identity.Dev && private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid)
}

// Digest reads one image in full. Media is far larger than any record, so the
// bytes are hashed as they stream and never held in memory.
func (a *mediaArea) Digest(ctx context.Context, name string) (string, error) {
	if err := a.available(ctx, false); err != nil {
		return "", err
	}
	if a.dir == nil || !managedos.ValidMediaName(name) {
		return "", mediaFailure("the media store holds no image with that name", "list the store with bootwright media list")
	}
	file, err := openRelative(a.dir, name, syscall.O_RDONLY, 0)
	if err != nil {
		return "", safeError(err)
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !privateMediaFile(before, a.dir) {
		return "", state("media file type, owner, permissions or links is unsafe")
	}
	digest, size, err := hashStream(ctx, file, managedos.MaxMediaBytes)
	if err != nil {
		return "", err
	}
	after, err := statHandle(file)
	if err != nil || !sameFile(before, after) || size != after.Size {
		return "", state("media file changed while it was being read")
	}
	return digest, nil
}

func hashStream(ctx context.Context, source io.Reader, limit int64) (string, int64, error) {
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	total := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		n, err := source.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return "", 0, mediaFailure("the image exceeds the media store's size limit", "install from smaller media")
			}
			hash.Write(buffer[:n])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, mediaFailure("the image could not be read in full", "verify the source and repeat the command")
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), total, nil
}

// Frozen names every image a context reserves. A media claim is shared, so any
// number of contexts may hold one and it blocks only deletion and replacement.
func (a *mediaArea) Frozen(ctx context.Context) ([]string, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	names := []string{}
	for _, reservation := range a.stored.value.Reservations {
		for _, key := range reservation.Keys {
			name, found := strings.CutPrefix(key, "media:")
			if !found || slices.Contains(names, name) {
				continue
			}
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// pruneStaging removes the private staging files an interrupted publication
// left behind. They are never adopted: only complete bytes are published.
func (a *mediaArea) pruneStaging(ctx context.Context) error {
	entries, err := directoryNames(a.dir, 2*managedos.MaxMediaEntries+maxStagedMedia)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if !stagedMediaName(entry) {
			continue
		}
		if err := a.store.checkpoint(ctx, "before-media-staging-prune"); err != nil {
			return err
		}
		if err := syscall.Unlinkat(int(a.dir.file.Fd()), entry); err != nil && !errors.Is(err, syscall.ENOENT) {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return a.store.syncDirectory(ctx, a.dir)
}

const maxStagedMedia = 16

func stagedMediaName(name string) bool { return identifier(name, "pending-") }

// Stage copies the source into a private staging file, reporting the exact
// bytes it wrote. Nothing is published until the caller has proved them.
func (a *mediaArea) Stage(ctx context.Context, source media.Payload, limit int64) (media.Staged, error) {
	if err := a.available(ctx, true); err != nil {
		return media.Staged{}, err
	}
	if source == nil || limit <= 0 || limit > managedos.MaxMediaBytes {
		return media.Staged{}, state("media staging bounds are invalid")
	}
	name, err := a.store.candidate("pending-")
	if err != nil {
		return media.Staged{}, err
	}
	if err := a.store.checkpoint(ctx, "before-media-staging"); err != nil {
		return media.Staged{}, err
	}
	file, err := openRelative(a.dir, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return media.Staged{}, safeError(err)
	}
	staged, err := a.stage(ctx, file, name, source, limit)
	file.Close()
	if err != nil {
		_ = syscall.Unlinkat(int(a.dir.file.Fd()), name)
		return media.Staged{}, err
	}
	return staged, nil
}

func (a *mediaArea) stage(ctx context.Context, file *os.File, name string, source media.Payload, limit int64) (media.Staged, error) {
	before, err := statHandle(file)
	if err != nil || !privateMediaFile(before, a.dir) || before.Size != 0 {
		return media.Staged{}, state("new media staging file is unsafe")
	}
	digest, size, err := hashStream(ctx, io.TeeReader(source, file), limit)
	if err != nil {
		return media.Staged{}, err
	}
	if err := a.store.checkpoint(ctx, "before-media-staging-sync"); err != nil {
		return media.Staged{}, err
	}
	if err := file.Sync(); err != nil {
		return media.Staged{}, state("media staging durability could not be established")
	}
	after, err := statHandle(file)
	if err != nil || !sameIdentity(before, after) || after.Size != size {
		return media.Staged{}, state("media staging file changed while it was written")
	}
	return media.Staged{ID: name, Size: size, SHA256: digest}, nil
}

// Publish installs a staged image and its record. A replacement removes the
// entry it supersedes first, so a reader never sees a record whose bytes have
// already changed.
func (a *mediaArea) Publish(ctx context.Context, name string, staged media.Staged, record []byte, replace bool) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if !managedos.ValidMediaName(name) || !stagedMediaName(staged.ID) || len(record) == 0 || len(record) > managedos.MaxMediaRecord {
		return state("media publication values are invalid")
	}
	if _, err := managedos.DecodeMediaRecord(record, name); err != nil {
		return err
	}
	if replace {
		if err := a.remove(ctx, name); err != nil {
			return err
		}
	}
	if err := a.store.checkpoint(ctx, "before-media-rename"); err != nil {
		return err
	}
	if err := renameNoReplaceAt(a.dir, staged.ID, name); err != nil {
		return safeError(err)
	}
	size, err := a.size(name)
	if err != nil || size != staged.Size {
		return state("published media bytes are not the staged image")
	}
	if err := a.store.syncDirectory(ctx, a.dir); err != nil {
		return err
	}
	if err := a.store.checkpoint(ctx, "before-media-record"); err != nil {
		return err
	}
	if err := a.store.writeExclusiveAtomic(ctx, a.dir, name+".json", record, false); err != nil {
		return safeError(err)
	}
	return a.available(ctx, true)
}

func (a *mediaArea) Delete(ctx context.Context, name string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if !managedos.ValidMediaName(name) {
		return state("media deletion requires a valid image name")
	}
	if err := a.remove(ctx, name); err != nil {
		return err
	}
	return a.available(ctx, true)
}

// remove drops the record before the bytes, so an interruption leaves an
// occupied name with no record rather than a record describing absent bytes.
func (a *mediaArea) remove(ctx context.Context, name string) error {
	if err := a.store.checkpoint(ctx, "before-media-record-removal"); err != nil {
		return err
	}
	if err := a.dir.verify(); err != nil {
		return err
	}
	if err := syscall.Unlinkat(int(a.dir.file.Fd()), name+".json"); err != nil && !errors.Is(err, syscall.ENOENT) {
		return safeError(err)
	}
	if err := a.store.syncDirectory(ctx, a.dir); err != nil {
		return err
	}
	if err := a.store.checkpoint(ctx, "before-media-image-removal"); err != nil {
		return err
	}
	if err := syscall.Unlinkat(int(a.dir.file.Fd()), name); err != nil && !errors.Is(err, syscall.ENOENT) {
		return safeError(err)
	}
	return a.store.syncDirectory(ctx, a.dir)
}

func mediaFailure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, "", remediation)
}
