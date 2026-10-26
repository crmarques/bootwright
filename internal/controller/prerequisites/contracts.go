package prerequisites

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

// FIPSInspector reports kernel mode; selected FIPS clusters require a positive observation.
type FIPSInspector interface {
	FIPSMode(context.Context) (bool, error)
}

type LoaderPreloadInspector interface {
	LoaderPreload(context.Context) (bool, error)
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
	Fetch(ctx context.Context, abi string, egress SetupEgress, mirror WheelMirror) error
	Export(ctx context.Context, destination string, egress SetupEgress, mirror WheelMirror) (WheelExport, error)
	Inspect(ctx context.Context, interpreter PythonInterpreter) (Runtime, bool, error)
	Build(ctx context.Context, publisher RuntimePublisher, interpreter PythonInterpreter) (Runtime, error)
	RuntimeWheels(ctx context.Context, digest string) ([]string, error)
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
type ControllerRebinder interface {
	RebindController(ctx context.Context, recorded, next controller.InstalledHostIdentity) (RebindResult, error)
}

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
	PruneWheels(ctx context.Context, keep []string) (int, error)
}

type SetupWaiter interface {
	AwaitSetup(ctx context.Context, callback func(SetupTransaction) error) error
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

type ListenerChecks func(context.Context, api.Catalog, string) ([]Check, error)
