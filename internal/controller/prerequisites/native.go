package prerequisites

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/controller"
)

// NativeIdentity is the exact installed RPM identity, independent of a source URL.
type NativeIdentity struct {
	Name         string `json:"name"`
	Epoch        int    `json:"epoch"`
	Version      string `json:"version"`
	Release      string `json:"release"`
	Architecture string `json:"architecture"`
}

// NativeRoot binds one required dependency to its selected repository release.
type NativeRoot struct {
	Key       string         `json:"key"`
	Requested string         `json:"requested"`
	Package   NativeIdentity `json:"package"`
}

// NativePresence reports whether every selected root package is installed by
// name. Installed identities are display evidence for the report, never a
// comparison against the frozen release.
type NativePresence struct {
	Ready     bool
	Installed []NativeRootPresence
}

type NativeRootPresence struct {
	Key     string
	Package NativeIdentity
}

// NativeAction permits only a named install or replacement in the solved set.
// A replacement must preserve the package name and architecture.
type NativeAction struct {
	Kind     string          `json:"kind"`
	Before   *NativeIdentity `json:"before,omitempty"`
	After    NativeIdentity  `json:"after"`
	SourceID string          `json:"sourceID"`
	Reason   string          `json:"reason"`
}

// NativeRepository records the exact metadata selected from an approved source.
// It contains no credentials or local credential file names.
type NativeRepository struct {
	ID             string `json:"id"`
	BaseURL        string `json:"baseURL"`
	MetadataSHA256 string `json:"metadataSHA256"`
}

// NativeResolvedPlan freezes a maintained solver's result before confirmation.
// Packages includes every inbound action and every selected root, including
// already installed roots, so signed source evidence remains available.
type NativeResolvedPlan struct {
	Format        string                        `json:"format"`
	Platform      Platform                      `json:"platform"`
	Solver        string                        `json:"solver"`
	SolverVersion string                        `json:"solverVersion"`
	Requests      controller.DependencyVersions `json:"requests"`
	Requirements  NativeRequirements            `json:"requirements"`
	Roots         []NativeRoot                  `json:"roots"`
	Repositories  []NativeRepository            `json:"repositories"`
	Packages      []NativePackage               `json:"packages"`
	Actions       []NativeAction                `json:"actions"`
	BeforeSHA256  string                        `json:"beforeSHA256"`
	AfterSHA256   string                        `json:"afterSHA256"`
	Digest        string                        `json:"digest"`
}

// ReadNativePreparation admits a container-runtime action's before-state only
// when its bytes are exactly what EncodeNativePreparation writes, its added
// sources belong to definition, and its native fields match definition's
// native plan or, without one, are absent. Setup reads each preparation back
// this way before retaining it and again before recovering from it.
func ReadNativePreparation(data []byte, definition Definition) (NativePreparation, error) {
	var preparation NativePreparation
	invalid := func() (NativePreparation, error) {
		return NativePreparation{}, failure("controller.unknown", "native preparation evidence is incomplete or incompatible", "restore the exact native setup evidence before continuing")
	}
	if len(data) == 0 || len(data) > 64<<10 || json.Unmarshal(data, &preparation) != nil {
		return invalid()
	}
	digest, err := hex.DecodeString(preparation.InventorySHA256)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != preparation.InventorySHA256 || preparation.AddedSources == nil || len(preparation.AddedSources) > 512 {
		return invalid()
	}
	previous := ""
	for _, id := range preparation.AddedSources {
		if id <= previous || !slices.ContainsFunc(definition.Sources, func(source DependencySource) bool { return source.ID == id }) {
			return invalid()
		}
		previous = id
	}
	if definition.Native != nil {
		transitions, err := NativeTransitionsDigest(definition.Native.Actions)
		if err != nil || preparation.PlanDigest != definition.Native.Digest || preparation.InventorySHA256 != definition.Native.BeforeSHA256 || preparation.AfterInventorySHA256 != definition.Native.AfterSHA256 || preparation.TransitionsSHA256 != transitions {
			return invalid()
		}
	} else if preparation.PlanDigest != "" || preparation.AfterInventorySHA256 != "" || preparation.TransitionsSHA256 != "" {
		return invalid()
	}
	encoded := EncodeNativePreparation(preparation)
	if !bytes.Equal(data, encoded) {
		return invalid()
	}
	return preparation, nil
}

// EncodeNativePreparation is the canonical object the container-runtime
// action retains as its preparation, published once while its intent is held.
func EncodeNativePreparation(preparation NativePreparation) json.RawMessage {
	value := map[string]any{"inventorySHA256": preparation.InventorySHA256, "addedSources": slices.Clone(preparation.AddedSources)}
	if preparation.PlanDigest != "" || preparation.AfterInventorySHA256 != "" || preparation.TransitionsSHA256 != "" {
		value["planDigest"] = preparation.PlanDigest
		value["afterInventorySHA256"] = preparation.AfterInventorySHA256
		value["transitionsSHA256"] = preparation.TransitionsSHA256
	}
	return nativeObject(value)
}

func nativeObject(values map[string]any) json.RawMessage {
	data, _ := json.Marshal(values)
	return data
}
