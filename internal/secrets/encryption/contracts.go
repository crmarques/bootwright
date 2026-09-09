package encryption

import "github.com/crmarques/bootwright/internal/secrets/storage"

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

func componentStatus(ref storage.ComponentRef) ComponentStatus {
	return ComponentStatus{ID: ref.ID, InterfaceVersion: ref.InterfaceVersion, StateVersion: ref.StateVersion, ConfigVersion: ref.ConfigVersion}
}

type ItemStatus struct {
	CurrentVersions   int  `json:"currentVersions"`
	BoundVersions     int  `json:"boundVersions"`
	MaterialParts     int  `json:"materialParts"`
	RetainedArtifacts int  `json:"retainedArtifacts"`
	CleanupRequired   bool `json:"cleanupRequired"`
}
type StatusResult struct {
	Initialized    bool                  `json:"initialized"`
	Implementation *ImplementationStatus `json:"implementation"`
	ActiveKey      *string               `json:"activeKey"`
	Keys           []storage.Key         `json:"keys"`
	Items          ItemStatus            `json:"items"`
}
type MutationResult struct {
	Context        storage.Context
	Implementation storage.Selection
	ActiveKey      string
	Changed        bool
}
