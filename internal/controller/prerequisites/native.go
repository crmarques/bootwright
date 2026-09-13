package prerequisites

import (
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
