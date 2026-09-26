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
// freeze it. A stage the callback claims is the one thing that outlives it.
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
	entries, err := directoryNames(a.dir, maxMediaDirectory)
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

const (
	// maxStagedMedia bounds the stages that exist at once, live or abandoned:
	// a stage is claimed only after abandoned ones are removed.
	maxStagedMedia = 16
	// maxMediaDirectory bounds the media directory: every image beside its
	// record, and the stages or record temporary files maxStagedMedia allows.
	maxMediaDirectory = 2*managedos.MaxMediaEntries + maxStagedMedia
)

// stagedMediaName is a stage, which an acquisition writes holding no root lock.
func stagedMediaName(name string) bool { return identifier(name, "staging-") }

// pendingMediaName is a record's temporary file, which only a holder of the
// exclusive root lock writes.
func pendingMediaName(name string) bool { return identifier(name, "pending-") }

// mediaStageName names an image's stage by the image, so two stages of one
// name contend for one file.
func mediaStageName(image string) string {
	sum := sha256.Sum256([]byte(image))
	return "staging-" + hex.EncodeToString(sum[:16])
}

// pruneStaging removes what interrupted media mutations left behind. It runs
// under the exclusive root lock, which every record temporary file is written
// under, so any found now is abandoned. A stage is written without that lock,
// so it is abandoned only when no process holds its own lock: its owner holds
// that from claiming the stage until it publishes or removes it, and the
// kernel releases it when the owner dies. Nothing is adopted: only complete
// bytes are published.
func (a *mediaArea) pruneStaging(ctx context.Context) error {
	entries, err := directoryNames(a.dir, maxMediaDirectory)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if !pendingMediaName(entry) && !stagedMediaName(entry) {
			continue
		}
		if err := a.store.checkpoint(ctx, "before-media-staging-prune"); err != nil {
			return err
		}
		if stagedMediaName(entry) {
			held, err := a.stageHeld(entry)
			if err != nil {
				return err
			}
			if held {
				continue
			}
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

// stageHeld reports whether a live acquisition still holds the stage. A stage
// its owner removed after the listing is not held; neither is an entry that is
// not a regular file, which no owner could hold.
func (a *mediaArea) stageHeld(name string) (bool, error) {
	probe, err := openRelative(a.dir, name, pathHandle, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	listed, err := statHandle(probe)
	probe.Close()
	if err != nil || listed.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return false, err
	}
	file, err := openRelative(a.dir, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	opened, err := statHandle(file)
	if err != nil || !sameIdentity(listed, opened) {
		return false, state("media staging changed while it was inspected")
	}
	switch err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return true, nil
	case err != nil:
		return false, state("media staging lock cannot be inspected")
	}
	return false, nil
}

// Stage claims private staging for one image. It lives in the media directory,
// so publishing it is a rename within one filesystem, and it is named by its
// image, so creating it fails while a live stage holds that name. Abandoned
// stages were pruned before this callback, so every stage listed is live.
func (a *mediaArea) Stage(ctx context.Context, name string) (media.Stage, error) {
	if err := a.available(ctx, true); err != nil {
		return nil, err
	}
	if a.dir == nil || !managedos.ValidMediaName(name) {
		return nil, state("media staging requires a valid image name")
	}
	entries, err := directoryNames(a.dir, maxMediaDirectory)
	if err != nil {
		return nil, err
	}
	if len(slices.DeleteFunc(entries, func(entry string) bool { return !stagedMediaName(entry) })) >= maxStagedMedia {
		return nil, mediaFailure("the media store is already acquiring its maximum number of images",
			"retry after another bootwright media add finishes")
	}
	stage, err := a.openStage(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := a.store.checkpoint(ctx, "before-media-staging"); err != nil {
		stage.release()
		return nil, err
	}
	file, err := openRelative(stage.dir, stage.name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if errors.Is(err, syscall.EEXIST) {
		stage.release()
		return nil, mediaFailure("another bootwright media add is already acquiring image "+name,
			"wait for it to finish, then review the store with bootwright media list")
	}
	if err != nil {
		stage.release()
		return nil, safeError(err)
	}
	stage.file = file
	created, err := statHandle(file)
	if err != nil || !privateMediaFile(created, stage.dir) || created.Size != 0 ||
		syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		stage.Close()
		return nil, state("new media staging file is unsafe")
	}
	stage.created = created
	return stage, nil
}

// openStage gives a stage its own handles on the root and media directory, so
// it can verify and remove itself after the transaction's handles close.
func (a *mediaArea) openStage(ctx context.Context, image string) (*mediaStage, error) {
	root, err := a.store.openRoot(ctx, false, nil)
	if err != nil {
		return nil, safeError(err)
	}
	if !sameIdentity(root.identity, a.root.identity) {
		root.file.Close()
		return nil, state("state root was replaced")
	}
	dir, err := openDirectory(root, mediaContainer)
	if err == nil && !sameIdentity(dir.identity, a.dir.identity) {
		dir.file.Close()
		err = state("media directory was replaced")
	}
	if err != nil {
		root.file.Close()
		return nil, safeError(err)
	}
	return &mediaStage{store: a.store, root: root, dir: dir, image: image, name: mediaStageName(image)}, nil
}

// mediaStage is one claimed stage. Its owner holds an exclusive lock on the
// file from the claim until Close, which is how pruneStaging tells it from an
// abandoned one. It belongs to one invocation and is used sequentially.
type mediaStage struct {
	store     *Store
	root      *directory
	dir       *directory
	file      *os.File
	name      string
	image     string
	created   syscall.Stat_t
	filling   bool
	staged    *media.Staged
	published bool
	closed    bool
}

// Fill writes the payload into the stage, reporting the exact bytes it wrote.
// It holds no root lock: only this stage's owner writes the file, and nothing
// is visible under an image name until a transaction publishes it.
func (s *mediaStage) Fill(ctx context.Context, source media.Payload, limit int64) (media.Staged, error) {
	if err := ctx.Err(); err != nil {
		return media.Staged{}, err
	}
	if s.closed || s.filling {
		return media.Staged{}, state("media stage is closed or already filled")
	}
	if source == nil || limit <= 0 || limit > managedos.MaxMediaBytes {
		return media.Staged{}, state("media staging bounds are invalid")
	}
	s.filling = true
	digest, size, err := hashStream(ctx, io.TeeReader(source, s.file), limit)
	if err != nil {
		return media.Staged{}, err
	}
	if err := s.store.checkpoint(ctx, "before-media-staging-sync"); err != nil {
		return media.Staged{}, err
	}
	if err := s.file.Sync(); err != nil {
		return media.Staged{}, state("media staging durability could not be established")
	}
	after, err := statHandle(s.file)
	if err != nil || !sameIdentity(s.created, after) || after.Size != size {
		return media.Staged{}, state("media staging file changed while it was written")
	}
	s.staged = &media.Staged{Size: size, SHA256: digest}
	return *s.staged, nil
}

// Close removes an unpublished stage while its lock is still held, then
// releases it. A stage it cannot prove is its own stays for pruneStaging.
func (s *mediaStage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.file != nil && !s.published {
		var held syscall.Stat_t
		if held, err = statHandle(s.file); err == nil {
			if err = unlinkVerified(s.dir, s.name, held, false); err == nil {
				err = s.dir.file.Sync()
			}
		}
	}
	s.release()
	return err
}

func (s *mediaStage) release() {
	if s.file != nil {
		s.file.Close()
	}
	s.dir.file.Close()
	s.root.file.Close()
}

// at proves that the named entry is exactly this stage's filled file.
func (s *mediaStage) at(dir *directory, name string) error {
	held, err := statHandle(s.file)
	if err != nil {
		return err
	}
	current, err := openRelative(dir, name, pathHandle, 0)
	if err != nil {
		return state("media stage cannot be verified")
	}
	named, err := statHandle(current)
	current.Close()
	if err != nil || !sameFile(held, named) || !privateMediaFile(named, dir) || named.Size != s.staged.Size {
		return state("media stage was substituted before publication")
	}
	return nil
}

// Publish installs a filled stage and its record. A replacement removes the
// entry it supersedes first, so a reader never sees a record whose bytes have
// already changed.
func (a *mediaArea) Publish(ctx context.Context, name string, published media.Stage, record []byte, replace bool) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	stage, ok := published.(*mediaStage)
	if !managedos.ValidMediaName(name) || !ok || stage == nil || len(record) == 0 || len(record) > managedos.MaxMediaRecord {
		return state("media publication values are invalid")
	}
	entry, err := managedos.DecodeMediaRecord(record, name)
	if err != nil {
		return err
	}
	if stage.closed || stage.published || stage.staged == nil || stage.image != name || a.dir == nil ||
		!sameIdentity(stage.dir.identity, a.dir.identity) || entry.Size != stage.staged.Size || entry.SHA256 != stage.staged.SHA256 {
		return state("media publication does not describe a filled stage of this store")
	}
	if err := stage.at(a.dir, stage.name); err != nil {
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
	if err := renameNoReplaceAt(a.dir, stage.name, name); err != nil {
		return safeError(err)
	}
	stage.published = true
	if err := stage.at(a.dir, name); err != nil {
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
