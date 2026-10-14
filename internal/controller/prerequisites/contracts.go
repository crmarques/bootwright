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
}

type ReleaseInspector interface {
	Release(context.Context) (HostRelease, error)
}

type HostRelease struct {
	ID           string
	VersionID    string
	Like         []string
	Architecture string
}

// FIPSInspector is how a HostInspector reports whether the host's kernel runs
// in FIPS mode. A host without it reports no FIPS mode check; the check never
// changes readiness either way.
type FIPSInspector interface {
	FIPSMode(context.Context) (bool, error)
}

type LoaderPreloadInspector interface {
	LoaderPreload(context.Context) (bool, error)
}

// FoundationInspector verifies, under the native package read lock, the
// provided execution foundation this executable was compiled against for a
// platform: the loader, glibc and libgcc files and links the private
// interpreter runs on, byte for byte, exactly as every private Python launch
// verifies them.
type FoundationInspector interface {
	Inspect(context.Context, Platform) (FoundationInspection, error)
}

// FoundationInspection names the package builds that provide the foundation
// and, for one that differs, the first path that differs and the refusal that
// names that path, the package build that provides it, what was found and the
// remedy. Its error is reserved for an inspection that could not run.
//
// Qualified is set when the compiled foundation differs but the host holds
// vendor-signed builds of the same upstream versions within the qualified
// minor whose files match the RPM database byte for byte: it is the
// requirement those builds prove, and Refusal is nil.
type FoundationInspection struct {
	Required  string
	Drift     string
	Refusal   error
	Qualified *QualifiedFoundation
}

// FoundationBuildReader reads, from a snapshot of the host's package database,
// each installed instance of the named foundation packages: its identity,
// whether a signature of the platform's vendor key covers it, its file digest
// algorithm and the digest or link target rpm records for each file. Only a
// foundation inspection reads it; no launch ever does.
type FoundationBuildReader interface {
	FoundationBuilds(context.Context, Platform, []string) ([]InstalledBuild, error)
}

// InstalledBuild is one installed package instance as the RPM database
// records it. DigestAlgorithm is rpm's FILEDIGESTALGO, 8 for SHA-256.
type InstalledBuild struct {
	Name            string
	Epoch           int
	Version         string
	Release         string
	Architecture    string
	Signed          bool
	DigestAlgorithm int
	Files           map[string]InstalledBuildFile
}

// InstalledBuildFile is what rpm records for one file of a build: its digest,
// empty for a directory or a link, and its link target, empty for anything
// but a link.
type InstalledBuildFile struct {
	SHA256 string
	LinkTo string
}

// DependencyCatalog is pure: Admit refuses a platform whose provided execution
// foundation this executable was not compiled against, before any dependency
// is resolved or acquired, and ValidateEgress the acquisition policy.
type DependencyCatalog interface {
	Admit(Platform) error
	ValidateEgress(SetupEgress) error
}

type BundleManager interface {
	Inspect(context.Context, BundleArea, Definition, bool) (BundleInspection, error)
	// Validate refuses, without reading any area, a definition this executable
	// could never inspect or prepare, exactly as Inspect and Prepare refuse it:
	// a retained resolution whose automation or provided execution foundation
	// moved returns ErrBootstrapIncompatible.
	Validate(Definition) error
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
	// A source that area cannot serve as its approved bytes returns
	// ErrRetainedSourceUnavailable.
	Rebase(context.Context, BundleArea, BootstrapDefinition) (BootstrapDefinition, error)
}

type HostPackages interface {
	Qualify(ctx context.Context) error
	Missing(ctx context.Context, requirements []string) ([]string, error)
	Installed(ctx context.Context, requirements []string) ([]HostPackageEvidence, error)
	Present(ctx context.Context, requirements []string) ([]HostPackageEvidence, error)
	Preview(ctx context.Context, missing []string) (HostPackagePreview, error)
	Install(ctx context.Context, missing []string, output RunOutput) ([]HostPackageEvidence, error)
}

