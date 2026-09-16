package lifecycle

import (
	"context"
	"encoding/json"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type ContextIdentity struct {
	Name     string
	Revision string
}

// View is a coherent read of everything an operation reasons about, taken
// under the store's shared lock.
type View interface {
	Identity() ContextIdentity
	Inputs() desiredstate.Sources
	Controller() prerequisites.StorageView
	Evidence() []byte
	Operations() operationstore.Area
}

// RunView is what a bounded operation outside the lifecycle reads. It adds
// exactly two things an inspection has no use for: the controller's approved
// execution bundle, which the adapter call runs inside, and a writable area
// holding what that adapter printed. Neither is desired state, ownership or a
// continuation cursor, so a bounded run still registers no operation.
type RunView interface {
	View
	Runs() operationstore.Area
}

// Transaction adds the publications an operation performs. Each holds the root
// lock and the context lease for the whole callback, so reservations,
// controller evidence and operation records stay coherent while effects run.
type Transaction interface {
	View
	PublishEvidence(context.Context, []byte) error
	// Bind records this context's controller relationship on the prepared host.
	// A first apply establishes it; every later one revalidates exactly it.
	Bind(context.Context, string, controller.InstalledHostIdentity) error
	Reserve(context.Context, []prerequisites.HostReservation) error
	ReleaseReservations(context.Context) error
	// ClientArea opens the shared host area one exact client closure a context
	// selects is published into, creating and attributing it under a durable
	// reservation. A sealed area reopens read-only.
	ClientArea(context.Context, string) (prerequisites.BundleArea, error)
	// SealClientArea makes that closure immutable after its complete tree is
	// verified and durable.
	SealClientArea(context.Context, string) error
	// RetainDependencies records the acquisition identities and native
	// resolution a controller stage froze, before it installs them.
	RetainDependencies(context.Context, *prerequisites.Definition, []prerequisites.DependencySource) error
}

type Workspace interface {
	ReadLifecycle(context.Context, string, func(View) error) error
	RunLifecycle(context.Context, string, func(RunView) error) error
	MutateLifecycle(context.Context, string, func(Transaction) error) error
}

type Inputs interface {
	ReadInputs(context.Context, string) (desiredstate.Sources, error)
}

type Compiler interface {
	Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}

// SecretBinder freezes confidential material before registration and releases
// it only after a completed destroy no longer needs it.
type SecretBinder interface {
	Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
	Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error)
	Release(context.Context, custody.BindingRequest) (bool, error)
}

type HostIdentity interface {
	Identity(context.Context) (controller.InstalledHostIdentity, error)
}

// AutomationIdentity is the content identity of the embedded automation and
// dependency catalog this executable would run. A continuation refuses when it
// no longer matches what the operation froze.
type AutomationIdentity interface {
	CatalogDigest() string
}

type ExecutionGuard interface {
	WithPython(context.Context, prerequisites.BundleArea, prerequisites.ExecutionRequirement, func(prerequisites.PythonLaunch, func() error) error) error
}

// Capability is one domain's lifecycle implementation. It plans its own blocks
// and owns what their completion, readiness, quiescence and absence mean; it
// never schedules another domain's work or writes lifecycle state.
type Capability interface {
	Plan(context.Context, PlanInput) (CapabilityPlan, error)
	Apply(context.Context, Execution) (Result, error)
	Observe(context.Context, Execution) (Observation, error)
	// Quiescent reports whether what this block owns is still in use. It is
	// part of the port rather than an optional extra, so a capability cannot be
	// silently left out of the gate that protects a live environment.
	Quiescent(context.Context, Probe) (Quiescence, error)
	Destroy(context.Context, Execution) (Result, error)
}

// Probe is one read-only quiescence observation against a frozen block. It
// carries no operation identity, log or before-state publication, because it
// runs before any operation is registered and may change nothing.
type Probe struct {
	Block    reconciliation.Block
	Launch   prerequisites.PythonLaunch
	Bundle   prerequisites.BundleLocation
	Area     prerequisites.BundleArea
	Material map[string]secrets.Material
}

// Execution presents the probe as one bounded adapter call, so a capability
// reaches its own observation the same way it always does. It carries no log,
// progress or before-state hook, because a probe runs before any operation
// exists to record against and may change nothing.
func (p Probe) Execution() Execution {
	return Execution{Block: p.Block, Launch: p.Launch, Bundle: p.Bundle, Area: p.Area, Material: p.Material}
}

// QuiescenceState is what a probe proved about one block's targets.
const (
	// Quiescent means nothing this block owns is in use, so removing it
	// interrupts nothing.
	Quiescent = "quiescent"
	// Live means something it owns is in use.
	Live = "live"
	// Unproved means the probe could not tell. It is treated as live, because
	// an environment that cannot prove it is idle is never assumed to be.
	Unproved = "unproved"
)

// Quiescence is one block's answer. Reason says what is in use in the
// operator's terms, and Stop is the exact command that ends it, so a refusal
// names the way forward rather than only the obstacle.
type Quiescence struct {
	State  string
	Reason string
	Stop   string
}

// Settled reports the one state that admits removal.
func (q Quiescence) Settled() bool { return q.State == Quiescent }

