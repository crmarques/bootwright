package secretstore

import (
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Context struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Revision string `json:"-"`
}

type ContextSnapshot struct {
	Context         Context
	Inputs          desiredstate.Sources
	SecretStoreType string
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
	Context         string `json:"context"`
	Backend         string `json:"backend"`
	Generation      string `json:"generation"`
}

type SessionRequirement struct{ Kind string }

type Version struct {
	ID string `json:"id"`
	// Sequence is the version's durable per-secret ordinal, counting from one.
	// The identifier remains the only durable reference; the ordinal exists so
	// a person can name a version without reading a digest.
	Sequence    int                        `json:"sequence"`
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

func Failure(code, message string) error { return diagnostics.NewFailure("secret."+code, message, "") }
