package contextfs

import (
	"encoding/json"
	"errors"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// otherBuildGuidance is the remedy for a root this build cannot read as its
// own. It never says to move the root aside: another build's services may be
// running from it.
const otherBuildGuidance = "another Bootwright build may have created this root and may manage live environments there: " +
	"run this build on another controller host, or retire that build's environments before archiving its root, " +
	"and never move it aside while its services run; once none of them remains, bootwright controller uninstall --discard-unreadable removes it with every root Bootwright keeps on this host"

type anotherBuildsStore struct{ failure error }

func (e *anotherBuildsStore) Error() string { return e.failure.Error() }

func (e *anotherBuildsStore) Unwrap() error { return e.failure }

func anotherBuilds(failure error) error { return &anotherBuildsStore{failure: failure} }

func isAnotherBuilds(err error) bool {
	var another *anotherBuildsStore
	return errors.As(err, &another)
}

type unattributedEntryState struct{ failure error }

func (e *unattributedEntryState) Error() string { return e.failure.Error() }

func (e *unattributedEntryState) Unwrap() error { return e.failure }

func isUnattributedEntry(err error) bool {
	var unattributed *unattributedEntryState
	return errors.As(err, &unattributed)
}

const (
	missingRegistryMessage   = "the state root holds state but no registry.json this build can read"
	storeRecoveryRemediation = otherBuildGuidance + "; a store this build created is restored whole from a matching backup"
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
	if envelope.Version != contexts.RegistryVersion {
		// Any other version is another build's state, never damage this build
		// may convert or move aside.
		return anotherBuilds(contexts.StateErrorWithRemediation(missingRegistryMessage, storeRecoveryRemediation))
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
