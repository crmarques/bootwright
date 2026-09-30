package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	// Prepare publishes the approved closure into the first area. The second is
	// a retained area this host already holds, or nil: every approved source is
	// read there before its publisher is contacted, so a resolution carried
	// onto new automation acquires nothing again. A source that area cannot
	// serve is acquired exactly as it would be without it. It returns the
	// verification of what it published, so a complete closure is read back and
	// qualified once rather than once by the adapter and again by its caller.
	Prepare(context.Context, BundleArea, BundleArea, Definition, SetupEgress, func(ProgressEvent)) (BundleInspection, error)
	// Rebase reprojects a retained resolution under the automation the running
	// executable embeds, reading its sources from the retained area that holds
	// them. It contacts no publisher and changes no release, byte count or
	// signer; only the projection identity that carries the automation moves.
	Rebase(context.Context, BundleArea, BootstrapDefinition) (BootstrapDefinition, error)
}

// RuntimeInstaller reports the phase of its native transaction and each target
// tool through the progress callback; the caller supplies the action identity.
// The RunOutput receives what the Ansible it starts prints, and a nil one
// discards it.
type RuntimeInstaller interface {
	Prepare(context.Context, BundleArea, Platform, Definition, SetupEgress, func(context.Context, NativePreparation) error, func(ProgressEvent), RunOutput) (ActionResult, error)
	Recover(context.Context, BundleArea, Platform, Definition, SetupEgress, NativePreparation, func(ProgressEvent), RunOutput) (ActionResult, error)
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
// The warnings it returns, with a resolution or with the error that stopped
// one, are reported with setup's result.
type BootstrapResolver interface {
	Resolve(context.Context, Platform, controller.DependencyVersions, SetupEgress) (BootstrapDefinition, []diagnostics.Diagnostic, error)
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
	// Present reports whether every resolved tool's retained source and
	// published files exist in the area. It reads no file content and contacts
	// no publisher, so a prepared host proves its tools without acquisition.
	Present(context.Context, BundleArea, []ToolDefinition) (bool, error)
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
	// RetireBundles removes superseded execution bundle areas and the retained
	// resolutions they carry. It records its intent before removing anything,
	// so an interruption leaves an area that is never read rather than one the
	// record still presents as usable.
	RetireBundles(context.Context, []string) error
	// RetireResolutions drops retained resolutions, named by resolution
	// digest, of a bundle that stays. It refuses the receipt's own and any
	// whose bundle no other retained resolution would still name, so every
	// execution bundle stays identifiable as one.
	RetireResolutions(context.Context, []string) error
	// OpenRun creates the next setup run while an action of the receipt holds
	// its durable intent, removing the oldest runs first so no more than the
	// retention bound are ever kept. It publishes nothing and changes no
	// record, so a caller treats its failure as retention it does not have.
	OpenRun(context.Context) (SetupRun, error)
}

// SetupRun keeps what one controller Ansible run of setup prints, so an
// operator can read what setup did when its receipt does not say. It is
// troubleshooting material only: nothing reads it back, a write past its bound
// is dropped, and neither a failed write nor a failed Close changes a setup
// outcome or its receipt.
type SetupRun interface {
	RunOutput
	// Location names the run's own directory on this host, for an operator to
	// open; it is built for reading, never opened through.
	Location() string
	Close() error
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

// BundleStream is how a bundle area that holds target tools lets inspection
// prove a source or member far larger than memory should hold. consume reads
// the file at path, no larger than maximum bytes, chunk by chunk; the stream
// fails unless consume read it whole and the file kept its identity.
type BundleStream interface {
	Stream(ctx context.Context, path string, maximum int64, consume func(BundleReader) error) error
}

// BundleReader is one bundle file's stream. Its shape is declared here rather
// than imported, as RunOutput's is.
type BundleReader interface {
	Read([]byte) (int, error)
}

// PlanPresenter opens the result with its scope before inspection streams the
// host checks, and presents the plan before confirmation.
type PlanPresenter interface {
	PresentControllerScope(context.Context, string, Report) error
	PresentControllerPlan(context.Context, Report) error
}

// RunOutput receives an adapter process's own standard output and error as it
// is produced, so an operator can read what a run did when its structured
// events do not say. The shape is declared here rather than imported, because
// the domains that cross this execution boundary depend on no I/O package.
type RunOutput interface {
	Write([]byte) (int, error)
}

// ProgressReporter receives events while setup runs. Reporting is best effort:
// a reporting failure never changes an effect or its recorded outcome.
type ProgressReporter interface {
	ReportProgress(context.Context, ProgressEvent)
	// ReportLogLocation names where a setup run keeps what its Ansible prints,
	// before that Ansible starts, so the run can be followed while it happens.
	ReportLogLocation(context.Context, string)
}
