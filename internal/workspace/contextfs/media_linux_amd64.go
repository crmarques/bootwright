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
	if callback == nil {
		return state("media read callback is missing")
	}
	return s.withMedia(ctx, false, func(area *mediaArea) error { return callback(area) })
}

// MutateMedia holds the exclusive root lock for the whole callback, so an image
// cannot be published while another invocation reads the reservations that
// freeze it. A stage the callback claims is the one thing that outlives it.
func (s *Store) MutateMedia(ctx context.Context, callback func(media.Transaction) error) error {
	if callback == nil {
		return state("media mutation callback is missing")
	}
	return s.withMedia(ctx, true, func(area *mediaArea) error { return callback(area) })
}

func (s *Store) withMedia(ctx context.Context, write bool, callback func(*mediaArea) error) error {
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
		var held *busyError
		if errors.As(err, &held) {
			return &mediaBusy{held}
		}
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

// mediaBusy is a root lock another command holds. The media service tells it
// from every other refusal, so a pinned add keeps the stage it verified; it
// still reports the lock's lifecycle.lease diagnostic.
type mediaBusy struct{ held *busyError }

func (e *mediaBusy) Error() string { return e.held.Error() }

func (e *mediaBusy) Unwrap() error { return e.held }

func (e *mediaBusy) Is(target error) bool { return target == media.ErrBusy }

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

// The causes a listing gives for one image it cannot read. Each fails that
// image alone.
const (
	recordUnreadable  = "its record cannot be read safely"
	recordUndecodable = "its record is malformed, not canonical or names another image"
	imageUnsafe       = "its file type, owner, permissions or links are unsafe"
	imageUnopenable   = "its file cannot be opened"
	imageUnreadable   = "its bytes could not be read in full or exceed the media store's size limit"
	imageChanged      = "its bytes changed while they were read"
	imageReplaced     = "it was deleted or replaced while it was read"
)

// Entries lists every image whose record and bytes are both present, each with
// the size its bytes have now. An interrupted publication is occupied but not
// complete, so it is not listed; an image whose size no longer matches its
// record is listed with the size observed, and one whose record or file cannot
// be read is listed with the cause, so the rest of the store still is.
func (a *mediaArea) Entries(ctx context.Context) ([]media.Image, error) {
	names, err := a.Names(ctx)
	if err != nil {
		return nil, err
	}
	images := []media.Image{}
	for _, name := range names {
		data, err := readBounded(ctx, a.dir, name+".json", managedos.MaxMediaRecord, true)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			if err := a.storeFailure(ctx); err != nil {
				return nil, err
			}
			images = append(images, media.Image{MediaEntry: managedos.MediaEntry{Name: name}, Failure: recordUnreadable})
			continue
		}
		entry, err := managedos.DecodeMediaRecord(data, name)
		if err != nil {
			if err := a.storeFailure(ctx); err != nil {
				return nil, err
			}
			images = append(images, media.Image{MediaEntry: managedos.MediaEntry{Name: name}, Failure: recordUndecodable})
			continue
		}
		size, err := a.size(name)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			if err := a.storeFailure(ctx); err != nil {
				return nil, err
			}
			images = append(images, media.Image{MediaEntry: entry, Failure: imageUnsafe})
			continue
		}
		images = append(images, media.Image{MediaEntry: entry, Observed: size})
	}
	return images, nil
}

// imageFailure is the cause this store gives for one image it cannot read.
// The media service tells it from every other refusal and lists that image
// failed with its reason, so it never refuses the rest of a listing.
type imageFailure struct{ reason string }

func (e imageFailure) Error() string { return e.reason }

func (imageFailure) Is(target error) bool { return target == media.ErrImageFailed }

// storeFailure is what fails a whole listing: cancellation, or a media
// directory that was replaced or closed.
func (a *mediaArea) storeFailure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.available(ctx, false)
}

