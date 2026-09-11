package prerequisites

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type Compiler interface {
	Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type Definition struct {
	Platform           Platform                      `json:"platform"`
	Versions           controller.DependencyVersions `json:"versions"`
	ToolRequests       []controller.ToolRequest      `json:"toolRequests"`
	Native             *NativeResolvedPlan           `json:"native"`
	Bootstrap          *BootstrapDefinition          `json:"bootstrap"`
	CatalogDigest      string                        `json:"catalogDigest"`
	ResolutionDigest   string                        `json:"resolutionDigest"`
	BaseCatalogDigest  string                        `json:"baseCatalogDigest"`
	NativeRequirements NativeRequirements            `json:"nativeRequirements"`
	Tools              []ToolDefinition              `json:"tools"`
	PythonVersion      string                        `json:"pythonVersion"`
	AnsibleVersion     string                        `json:"ansibleVersion"`
	Sources            []DependencySource            `json:"sources"`
	Execution          ExecutionRequirement          `json:"execution"`
	Runtime            RuntimeRequirement            `json:"runtime"`
}

// ExecutionRequirement fixes the provided host libraries used before private
// Python can enforce its own import and native transaction boundaries.
type ExecutionRequirement struct {
	PythonExecutable string          `json:"pythonExecutable"`
	Loader           string          `json:"loader"`
	LockPath         string          `json:"lockPath"`
	Files            []InstalledFile `json:"files"`
	Links            []InstalledLink `json:"links"`
	Preload          []string        `json:"preload"`
}

type PythonLaunch struct {
	Loader      string
	Arguments   []string
	Directory   string
	Environment []string
}

// WithPython verifies and holds the execution foundation under the native
// package read lock. A native installer may release that lock only after its
// loaded helper acknowledges readiness to acquire and revalidate under the
// native write lock. All other callers retain it through process completion.
type PythonExecutionGuard interface {
	WithPython(context.Context, BundleArea, ExecutionRequirement, func(PythonLaunch, func() error) error) error
}

type DependencyCatalog interface {
	Select(Platform, NativeRequirements) (Definition, error)
	ValidateEgress(SetupEgress) error
}

// NativeRequirements selects the fixed package closure required by the
// admitted bastion capabilities and referenced infrastructure providers.
type NativeRequirements struct {
	ContainerRuntime bool `json:"containerRuntime"`
	LibvirtClient    bool `json:"libvirtClient"`
}

type SourceAcquirer interface {
	Acquire(context.Context, DependencySource, SetupEgress) ([]byte, error)
}

// NativePackage records an exact publisher-signed RPM identity. The closed
// dependency set also includes OS prerequisites and required publisher hooks;
// it grants no authority to replace installed packages or run unrelated hooks.
type NativePackage struct {
	Name         string           `json:"name"`
	Epoch        int              `json:"epoch"`
	Version      string           `json:"version"`
	Release      string           `json:"release"`
	Architecture string           `json:"architecture"`
	Signer       string           `json:"signer"`
	Source       DependencySource `json:"source"`
}

type BundleInspection struct {
	Ready      bool
	ToolsReady bool
	Sealed     bool
	// Recoverable means every existing file has exact approved content and no
	// unapproved entry exists. Only an attributable incomplete action may resume.
	Recoverable bool
}

type BundleManager interface {
	Inspect(context.Context, BundleArea, Definition, bool) (BundleInspection, error)
	Prepare(context.Context, BundleArea, Definition, SetupEgress) error
}

type RuntimeInstaller interface {
	Prepare(context.Context, BundleArea, Platform, Definition, SetupEgress, func(context.Context, NativePreparation) error) (ActionResult, error)
	Recover(context.Context, BundleArea, Platform, Definition, SetupEgress, NativePreparation) (ActionResult, error)
}

type NativePreparation struct {
	InventorySHA256      string   `json:"inventorySHA256"`
	AfterInventorySHA256 string   `json:"afterInventorySHA256,omitempty"`
	PlanDigest           string   `json:"planDigest,omitempty"`
	TransitionsSHA256    string   `json:"transitionsSHA256,omitempty"`
	AddedSources         []string `json:"addedSources"`
}

type ActionResult struct {
	Outcome  string
	Evidence json.RawMessage
}
