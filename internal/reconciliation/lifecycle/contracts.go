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

// ContextIdentity names the context a lifecycle read resolved. Mode is the
// context record's mode, which a lifecycle read admits only while it is ready;
// status reports it and no capability reads it.
type ContextIdentity struct {
	Name     string
	Revision string
	Mode     string
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

// SecretBinder freezes confidential material before registration. A binding an
// operation names is released only after a completed destroy no longer needs
// it; one a registration that provably did not happen created is released at
// once, and one no operation names is released after the context's next
// registration or by its next destroy. Bindings lists identities only, so the
// engine can tell those bindings apart without reading any material.
type SecretBinder interface {
	Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
	Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error)
	Release(context.Context, custody.BindingRequest) (bool, error)
	Bindings(context.Context, custody.BindingsRequest) ([]string, error)
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
	// Removal reads one frozen block as the removal of what it created. It is
	// part of the port rather than an optional extra, because a block left out
	// would be removed under the authorization its apply acknowledged instead
	// of the authorization removing it needs.
	Removal(context.Context, reconciliation.Block) (Removal, error)
	Apply(context.Context, Execution) (Result, error)
	// Observe reads a frozen block of an apply, read-only, and reports the
	// state of the apply's effect.
	Observe(context.Context, Execution) (Observation, error)
	// Quiescent reports whether what this block owns is still in use. It is
	// part of the port rather than an optional extra, so a capability cannot be
	// silently left out of the gate that protects a live environment.
	Quiescent(context.Context, Probe) (Quiescence, error)
	Destroy(context.Context, Execution) (Result, error)
	// ObserveRemoval reads a frozen block of a destroy, read-only, and reports
	// the state of the removal: completed when the removal's own postcondition
	// holds, no-effect when the target still shows everything the removal
	// takes back, partial when part of that remains and is this context's own,
	// and unknown otherwise. It is separate from Observe because a target the
	// apply realized is the removal's absence of effect, and what a removal
	// keeps by design proves nothing against it. An observation that cannot
	// tell the removal's postcondition from a target it could not read is
	// never completed: it reports no-effect, so the removal repeats and proves
	// its own postcondition.
	ObserveRemoval(context.Context, Execution) (Observation, error)
}

// Removal is the half of a frozen block that removing it decides: the words it
// is planned and reported in, the impacts it lists and the authorization it
// consumes. The block's identity, request and digests are deliberately absent,
// so planning a removal from a frozen plan can never change what is removed.
type Removal struct {
	Description string
	Impacts     []string
	Consumes    []string
	Groups      []reconciliation.Group
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
	// Claim creates a fresh apply's operation directory, empty, before it binds
	// anything; Register then fills that directory under the same identity.
	Claim(context.Context, string) error
	// Claimed names every operation directory, which nothing removes, so a
	// directory added since a count was taken proves a newer claim.
	Claimed(context.Context) ([]string, error)
	// Started reports whether an operation directory lists a block record.
	Started(context.Context, string) (bool, error)
	Register(context.Context, operationstore.Operation, reconciliation.Plan) error
	ReadOperation(context.Context, string) (operationstore.Operation, error)
	ReadPlan(context.Context, string) (reconciliation.Plan, error)
	UpdateOperation(context.Context, operationstore.Operation) error
	BlockStates(context.Context, string, reconciliation.Plan) (map[string]reconciliation.BlockState, error)
	Block(context.Context, string, string) (operationstore.BlockRecord, error)
	LostBlockRecords(context.Context, string, reconciliation.Plan) ([]string, error)
	Attempt(context.Context, string, string, int) (operationstore.Attempt, error)
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
	// Proved is what each block this apply attempt's block depends on durably
	// proved in this operation, read from that block's last observed attempt
	// and handed over unread. A capability decodes it only through the owner
	// of that evidence, which composition wires. A destroy, a resolution and a
	// probe receive none.
	Proved []BlockEvidence
	// LocateTool answers where the controller stage installed one executable,
	// so a block runs the exact file that stage published for the release its
	// own graph selected.
	LocateTool func(context.Context, controller.InstalledTool) (string, error)
	// Stage is the controller stage's own publication boundary and is present
	// only on the block that extends this host's prerequisites. Every other
	// capability receives nil and cannot reach it.
	Stage    *ControllerStage
	Log      func(context.Context, operationstore.LogRecord) error
	Progress func(context.Context, string, string)
	// Output retains what the adapter prints on its own standard output and
	// error, for an operator to read when no structured event explains what a
	// run did. An adapter hands it the process's streams and writes nothing
	// itself; whoever opened it owns closing it.
	Output prerequisites.RunOutput
}

// ControllerStage is what extending the host's prerequisites needs and nothing
// else does: the evidence setup froze, the shared area a closure is published
// into with its sealing, the durable identities recorded before any
// acquisition, the before-state published ahead of any host change, and the
// one handback of the native package read lock.
type ControllerStage struct {
	Setup              prerequisites.StorageView
	ClientArea         func(context.Context, string) (prerequisites.BundleArea, error)
	SealClientArea     func(context.Context, string) error
	RetainDependencies func(context.Context, *prerequisites.Definition, []prerequisites.DependencySource) error
	Prepare            func(context.Context, prerequisites.NativePreparation) error
	ReleaseFoundation  func() error
}

type Result struct {
	Outcome  reconciliation.Outcome
	Evidence json.RawMessage
}

type Observation struct {
	Effect   reconciliation.EffectState
	Evidence json.RawMessage
}