// Entry reports the record published for one image when this store can read
// and decode it, whether or not the image's bytes still match it. It only shows
// and tells a confirmed entry from a changed one, so a damaged image stays
// replaceable and deletable.
func (a *mediaArea) Entry(ctx context.Context, name string) (managedos.MediaEntry, bool, error) {
	if err := a.available(ctx, false); err != nil {
		return managedos.MediaEntry{}, false, err
	}
	if a.dir == nil || !managedos.ValidMediaName(name) {
		return managedos.MediaEntry{}, false, nil
	}
	data, err := readBounded(ctx, a.dir, name+".json", managedos.MaxMediaRecord, true)
	if err != nil {
		return managedos.MediaEntry{}, false, ctx.Err()
	}
	entry, err := managedos.DecodeMediaRecord(data, name)
	return entry, err == nil, nil
}

// Retained lists every stage retained beside its record, as the entry that
// record states, sorted by image name. It is a read: it probes no stage lock
// and repairs nothing.
func (a *mediaArea) Retained(ctx context.Context) ([]managedos.MediaEntry, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	kept := []managedos.MediaEntry{}
	if a.dir == nil {
		return kept, nil
	}
	entries, err := directoryNames(a.dir, maxMediaDirectory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !stagedMediaName(entry) || !slices.Contains(entries, entry+".json") {
			continue
		}
		file, err := openRelative(a.dir, entry, pathHandle, 0)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			return nil, safeError(err)
		}
		opened, err := statHandle(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		pair, retained, err := retainedPairOf(ctx, a.dir, entry, opened)
		if err != nil {
			return nil, err
		}
		if retained {
			kept = append(kept, pair.entry)
		}
	}
	slices.SortFunc(kept, func(x, y managedos.MediaEntry) int { return strings.Compare(x.Name, y.Name) })
	return kept, nil
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

// Hold opens one listed image under the shared root lock and proves its file
// safe. The descriptor outlives the read, so the image is read in full after
// the lock is released. The image is opened without blocking, so a FIFO
// planted at its name cannot stall the open while the lock is held. Under the
// shared lock only an actor other than Bootwright removes an image, so a
// missing file is that image's failure, not the listing's.
func (a *mediaArea) Hold(ctx context.Context, name string) (media.Held, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	if a.dir == nil || !managedos.ValidMediaName(name) {
		absent := "the media store holds no image with that name"
		if managedos.ValidMediaName(name) {
			absent = "the media store holds no image named " + name
		}
		return nil, mediaFailure(absent, "list the store with bootwright media list")
	}
	file, err := openRelative(a.dir, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if err := a.storeFailure(ctx); err != nil {
			return nil, err
		}
		return nil, imageFailure{reason: imageUnopenable}
	}
	before, err := statHandle(file)
	if err != nil || !privateMediaFile(before, a.dir) {
		file.Close()
		return nil, imageFailure{reason: imageUnsafe}
	}
	return &mediaHeld{file: file, before: before}, nil
}

// mediaHeld is one image a listing opened under the shared lock. It keeps no
// reference to the area, whose callback has ended by the time it is read.
type mediaHeld struct {
	file   *os.File
	before syscall.Stat_t
	closed bool
}

// Digest reads the held image in full. Media is far larger than any record, so
// the bytes are hashed as they stream and never held in memory. The status of
// the descriptor at the end of the read must equal the one it had when it was
// opened, so a write, a truncation or an unlink meanwhile fails the image.
func (h *mediaHeld) Digest(ctx context.Context) (string, error) {
	digest, size, err := hashStream(ctx, h.file, managedos.MaxMediaBytes)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", imageFailure{reason: imageUnreadable}
	}
	after, err := statHandle(h.file)
	switch {
	case err != nil:
		return "", imageFailure{reason: imageChanged}
	case after.Nlink == 0:
		return "", imageFailure{reason: imageReplaced}
	case !sameFile(h.before, after) || size != after.Size:
		return "", imageFailure{reason: imageChanged}
	}
	return digest, nil
}

// Close releases the descriptor once.
func (h *mediaHeld) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	return h.file.Close()
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
			// A cancellation is reported as the cancellation, and a source
			// that names its own cause, such as a download's deadline,
			// keeps it.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", 0, ctxErr
			}
			if len(diagnostics.Of(err)) != 0 {
				return "", 0, err
			}
			return "", 0, mediaFailure("the image could not be read in full", "verify the source and repeat the command")
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), total, nil
}

