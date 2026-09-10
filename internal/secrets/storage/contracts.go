package storage

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Context struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Mode     string `json:"mode"`
	Revision string `json:"-"`
}

type ContextSnapshot struct {
	Context         Context
	Inputs          desiredstate.Sources
	SecretStoreType string
}

// Workspace provides a coherent input snapshot and confines every store effect
// to its context. Mutate holds the root lock followed by the context lease.
type Workspace interface {
	SecretContext(context.Context, string) (ContextSnapshot, error)
	ReadSecrets(context.Context, Context, func(Area) error) error
	MutateSecrets(context.Context, Context, func(Area) error) error
}

type Entry struct {
	Name      string
	Directory bool
	Size      int64
}

type RecordExpectation struct {
	Path string
	Data []byte
}

// Area is valid only during its Workspace callback. Paths contain at most two
// safe segments. Implementations bound enumeration to 32768 entries and total
// physical bytes to 256 MiB. Read-only areas refuse every mutation.
type Area interface {
	Read(context.Context, string, int) ([]byte, bool, error)
	// ReadMutable remembers the opened inode and bytes (or verified absence).
	// Replace requires that same identity, not only equal content.
	ReadMutable(context.Context, string, int) ([]byte, bool, error)
	Entries(context.Context, string) ([]Entry, error)
	EnsureDirectory(context.Context, string) error
	// WriteExclusive reserves the final path before streaming. Backends use it
	// only for artifacts whose names are attributable during crash recovery and
	// remain hidden until a later selector publication.
	WriteExclusive(context.Context, string, []byte) error
	// PublishExclusive exposes a fully written immutable artifact atomically and
	// never replaces an existing final path. A crash may retain an unselected temporary file.
	PublishExclusive(context.Context, string, []byte) error
	Replace(ctx context.Context, path string, replacement, expected []byte) (Outcome, error)
	// Prune removes only previously observed files or empty directories after
	// verifying and synchronizing the exact current store.json. It may run
	// before publication writes or after a durably committed store replacement;
	// uncertain publication and read-only access refuse cleanup. General writes
	// remain forbidden after store publication.
	Prune(ctx context.Context, expectedStore []byte, paths []string) error
	// PruneUnpublished removes attributable unpublished artifacts only while
	// store.json remains absent. Guards are exact prior ReadMutable snapshots
	// of 1..8 root files, bounded to 8 MiB together, synchronized before cleanup
	// and revalidated at every removal. Guard files cannot be cleanup targets.
	PruneUnpublished(ctx context.Context, guards []RecordExpectation, paths []string) error
	// SyncFile establishes file and parent durability only for the exact object
	// previously observed by Entries or ReadMutable. Read-only access and
	// committed or uncertain metadata publication refuse this effect.
	SyncFile(context.Context, string) error
	Sync(context.Context, string) error
}

type Outcome string

const (
	NotCommitted Outcome = "not-committed"
	Committed    Outcome = "committed"
	Uncertain    Outcome = "uncertain"
)

type ComponentRef struct {
	ID               string   `json:"id"`
	InterfaceVersion int      `json:"interfaceVersion"`
	StateVersion     int      `json:"stateVersion"`
	ConfigVersion    int      `json:"configVersion"`
	Config           struct{} `json:"config"`
}

type Selection struct {
	Type       string       `json:"type"`
	Store      ComponentRef `json:"store"`
	KeyCustody ComponentRef `json:"keyCustody"`
}

type Selector struct {
	SelectorVersion int    `json:"version"`
	ContextID       string `json:"contextId"`
	Backend         string `json:"backend"`
	Generation      string `json:"generation"`
}

type SessionRequirement struct{ Kind string }

// SessionMaterial is an invocation-owned, non-serializable unlock capability.
// No raw credential is carried by a command request, selector, or result.
type SessionMaterial interface{ Close() error }
type SessionMaterialSource interface {
	Acquire(context.Context, Context, Selection, []SessionRequirement) (SessionMaterial, error)
}

type SecretStoreImplementation interface {
	Backend() string
	Selection() Selection
	Requirements() []SessionRequirement
	// Initialize may resume attributable initialization, perform an explicitly
	// supported upgrade, or finish cleanup of authenticated published state.
	// Unknown nonempty state is never an uninitialized store.
	Initialize(context.Context, Context, Area, SessionMaterial) (StoreSession, error)
	Open(context.Context, Context, Area, Selector, SessionMaterial) (StoreSession, error)
}

type Version struct {
	ID          string                     `json:"id"`
	Declaration secrets.VersionDeclaration `json:"declaration"`
	Parts       []secrets.Part             `json:"parts"`
}
type Current struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type Binding struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions"`
}
type Key struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Seals uint64 `json:"seals"`
}
type Snapshot struct {
	Versions          []Version
	Current           []Current
	Bindings          []Binding
	ActiveKey         string
	Keys              []Key
	RetainedArtifacts int
	CleanupRequired   bool
}

type Put struct {
	Declaration secrets.Declaration
	Material    secrets.Material
}
type BoundInput struct {
	Declaration secrets.Declaration
	Version     string
	Material    secrets.Material
}
type BoundMaterial struct {
	Version  Version
	Material secrets.Material
}

// StoreSession implements one selected backend. Its read methods perform no
// writes; mutation methods atomically publish one complete semantic transition.
type StoreSession interface {
	Inspect(context.Context) (Snapshot, error)
	Read(context.Context, string) (secrets.Material, error)
	PutBatch(context.Context, []Put) ([]Version, error)
	Delete(context.Context, string) (bool, error)
	Bind(context.Context, []BoundInput) (Binding, error)
	Reopen(context.Context, string) ([]BoundMaterial, error)
	Release(context.Context, string) (bool, error)
	Rotate(context.Context) (string, error)
	Close() error
}

func Failure(code, message string) error { return desiredstate.NewFailure("secret."+code, message, "") }
