package encryption

import (
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type EncryptionInitRequest struct {
	ContextName string
}

type EncryptionStatusRequest struct{ ContextName string }

type EncryptionRotateRequest struct {
	ContextName      string
	SkipConfirmation bool
}

type ImplementationStatus struct {
	Type       string          `json:"type"`
	Store      ComponentStatus `json:"store"`
	KeyCustody ComponentStatus `json:"keyCustody"`
	State      string          `json:"state"`
}

type ComponentStatus struct {
	ID               string `json:"id"`
	InterfaceVersion int    `json:"interfaceVersion"`
	StateVersion     int    `json:"stateVersion"`
	ConfigVersion    int    `json:"configVersion"`
}

type ItemStatus struct {
	CurrentVersions   int  `json:"currentVersions"`
	BoundVersions     int  `json:"boundVersions"`
	MaterialParts     int  `json:"materialParts"`
	RetainedArtifacts int  `json:"retainedArtifacts"`
	CleanupRequired   bool `json:"cleanupRequired"`
}

// StatusResult is the store's status. Context names the context it was read
// from, which a next step names; the documented status JSON carries none.
type StatusResult struct {
	Context        secretstore.Context   `json:"-"`
	Initialized    bool                  `json:"initialized"`
	Implementation *ImplementationStatus `json:"implementation"`
	ActiveKey      *string               `json:"activeKey"`
	Keys           []secretstore.Key     `json:"keys"`
	Items          ItemStatus            `json:"items"`
}

// MutationResult reports an initialization or a rotation. A rotation names
// the keys it retired and counts the versions and parts it re-encrypted.
type MutationResult struct {
	Context             secretstore.Context
	Implementation      secretstore.Selection
	ActiveKey           string
	Changed             bool
	RetiredKeys         []string
	ReencryptedVersions int
	ReencryptedParts    int
}

func componentStatus(ref secretstore.ComponentRef) ComponentStatus {
	return ComponentStatus{ID: ref.ID, InterfaceVersion: ref.InterfaceVersion, StateVersion: ref.StateVersion, ConfigVersion: ref.ConfigVersion}
}