// Reservations maps every image a context reserves to the contexts reserving
// it. A media claim is shared, so any number of contexts may hold one and it
// blocks only deletion and replacement.
func (a *mediaArea) Reservations(ctx context.Context) (map[string][]string, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	reserved := map[string][]string{}
	for _, reservation := range a.stored.value.Reservations {
		for _, key := range reservation.Keys {
			name, found := strings.CutPrefix(key, "media:")
			if !found || slices.Contains(reserved[name], reservation.Context) {
				continue
			}
			reserved[name] = append(reserved[name], reservation.Context)
		}
	}
	for _, holders := range reserved {
		slices.Sort(holders)
	}
	return reserved, nil
}

const (
	// maxStagedMedia bounds the stages that exist at once, live, retained or
	// abandoned: a stage is claimed only after abandoned ones are removed.
	maxStagedMedia = 16
	// maxMediaDirectory bounds the media directory: every image beside its
	// record, and the stages maxStagedMedia allows, each of which may carry
	// the record retained beside it; a record temporary file replaces the stage
	// its image was published from.
	maxMediaDirectory = 2*managedos.MaxMediaEntries + 2*maxStagedMedia
)

// stagedMediaName is a stage, which an acquisition writes holding no root lock.
func stagedMediaName(name string) bool { return identifier(name, "staging-") }

// pendingMediaName is a record's temporary file, which only a holder of the
// exclusive root lock writes.
func pendingMediaName(name string) bool { return identifier(name, "pending-") }

// retainedMediaRecordName is the record a pinned add retained beside its stage.
func retainedMediaRecordName(name string) bool {
	stage, found := strings.CutSuffix(name, ".json")
	return found && stagedMediaName(stage)
}

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
// that from claiming the stage until it publishes, retains or removes it, and
// the kernel releases it when the owner dies. Only a retained pair is kept,
// for the repetition of the add that retained it; a retained record whose stage
// is gone is removed. A pinned add retains its stage without the root lock, so
// its record may appear after the listing: whether an unheld stage has one is
// read beside it, never taken from the listing.
func (a *mediaArea) pruneStaging(ctx context.Context) error {
	entries, err := directoryNames(a.dir, maxMediaDirectory)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		pruned := false
		switch {
		case pendingMediaName(entry):
			pruned, err = a.pruneEntry(ctx, entry)
		case stagedMediaName(entry):
			pruned, err = a.pruneStage(ctx, entry)
		case retainedMediaRecordName(entry) && !slices.Contains(entries, strings.TrimSuffix(entry, ".json")):
			pruned, err = a.pruneEntry(ctx, entry)
		}
		if err != nil {
			return err
		}
		removed = removed || pruned
	}
	if !removed {
		return nil
	}
	return a.store.syncDirectory(ctx, a.dir)
}

func (a *mediaArea) pruneEntry(ctx context.Context, entry string) (bool, error) {
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaStagingPrune); err != nil {
		return false, err
	}
	return true, unlinkPresent(a.dir, entry)
}

// pruneStage removes a stage no process holds, after the record beside it,
// unless the two are a retained pair. The stage's owner writes that record
// while it holds the stage's lock, so once the stage is unheld its record is
// complete or never comes, and it is read then rather than from the listing.
func (a *mediaArea) pruneStage(ctx context.Context, stage string) (bool, error) {
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaStagingPrune); err != nil {
		return false, err
	}
	held, opened, err := a.stageHeld(stage)
	if err != nil || held {
		return false, err
	}
	if _, retained, err := retainedPairOf(ctx, a.dir, stage, opened); err != nil || retained {
		return false, err
	}
	if err := unlinkPresent(a.dir, stage+".json"); err != nil {
		return false, err
	}
	return true, unlinkPresent(a.dir, stage)
}

// unlinkPresent removes a name that another invocation may already have
// removed.
func unlinkPresent(dir *directory, name string) error {
	if err := syscall.Unlinkat(int(dir.file.Fd()), name); err != nil && !errors.Is(err, syscall.ENOENT) {
		return err
	}
	return nil
}