// WithPython holds the runtime's launch lock shared for the whole block and
// verifies the runtime before use runs. A runtime that is not published
// reports ErrRuntimeUnavailable.
type PythonExecutionGuard interface {
	WithPython(ctx context.Context, runtime Runtime, use func(PythonLaunch) error) error
}

type PythonRuntime interface {
	PythonExecutionGuard
	Select(ctx context.Context, preferred string) (PythonInterpreter, error)
	Wheels(ctx context.Context, abi string) ([]RuntimeWheel, error)
	Fetch(ctx context.Context, abi string, egress SetupEgress) error
	Inspect(ctx context.Context, interpreter PythonInterpreter) (Runtime, bool, error)
	Build(ctx context.Context, publisher RuntimePublisher, interpreter PythonInterpreter) (Runtime, error)
}

// BootstrapResolver may stage and execute a wheel-only resolver in a disposable
// unprivileged workspace. It has no installed-host or shared-store authority.
// The warnings it returns, with a resolution or with the error that stopped
// one, are reported with setup's result.
type BootstrapResolver interface {
	Resolve(context.Context, Platform, controller.DependencyVersions, SetupEgress) (BootstrapDefinition, []diagnostics.Diagnostic, error)
}

// Staging hands dependency resolution its private scratch. Each Stage is a new
// directory beneath one Bootwright-owned parent outside the verified store,
// held by this invocation until Release; a stage whose holder is gone is swept
// by the next Stage. Noexec reports that the parent's filesystem refuses to
// execute what is staged there.
type Staging interface {
	Stage(ctx context.Context, kind string) (Stage, error)
}

// Stage is one invocation's scratch directory. Release removes it and is safe
// to call more than once.
type Stage struct {
	Path    string
	Noexec  bool
	Release func()
}

type AutomationStager interface {
	Stage(ctx context.Context) (StagedAutomation, error)
}

type StagedAutomation struct {
	Path    string
	Digest  string
	Lock    interface{ Fd() uintptr }
	Release func()
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

// Storage reads the shared controller record: host identity, bindings, tool
// sources and areas, and reservations. ExplicitName is empty for baseline
// scope; no method consults current-context selection. The callback holds the
// root lock and must consume input before returning.
type Storage interface {
	ReadController(context.Context, string, func(StorageView) error) error
}

// StateRootInspector is how a Storage that keeps its state beneath one root
// lets a setup dry run, which reads no record and stays unprivileged, report
// whether this build can use that root. A Storage without it leaves the dry
// run's report as it was.
type StateRootInspector interface {
	InspectStateRoot(context.Context) (StateRootInspection, error)
}

// StateRootInspection is what that inspection observed, as one check's
// requirement, observation and status, and, for a root this build cannot use,
// the store's own refusal of it, which names its remedy. Its error is reserved
// for an inspection that could not run at all.
type StateRootInspection struct {
	Required string
	Observed string
	Status   string
	Refusal  error
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

type SetupStore interface {
	ReadSetup(ctx context.Context) (SetupView, error)
	MutateSetup(ctx context.Context, callback func(SetupTransaction) error) error
	HoldRuntime(ctx context.Context, digest string) (RuntimeHold, error)
}

type SetupTransaction interface {
	RuntimePublisher
	View() SetupView
	Record(ctx context.Context, value SetupState) error
	DiscardForeign(ctx context.Context) error
	OpenRun(ctx context.Context) (string, SetupRun, error)
	Runtimes(ctx context.Context) ([]string, error)
	WheelPath(ctx context.Context, sha256, filename string) (string, error)
}

// RuntimePublisher is the setup transaction's runtime half: PublishRuntime
// reports false when another publisher won and removes the stage, and
// RetireRuntime false when a launch holds the runtime.
type RuntimePublisher interface {
	StageRuntime(ctx context.Context) (RuntimeStage, error)
	PublishRuntime(ctx context.Context, stage RuntimeStage, digest string) (bool, error)
	DiscardRuntime(ctx context.Context, stage RuntimeStage) error
	RetireRuntime(ctx context.Context, digest string) (bool, error)
}

type RuntimeStage interface {
	Path() string
}

type RuntimeHold interface {
	Path() string
	Release()
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
