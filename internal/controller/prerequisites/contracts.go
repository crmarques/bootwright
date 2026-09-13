package prerequisites

import (
	"context"

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

type HostInspector interface {
	Platform(context.Context) (Platform, error)
	Identity(context.Context) (controller.InstalledHostIdentity, error)
	Runtime(context.Context, RuntimeRequirement) (RuntimeInspection, error)
}

type DependencyCatalog interface {
	Select(Platform, NativeRequirements) (Definition, error)
	ValidateEgress(SetupEgress) error
}

type BundleManager interface {
	Inspect(context.Context, BundleArea, Definition, bool) (BundleInspection, error)
	Prepare(context.Context, BundleArea, Definition, SetupEgress, func(ProgressEvent)) error
}

// RuntimeInstaller reports the phase of its native transaction and each target
// tool through the progress callback; the caller supplies the action identity.
type RuntimeInstaller interface {
	Prepare(context.Context, BundleArea, Platform, Definition, SetupEgress, func(context.Context, NativePreparation) error, func(ProgressEvent)) (ActionResult, error)
	Recover(context.Context, BundleArea, Platform, Definition, SetupEgress, NativePreparation, func(ProgressEvent)) (ActionResult, error)
}

// WithPython verifies and holds the execution foundation under the native
// package read lock. A native installer may release that lock only after its
// loaded helper acknowledges readiness to acquire and revalidate under the
// native write lock. All other callers retain it through process completion.
type PythonExecutionGuard interface {
	WithPython(context.Context, BundleArea, ExecutionRequirement, func(PythonLaunch, func() error) error) error
}

// BootstrapResolver may stage and execute a wheel-only resolver in a disposable
// unprivileged workspace. It has no installed-host or shared-store authority.
type BootstrapResolver interface {
	Resolve(context.Context, Platform, controller.DependencyVersions, SetupEgress) (BootstrapDefinition, error)
}

// NativeResolver may use disposable unprivileged staging for repository
// metadata and maintained solver caches. It never applies a host transaction.
type NativeResolver interface {
	Resolve(context.Context, Platform, NativeRequirements, controller.DependencyVersions, SetupEgress) (NativeResolvedPlan, error)
}

// NativeInspector reports whether the selected native root packages are
// installed by name, without consulting repository metadata, verifying
// installed files or modifying installed state.
type NativeInspector interface {
	Check(context.Context, NativeResolvedPlan) (NativePresence, error)
}

type TargetToolCatalog interface {
	// Select is pure: recover exact retained identities without metadata reads.
	// Complete is false when setup must resolve a previously unseen requirement.
	Select([]controller.ToolRequest, []DependencySource) ([]ToolDefinition, bool, error)
	// Resolve reads bounded publisher metadata through the explicit route. It
	// does not acquire payloads, create files, run tools, or mutate host state.
	Resolve(context.Context, []controller.ToolRequest, SetupEgress) ([]ToolDefinition, error)
}

// Storage owns host-wide setup coordination and durable evidence. ExplicitName
// is empty for baseline scope; no method consults current-context selection.
// Callbacks hold the root lock and must consume input before returning.
type Storage interface {
	ReadController(context.Context, string, func(StorageView) error) error
	// Create authorizes root/empty-registry bootstrap and requires prior ordinary
	// confirmation. Mutations additionally hold the selected context's lease.
	MutateController(context.Context, SetupContext, bool, func(StorageTransaction) error) error
}

// StorageTransaction is invocation-scoped. Publish revalidates exact stored
// evidence before atomic replacement; Unknown disables all further publication.
type StorageTransaction interface {
	Snapshot() StorageView
	Publish(context.Context, HostState) (Publication, error)
	Bundle(context.Context, string) (BundleArea, error)
}

// BundleArea confines the qualified bundle adapter to one catalog namespace.
// It is valid only while its storage callback holds coordination. Caller paths
// come from the embedded dependency closure, never authored desired state.
type BundleArea interface {
	Read(context.Context, string, int) ([]byte, error)
	Write(context.Context, string, []byte, bool) error
	EnsureDirectory(context.Context, string) error
	Entries(context.Context) ([]BundleEntry, error)
	Verify(context.Context) error
	Location(context.Context) (BundleLocation, error)
}

type PlanPresenter interface {
	PresentControllerPlan(context.Context, Report) error
}

// ProgressReporter receives events while setup runs. Reporting is best effort:
// a reporting failure never changes an effect or its recorded outcome.
type ProgressReporter interface {
	ReportProgress(context.Context, ProgressEvent)
}