// stageHeld reports whether a live acquisition still holds the stage, and
// otherwise the status of the stage it opened. A stage its owner removed after
// the listing is not held; neither is an entry that is not a regular file,
// which no owner could hold.
func (a *mediaArea) stageHeld(name string) (bool, syscall.Stat_t, error) {
	probe, err := openRelative(a.dir, name, pathHandle, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, syscall.Stat_t{}, nil
	}
	if err != nil {
		return false, syscall.Stat_t{}, err
	}
	listed, err := statHandle(probe)
	probe.Close()
	if err != nil || listed.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return false, listed, err
	}
	file, err := openRelative(a.dir, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, syscall.Stat_t{}, nil
	}
	if err != nil {
		return false, syscall.Stat_t{}, err
	}
	defer unlockAndClose(file)
	opened, err := statHandle(file)
	if err != nil || !sameIdentity(listed, opened) {
		return false, syscall.Stat_t{}, state("media staging changed while it was inspected")
	}
	switch err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return true, opened, nil
	case err != nil:
		return false, syscall.Stat_t{}, state("media staging lock cannot be inspected")
	}
	return false, opened, nil
}

// unlockAndClose releases a stage lock before closing its descriptor. The lock
// belongs to the open file description, which a child forked meanwhile shares
// until it execs, so closing the descriptor alone can leave the stage held.
func unlockAndClose(file *os.File) {
	syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	file.Close()
}

// retainedPair is a stage a pinned add retained: the entry the record beside
// it states, and that record's identity.
type retainedPair struct {
	entry  managedos.MediaEntry
	record syscall.Stat_t
}

// retainedPairOf proves that a stage and the record beside it are retained,
// given the status of the stage's opened handle: the record decodes within its
// bound for the image whose stage this is, and the stage is a private file of
// exactly the size the record states. Anything else is no pair.
func retainedPairOf(ctx context.Context, dir *directory, stage string, opened syscall.Stat_t) (retainedPair, bool, error) {
	data, record, err := readBoundedIdentity(ctx, dir, stage+".json", managedos.MaxMediaRecord, true)
	if err != nil {
		return retainedPair{}, false, ctx.Err()
	}
	entry, err := managedos.DecodeStagedMediaRecord(data)
	if err != nil || mediaStageName(entry.Name) != stage || !privateMediaFile(opened, dir) || opened.Size != entry.Size {
		return retainedPair{}, false, nil
	}
	return retainedPair{entry: entry, record: record}, true, nil
}

// Stage claims private staging for one image. It lives in the media directory,
// so publishing it is a rename within one filesystem, and it is named by its
// image, so creating it fails while a live stage holds that name. Abandoned
// stages were pruned before this callback, so every stage listed is live or
// retained. The image's own retained stage is adopted when the pin names its
// digest and removed otherwise, so it never counts against this claim's bound.
func (a *mediaArea) Stage(ctx context.Context, name, pin string) (media.Stage, error) {
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
	own := mediaStageName(name)
	if len(slices.DeleteFunc(entries, func(entry string) bool { return !stagedMediaName(entry) || entry == own })) >= maxStagedMedia {
		return nil, mediaFailure("the media store is already acquiring its maximum number of images",
			"retry after another bootwright media add finishes, or discard an image retained for a repeated add with bootwright media delete --name <name>")
	}
	stage, err := a.openStage(ctx, name)
	if err != nil {
		return nil, err
	}
	retained, err := stage.lockRetained()
	if err == nil && retained {
		retained, err = stage.adopt(ctx, pin)
	}
	if err != nil {
		stage.release()
		return nil, err
	}
	if retained {
		return stage, nil
	}
	if err := stage.claim(ctx); err != nil {
		return nil, err
	}
	return stage, nil
}

// claim creates this image's stage exclusively and takes its lock. Every
// failure releases the stage's handles.
func (s *mediaStage) claim(ctx context.Context) error {
	if err := s.store.checkpoint(ctx, checkpointBeforeMediaStaging); err != nil {
		s.release()
		return err
	}
	file, err := openRelative(s.dir, s.name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if errors.Is(err, syscall.EEXIST) {
		s.release()
		return acquiringFailure(s.image)
	}
	if err != nil {
		s.release()
		return safeError(err)
	}
	s.file = file
	created, err := statHandle(file)
	if err != nil || !privateMediaFile(created, s.dir) || created.Size != 0 ||
		syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		s.Close()
		return state("new media staging file is unsafe")
	}
	s.created = created
	return nil
}

