package contextfs

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
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
		return state("context registry version is unsupported")
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