// CapabilityBinding is one implementation this executable offers for one API
// kind. Several capabilities may realize the same kind through different
// implementations, so a binding, not a kind alone, identifies one of them.
type CapabilityBinding struct {
	Kind           string
	Implementation string
}

// CapabilityResolver is the immutable set of capabilities this executable
// offers, in canonical API-kind order. Bindings names what it can realize so
// the engine neither hard-codes a kind nor discovers one at runtime.
type CapabilityResolver interface {
	Bindings() []CapabilityBinding
	Resolve(kind, implementation string) (Capability, bool)
}

// UnsupportedReporter lets a capability name what it cannot realize without
// planning it. A capability that realizes everything it is given omits it.
type UnsupportedReporter interface {
	Unsupported(*compilation.State) []string
}

// OperationStore is the durable record set one operation publishes through.
// Composition binds it to the Workspace-held area for each invocation.
type OperationStore interface {
	Index(context.Context) (operationstore.Index, error)
	Register(context.Context, operationstore.Operation, reconciliation.Plan) error
	ReadOperation(context.Context, string) (operationstore.Operation, error)
	ReadPlan(context.Context, string) (reconciliation.Plan, error)
	UpdateOperation(context.Context, operationstore.Operation) error
	BlockStates(context.Context, string, reconciliation.Plan) (map[string]reconciliation.BlockState, error)
	StartAttempt(context.Context, string, string) (int, error)
	RecordPreparation(context.Context, string, string, int, json.RawMessage) error
	CompleteAttempt(context.Context, string, string, int, reconciliation.Outcome, reconciliation.EffectState, reconciliation.BlockState, json.RawMessage) error
	LastAttempt(context.Context, string, string) (int, error)
	StartResolution(context.Context, string, string, int) (int, error)
	CompleteResolution(context.Context, string, string, int, int, reconciliation.EffectState, reconciliation.BlockState, json.RawMessage) error
	OpenLog(context.Context, string) (*operationstore.Log, error)
	OpenAdapterOutput(context.Context, string) *operationstore.AdapterOutput
	LogPaths(context.Context, string, reconciliation.Plan) ([]string, error)
	LogDirectory(string) string
}

// OperationStoreFactory opens the record set over one held area. It is a
// function capability because an operation opens exactly one per invocation.
type OperationStoreFactory func(operationstore.Area) OperationStore

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type PlanPresenter interface {
	PresentLifecyclePlan(context.Context, PlanResult) error
}

type ProgressReporter interface {
	ReportProgress(context.Context, ProgressEvent)
	// ReportLogLocation names where this operation is writing, before its first
	// effect runs, so an operator can follow the work while it happens rather
	// than read it afterwards.
	ReportLogLocation(context.Context, string)
}

type Clock interface {
	Now() time.Time
}

// PlanInput is the pure input a capability plans from. It carries no host
// evidence, credential or endpoint, so planning stays read-only.
type PlanInput struct {
	Verb       reconciliation.Verb
	Context    ContextIdentity
	State      *compilation.State
	Controller string
}

// CapabilityPlan is what one capability contributes to an operation: its
// blocks, the exclusive host resources they claim and the Secret declarations
// their execution needs bound.
type CapabilityPlan struct {
	Definitions  []reconciliation.BlockDefinition
	Reservations []prerequisites.HostReservation
	Secrets      []string
}

// Execution is one authorized attempt against one frozen block. Material is
// bounded memory owned by the caller and cleared after the attempt.
type Execution struct {
	Operation  string
	Attempt    int
	Resolution int
	Block      reconciliation.Block
	Launch     prerequisites.PythonLaunch
	Bundle     prerequisites.BundleLocation
	Area       prerequisites.BundleArea
	Material   map[string]secrets.Material
	// Setup is the retained controller evidence this host was prepared with. A
	// block that extends those prerequisites reads the resolution and sources
	// setup froze rather than resolving the host foundation again.
	Setup prerequisites.StorageView
	// ClientArea, SealClientArea and RetainDependencies are the controller
	// stage's publication boundary: the shared area its closure is published
	// into, its sealing, and the durable identities it records before any
	// acquisition. Every other block leaves them untouched.
	ClientArea         func(context.Context, string) (prerequisites.BundleArea, error)
	SealClientArea     func(context.Context, string) error
	RetainDependencies func(context.Context, *prerequisites.Definition, []prerequisites.DependencySource) error
	// Prepare publishes the before-state this attempt observed, before it is
	// permitted to change the host. A block with no host-wide effect never
	// calls it.
	Prepare func(context.Context, prerequisites.NativePreparation) error
	// ReleaseFoundation hands the native package read lock this execution holds
	// back, so a block whose own transaction needs the write lock can take it.
	// It is valid once, and only before that transaction starts.
	ReleaseFoundation func() error
	Log               func(context.Context, operationstore.LogRecord) error
	Progress          func(context.Context, string, string)
	// Output retains what the adapter prints on its own standard output and
	// error, for an operator to read when no structured event explains what a
	// run did. An adapter hands it the process's streams and writes nothing
	// itself; whoever opened it owns closing it.
	Output prerequisites.RunOutput
}

type Result struct {
	Outcome  reconciliation.Outcome
	Evidence json.RawMessage
}

type Observation struct {
	Effect   reconciliation.EffectState
	Evidence json.RawMessage
}