func acquiringFailure(image string) error {
	return mediaFailure("another bootwright media add is already acquiring image "+image,
		"wait for it to finish, then review the store with bootwright media list")
}

// lockRetained takes the lock of this image's stage when a record is retained
// beside it, and reports whether one is. A stage another process holds is
// live, so it refuses as a second add of the image does; a record whose stage
// is gone leaves nothing to lock.
func (s *mediaStage) lockRetained() (bool, error) {
	record, err := openRelative(s.dir, s.name+".json", pathHandle, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, safeError(err)
	}
	record.Close()
	file, err := openRelative(s.dir, s.name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return true, nil
	}
	if err != nil {
		return false, safeError(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, acquiringFailure(s.image)
		}
		return false, state("media staging lock cannot be taken")
	}
	s.file = file
	return true, nil
}

// adopt keeps the locked retained stage when its pair names this image and the
// pin is its recorded digest. Anything else is discarded, so the claim starts
// fresh.
func (s *mediaStage) adopt(ctx context.Context, pin string) (bool, error) {
	if s.file != nil {
		opened, err := statHandle(s.file)
		if err != nil {
			return false, err
		}
		pair, retained, err := retainedPairOf(ctx, s.dir, s.name, opened)
		if err != nil {
			return false, err
		}
		if retained && pair.entry.Name == s.image && pin != "" && pair.entry.SHA256 == pin {
			s.adopted, s.record, s.created = &pair.entry, pair.record, opened
			return true, nil
		}
	}
	return false, s.discard(ctx)
}

// discard removes this image's retained record, then its stage, while it holds
// the stage's lock, and then releases that lock.
func (s *mediaStage) discard(ctx context.Context) error {
	if err := s.store.checkpoint(ctx, checkpointBeforeMediaRetainedRemoval); err != nil {
		return err
	}
	if err := unlinkPresent(s.dir, s.name+".json"); err != nil {
		return safeError(err)
	}
	if s.file != nil {
		opened, err := statHandle(s.file)
		if err != nil {
			return err
		}
		if err := unlinkVerified(s.dir, s.name, opened, false); err != nil {
			return err
		}
	}
	if err := s.store.syncDirectory(ctx, s.dir); err != nil {
		return err
	}
	if s.file != nil {
		unlockAndClose(s.file)
		s.file = nil
	}
	return nil
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

// mediaStage is one claimed or adopted stage. Its owner holds an exclusive
// lock on the file from the claim until Close, which is how pruneStaging tells
// it from an abandoned one. It belongs to one invocation and is used
// sequentially. An adopted stage carries the entry and record identity it was
// retained with, and mismatch once Verify found other bytes. measured is the
// stage's status when Fill or Verify measured its bytes, which publication
// proves unchanged.
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
	measured  syscall.Stat_t
	adopted   *managedos.MediaEntry
	record    syscall.Stat_t
	mismatch  bool
	retained  bool
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
	if s.closed || s.filling || s.adopted != nil {
		return media.Staged{}, state("media stage is closed, adopted or already filled")
	}
	if source == nil || limit <= 0 || limit > managedos.MaxMediaBytes {
		return media.Staged{}, state("media staging bounds are invalid")
	}
	s.filling = true
	stage := &stageWriter{file: s.file}
	digest, size, err := hashStream(ctx, io.TeeReader(source, stage), limit)
	if err != nil {
		if stage.err != nil && ctx.Err() == nil {
			return media.Staged{}, mediaWriteFailure(stage.err)
		}
		return media.Staged{}, err
	}
	if err := s.store.checkpoint(ctx, checkpointBeforeMediaStagingSync); err != nil {
		return media.Staged{}, err
	}
	if err := s.file.Sync(); err != nil {
		if _, found := storeErrno(err); found {
			return media.Staged{}, mediaWriteFailure(err)
		}
		return media.Staged{}, state("media staging durability could not be established")
	}
	after, err := statHandle(s.file)
	if err != nil || !sameIdentity(s.created, after) || after.Size != size {
		return media.Staged{}, state("media staging file changed while it was written")
	}
	s.staged, s.measured = &media.Staged{Size: size, SHA256: digest}, after
	return *s.staged, nil
}

