package media

import (
	"context"
	"errors"
	"time"

	"github.com/crmarques/bootwright/internal/managedos"
)

// Payload streams one acquired image. It is the only shape bytes cross this
// service in, so the use case reads no file, socket or stream of its own.
type Payload interface {
	Read([]byte) (int, error)
	Close() error
}

// ErrBusy marks a store command that could not take the store's lock because
// another command holds it. Nothing is wrong with the store: the same command
// succeeds once that command finishes.
var ErrBusy = errors.New("the media store is held by another command")

// Store is the host-wide media area. Media is shared by every context, so a
// callback holds the store's root coordination and no context lease. A Stage
// is the one handle that outlives a callback: it lets an image be acquired
// while the store holds no root lock.
type Store interface {
	ReadMedia(context.Context, func(View) error) error
	MutateMedia(context.Context, func(Transaction) error) error
}

// Image is one complete image as a view lists it: the record published for it
// and the size of the bytes the store holds now. Observed differs from the
// record's Size when the image was shortened or extended after publication.
// Failure is the cause the store gives for an image it lists but cannot read
// (its record, or its file). When Failure is set, MediaEntry carries only Name
// unless the record decoded, and Observed is meaningless.
type Image struct {
	managedos.MediaEntry
	Observed int64
	Failure  string
}

// ErrImageFailed marks the cause a store gives for one listed image it cannot
// read. It fails that image alone, never the rest of a listing. The store's
// error matches it with errors.Is, and its own text, unwrapped, is the reason.
var ErrImageFailed = errors.New("the media store cannot read this image")

// imageFailureReason reports the reason a store gave for one image it cannot
// read, and whether err is such a cause at all.
func imageFailureReason(err error) (string, bool) {
	if err == nil || !errors.Is(err, ErrImageFailed) {
		return "", false
	}
	return err.Error(), true
}

// Held is one stored image a read opened. Using it takes no lock.
type Held interface {
	// Digest reads the held image in full and reports its content digest. It
	// refuses with an error matching ErrImageFailed when the file changed, or
	// was deleted or replaced, while it was read, and with the cancellation
	// when ctx ends.
	Digest(context.Context) (string, error)
	// Close releases the handle. It takes no context, so a canceled listing
	// still releases it.
	Close() error
}

// View is a coherent read of the store taken under its shared lock.
type View interface {
	// Entries lists every complete image with its published record and its
	// observed size, whether or not that size still matches the record, so
	// one damaged image never hides the rest of the store. An image whose
	// record or file the store cannot read is listed with its Failure.
	Entries(context.Context) ([]Image, error)
	// Names lists every occupied name, including an incomplete publication, so
	// a new image never collides with bytes this store still holds.
	Names(context.Context) ([]string, error)
	// Hold opens one listed image under the store's shared lock and proves its
	// file safe. The handle outlives the read, so the image is read in full
	// with no root lock held. A cause that concerns that image alone matches
	// ErrImageFailed.
	Hold(context.Context, string) (Held, error)
	// Reservations maps every image a context reserves to the contexts that
	// reserve it, each sorted and named once.
	Reservations(context.Context) (map[string][]string, error)
	// Entry reports the record published for one image, whether or not its
	// bytes still match it, so a confirmation shows what it would replace or
	// delete and the hold that acts on it proves that record unchanged.
	Entry(context.Context, string) (managedos.MediaEntry, bool, error)
	// Retained lists every verified stage a pinned add kept because its
	// publication met another command's lock, as the entry it was verified
	// as, so a replacement's confirmation shows the source of the stage its
	// pin adopts.
	Retained(context.Context) ([]managedos.MediaEntry, error)
}

// Staged is one bounded image written into a stage, with the exact bytes the
// store observed while writing it.
type Staged struct {
	Size   int64
	SHA256 string
}

// Stage is private staging a transaction claimed for one image name. It
// outlives that transaction, so the image is acquired into it while the store
// holds no root lock, and a later transaction publishes it. While a stage
// lives, no other stage can claim its name.
type Stage interface {
	// Fill copies the payload into the stage under the byte limit and reports
	// what it wrote. It holds no root lock and publishes nothing.
	Fill(context.Context, Payload, int64) (Staged, error)
	// Retained reports the entry a retained stage was verified as, when this
	// stage adopted one rather than claiming fresh staging.
	Retained() (managedos.MediaEntry, bool)
	// Verify re-reads an adopted stage in full, holding no root lock, and
	// reports the bytes it holds now.
	Verify(context.Context) (Staged, error)
	// Retain keeps a filled, unpublished stage beside the record it would have
	// published, so a repeated add publishes it without acquiring it again.
	Retain(context.Context, []byte) error
	// Close discards the stage unless it was published or retained, and an
	// adopted stage only once Verify found other bytes in it. It takes no
	// context, so a cancelled acquisition still removes what it wrote.
	Close() error
}

type Transaction interface {
	View
	// Stage claims private staging for the named image. It refuses while
	// another live stage holds that name. It adopts the name's retained stage
	// when the digest pin equals the retained one, and otherwise removes that
	// stage before claiming fresh staging.
	Stage(ctx context.Context, name, sha256 string) (Stage, error)
	// Publish atomically installs a filled stage as the named image with its
	// record.
	Publish(context.Context, string, Stage, []byte, bool) error
	// Delete removes the named image and its record, and the stage retained
	// for that name.
	Delete(context.Context, string) error
}

// Acquisition is one opened media source.
type Acquisition struct {
	Payload Payload
}

// Acquirer opens exactly one authorized media source. It performs no retry,
// follows no redirect and never reads an ambient credential.
type Acquirer interface {
	// Origin reports the credential-free origin the published record retains
	// for a source, refusing what Open would refuse. It opens nothing, so the
	// origin is decided before anything is confirmed, claimed or acquired.
	Origin(Source) (string, error)
	Open(context.Context, Source) (Acquisition, error)
}

// Source is one validated media origin; exactly one field is populated.
type Source struct {
	Path string
	URL  string
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

// ProgressEvent is one row of a media command's progress. Check marks a proof
// about an image already stored, and every other event is a step of the
// change the command makes. Step identifies the step and Label names it;
// Detail is what a running step is doing or the summary a settled one proved.
type ProgressEvent struct {
	Check    bool
	Step     string
	Label    string
	Detail   string
	Status   string
	Position int
	Total    int
}

// Reporter receives a media command's progress while it runs. Reporting is
// presentation only: it never changes an effect or an outcome.
type Reporter interface {
	ReportProgress(context.Context, ProgressEvent)
}

// The changes a media confirmation authorizes.
const (
	ReplaceChange = "replace"
	DeleteChange  = "delete"
)

// Change is what one confirmation would authorize. Stored reports whether the
// name holds a stored image, and Readable whether its record could be read,
// which is what Entry then holds. Retained reports that a deletion also
// removes the stage an interrupted add kept, and NewOrigin is the origin a
// replacement would record: the source of the retained stage its pin adopts,
// or else the origin of its own source.
type Change struct {
	Action    string
	Name      string
	Stored    bool
	Readable  bool
	Entry     managedos.MediaEntry
	Retained  bool
	NewOrigin string
}

// Presenter shows the stored image a confirmation would replace or delete
// before the prompt, so an operator authorizes a change to an image they have
// seen. A presentation that fails refuses the command without prompting.
type Presenter interface {
	PresentMediaChange(context.Context, Change) error
}

type Clock interface {
	Now() time.Time
}
