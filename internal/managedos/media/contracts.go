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

// View is a coherent read of the store taken under its shared lock.
type View interface {
	// Entries lists every complete image with its published record.
	Entries(context.Context) ([]managedos.MediaEntry, error)
	// Names lists every occupied name, including an incomplete publication, so
	// a new image never collides with bytes this store still holds.
	Names(context.Context) ([]string, error)
	// Digest reads one image in full and reports its current content digest.
	Digest(context.Context, string) (string, error)
	// Frozen names every image a context reserves, in any context.
	Frozen(context.Context) ([]string, error)
	// Entry reports the record published for one image, whether or not its
	// bytes still match it, so a store that refuses to list a damaged image
	// still confirms its replacement or deletion.
	Entry(context.Context, string) (managedos.MediaEntry, bool, error)
	// Retained names every image whose verified stage a pinned add kept
	// because its publication met another command's lock.
	Retained(context.Context) ([]string, error)
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

// Acquisition is one opened media source and the credential-free origin the
// published record retains.
type Acquisition struct {
	Payload Payload
	Origin  string
}

// Acquirer opens exactly one authorized media source. It performs no retry,
// follows no redirect and never reads an ambient credential.
type Acquirer interface {
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

type Clock interface {
	Now() time.Time
}