// Retained reports the entry an adopted stage was retained with.
func (s *mediaStage) Retained() (managedos.MediaEntry, bool) {
	if s.adopted == nil {
		return managedos.MediaEntry{}, false
	}
	return *s.adopted, true
}

// Verify re-reads an adopted stage in full with no root lock held, as Digest
// reads an image, and records what it measured and the stage's status then:
// publication proves the stage unchanged since, and Close removes a stage
// whose bytes no longer match what was retained.
func (s *mediaStage) Verify(ctx context.Context) (media.Staged, error) {
	if err := ctx.Err(); err != nil {
		return media.Staged{}, err
	}
	if s.closed || s.adopted == nil || s.staged != nil {
		return media.Staged{}, state("media stage is not an adopted stage awaiting verification")
	}
	before, err := statHandle(s.file)
	if err != nil || !privateMediaFile(before, s.dir) {
		return media.Staged{}, state("media file type, owner, permissions or links is unsafe")
	}
	digest, size, err := hashStream(ctx, s.file, managedos.MaxMediaBytes)
	if err != nil {
		return media.Staged{}, err
	}
	after, err := statHandle(s.file)
	if err != nil || !sameFile(before, after) || size != after.Size {
		return media.Staged{}, state("media file changed while it was being read")
	}
	s.staged, s.measured = &media.Staged{Size: size, SHA256: digest}, after
	s.mismatch = size != s.adopted.Size || digest != s.adopted.SHA256
	return *s.staged, nil
}

// Retain keeps this filled, unpublished stage for the add's repetition: still
// holding the stage's lock and no root lock, it writes the record the add
// would have published exclusively beside the stage, never through a pending-
// temporary file, which a concurrent exclusive holder's prune removes as
// abandoned. An adopted stage is retained already, so it writes nothing.
func (s *mediaStage) Retain(ctx context.Context, record []byte) error {
	if s.closed || s.published || s.retained || s.staged == nil || s.mismatch {
		return state("media stage is not a filled, unpublished stage")
	}
	if s.adopted != nil {
		s.retained = true
		return nil
	}
	entry, err := managedos.DecodeStagedMediaRecord(record)
	if err != nil {
		return err
	}
	if entry.Name != s.image || entry.Size != s.staged.Size || entry.SHA256 != s.staged.SHA256 {
		return state("media retention does not describe this stage")
	}
	if err := s.at(s.dir, s.name, false); err != nil {
		return err
	}
	if err := s.store.checkpoint(ctx, checkpointBeforeMediaRetention); err != nil {
		return err
	}
	if err := s.store.writeExclusive(ctx, s.dir, s.name+".json", record); err != nil {
		return safeError(err)
	}
	s.retained = true
	return nil
}

// Close removes an unpublished stage while its lock is still held, then
// releases it. A retained stage stays, and so does an adopted one unless
// Verify found other bytes in it. A stage it cannot prove is its own stays for
// pruneStaging.
func (s *mediaStage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.file != nil && !s.published && !s.retained && (s.adopted == nil || s.mismatch) {
		err = s.remove()
	}
	s.release()
	return err
}

// remove deletes this stage, after the record retained beside an adopted one.
func (s *mediaStage) remove() error {
	held, err := statHandle(s.file)
	if err != nil {
		return err
	}
	if s.adopted != nil {
		if err := unlinkPresent(s.dir, s.name+".json"); err != nil {
			return safeError(err)
		}
	}
	if err := unlinkVerified(s.dir, s.name, held, false); err != nil {
		return err
	}
	return s.dir.file.Sync()
}

func (s *mediaStage) release() {
	if s.file != nil {
		unlockAndClose(s.file)
	}
	s.dir.file.Close()
	s.root.file.Close()
}

