package contextfs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type legacyRegistry struct {
	Version    int                 `json:"version"`
	Identities []contexts.Identity `json:"identities"`
	Contexts   []contexts.Record   `json:"contexts"`
}

type registryV3 struct {
	Version      int               `json:"version"`
	IDNamespace  string            `json:"idNamespace"`
	NextIdentity uint64            `json:"nextIdentity"`
	Contexts     []contexts.Record `json:"contexts"`
}

func registryRecord(registry contexts.Registry) any {
	if registry.Version == 3 {
		return registryV3{Version: registry.Version, IDNamespace: registry.IDNamespace, NextIdentity: registry.NextIdentity, Contexts: registry.Contexts}
	}
	return legacyRegistry{Version: registry.Version, Identities: registry.Identities, Contexts: registry.Contexts}
}

func decodeRegistry(data []byte, maximum int, target *contexts.Registry) error {
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return state("context registry is malformed")
	}
	switch envelope.Version {
	case 2:
		var record legacyRegistry
		if err := decodeRecord(data, maximum, &record); err != nil {
			return err
		}
		*target = contexts.Registry{Version: record.Version, Identities: record.Identities, Contexts: record.Contexts}
	case 3:
		var record registryV3
		if err := decodeRecord(data, maximum, &record); err != nil {
			return err
		}
		*target = contexts.Registry{Version: record.Version, IDNamespace: record.IDNamespace, NextIdentity: record.NextIdentity, Identities: []contexts.Identity{}, Contexts: record.Contexts}
	default:
		return state("context registry version is unsupported")
	}
	return nil
}

func identityNamespace(id string) string { return strings.TrimPrefix(id, "ctx-")[:16] }

func validNamespace(namespace string) bool {
	return len(namespace) == 16 && identifier("ctx-"+namespace+"0000000000000000", "ctx-")
}

// Upgrade only immediately before an already-authorized registry publication.
// Excluding every legacy prefix prevents this counter from ever issuing an ID
// that was allocated by the previous random-identity format.
func (s *Store) upgradeRegistry(registry contexts.Registry) (contexts.Registry, error) {
	if err := validateRegistry(registry); err != nil {
		return contexts.Registry{}, err
	}
	if registry.Version == 3 {
		return registry, nil
	}
	used := make(map[string]bool, len(registry.Identities))
	for _, item := range registry.Identities {
		used[identityNamespace(item.ID)] = true
	}
	for range 16 {
		candidate, err := s.candidate("ctx-")
		if err != nil {
			return contexts.Registry{}, err
		}
		namespace := identityNamespace(candidate)
		if used[namespace] {
			continue
		}
		registry.Version = 3
		registry.IDNamespace = namespace
		registry.NextIdentity = 1
		registry.Identities = []contexts.Identity{}
		return registry, nil
	}
	return contexts.Registry{}, state("context allocation namespace exhausted its collision limit")
}

func allocateContextIdentity(registry *contexts.Registry) (string, error) {
	if registry.Version != 3 || !validNamespace(registry.IDNamespace) || registry.NextIdentity == 0 || registry.NextIdentity == ^uint64(0) {
		return "", state("context identity allocation is invalid or exhausted")
	}
	id := fmt.Sprintf("ctx-%s%016x", registry.IDNamespace, registry.NextIdentity)
	registry.NextIdentity++
	return id, nil
}
