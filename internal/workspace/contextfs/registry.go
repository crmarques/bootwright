package contextfs

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// earlierBuildGuidance is the remedy for a root this build cannot read as its
// own. It never says to move the root aside: another build's services may be
// running from it.
const earlierBuildGuidance = "another Bootwright build may have created this root and may manage live environments there: " +
	"run this build on another controller host, or retire that build's environments before archiving its root, " +
	"and never move it aside while its services run"

const (
	missingRegistryMessage   = "the state root holds state but no registry.json this build can read"
	storeRecoveryRemediation = earlierBuildGuidance + "; a store this build created is restored whole from a matching backup"
)

// The setup v2 cut-over (D152) reads no store an earlier format wrote; the
// remedy is a fresh install, never a conversion.
const (
	freshInstallMessage     = "/var/lib/bootwright holds a store an earlier Bootwright build wrote (registry version 5), which this build does not read or convert"
	freshInstallRemediation = "with the build that wrote it, destroy each environment (bootwright destroy --context <name>) and delete each context " +
		"(bootwright context delete --name <name>); then, as root, remove /var/lib/bootwright and /var/lib/bootwright-staging and run bootwright setup " +
		"with this build. Never remove /var/lib/bootwright while services it manages run."
	cutOverRegistryVersion = 5
)

type registryRecord struct {
	Version    int                            `json:"version"`
	Contexts   []contexts.Record              `json:"contexts"`
	Controller *contexts.ControllerDescriptor `json:"controller,omitempty"`
}

func registryDocument(registry contexts.Registry) any {
	record := registryRecord{Version: registry.Version, Contexts: registry.Contexts}
	if registry.Controller != (contexts.ControllerDescriptor{}) {
		descriptor := registry.Controller
		record.Controller = &descriptor
	}
	return record
}

func decodeRegistry(data []byte, maximum int, target *contexts.Registry) error {
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return state("context registry is malformed")
	}
	if envelope.Version == cutOverRegistryVersion {
		return contexts.StateErrorWithRemediation(freshInstallMessage, freshInstallRemediation)
	}
	if envelope.Version != contexts.RegistryVersion {
		// Any other version is another build's state, never damage this build
		// may convert or move aside.
		return contexts.StateErrorWithRemediation(missingRegistryMessage, storeRecoveryRemediation)
	}
	var record registryRecord
	if err := decodeRecord(data, maximum, &record); err != nil {
		return err
	}
	*target = contexts.Registry{Version: record.Version, Contexts: record.Contexts}
	if record.Controller != nil {
		target.Controller = *record.Controller
	}
	return nil
}