// at proves that the named entry is this stage's file and that its status is
// still the one taken when its bytes were measured. A rename sets the change
// time, so once renamed the change time alone is not compared.
func (s *mediaStage) at(dir *directory, name string, renamed bool) error {
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
	measured := s.measured
	if renamed {
		measured.Ctim = held.Ctim
	}
	if err != nil || !sameFile(held, named) || !sameFile(measured, held) || !privateMediaFile(named, dir) {
		return state("media stage was substituted or changed before publication")
	}
	return nil
}

// Publish installs a filled stage and its record. A replacement removes the
// entry it supersedes first, so a reader never sees a record whose bytes have
// already changed. An adopted stage's retained record goes last, once the
// image's own record stands.
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
	if stage.closed || stage.published || stage.staged == nil || stage.mismatch || stage.image != name || a.dir == nil ||
		!sameIdentity(stage.dir.identity, a.dir.identity) || entry.Size != stage.staged.Size || entry.SHA256 != stage.staged.SHA256 {
		return state("media publication does not describe a filled stage of this store")
	}
	if err := stage.at(a.dir, stage.name, false); err != nil {
		return err
	}
	if replace {
		if err := a.remove(ctx, name); err != nil {
			return err
		}
	}
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaRename); err != nil {
		return err
	}
	if err := renameNoReplaceAt(a.dir, stage.name, name); err != nil {
		return safeError(err)
	}
	stage.published = true
	if err := stage.at(a.dir, name, true); err != nil {
		return state("published media bytes are not the staged image")
	}
	if err := a.store.syncDirectory(ctx, a.dir); err != nil {
		return err
	}
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaRecord); err != nil {
		return err
	}
	if err := a.store.writeExclusiveAtomic(ctx, a.dir, name+".json", record, false); err != nil {
		return safeError(err)
	}
	if stage.adopted != nil {
		if err := a.store.checkpoint(ctx, checkpointBeforeMediaRetainedRemoval); err != nil {
			return err
		}
		if err := unlinkVerified(a.dir, stage.name+".json", stage.record, false); err != nil {
			return err
		}
		if err := a.store.syncDirectory(ctx, a.dir); err != nil {
			return err
		}
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
	if err := a.discardRetained(ctx, name); err != nil {
		return err
	}
	occupied, err := a.Names(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(occupied, name) {
		if err := a.remove(ctx, name); err != nil {
			return err
		}
	}
	return a.available(ctx, true)
}

// discardRetained removes the stage retained for an image, with its record,
// once it holds the stage's lock: a stage another process holds is live, and
// the deletion refuses before it removes anything.
func (a *mediaArea) discardRetained(ctx context.Context, image string) error {
	stage, err := a.openStage(ctx, image)
	if err != nil {
		return err
	}
	defer stage.release()
	retained, err := stage.lockRetained()
	if err != nil || !retained {
		return err
	}
	return stage.discard(ctx)
}

// remove drops the record before the bytes, so an interruption leaves an
// occupied name with no record rather than a record describing absent bytes.
func (a *mediaArea) remove(ctx context.Context, name string) error {
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaRecordRemoval); err != nil {
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
	if err := a.store.checkpoint(ctx, checkpointBeforeMediaImageRemoval); err != nil {
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

// stageWriter records the error the stage's own write returned, so a store
// that cannot hold the image is never reported as a source that failed.
type stageWriter struct {
	file *os.File
	err  error
}

func (w *stageWriter) Write(data []byte) (int, error) {
	n, err := w.file.Write(data)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

// mediaWriteFailure names what the filesystem answered a stage write or sync:
// a capacity answer as the room the media store lacks.
func mediaWriteFailure(cause error) error {
	const device = "inspect the filesystem that holds /var/lib/bootwright/media (its kernel log names the device error), then repeat the command"
	errno, found := storeErrno(cause)
	switch {
	case !found:
		return mediaFailure("the image could not be written to the media store", device)
	case capacityErrno(errno):
		return mediaFailure("the media store could not hold the image: "+errnoText(errno),
			"free space, or raise the quota, on the filesystem that holds /var/lib/bootwright/media, then repeat the command")
	}
	return mediaFailure("the image could not be written to the media store: "+errnoText(errno), device)
}
