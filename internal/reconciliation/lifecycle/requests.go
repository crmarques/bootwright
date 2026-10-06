package lifecycle

import "github.com/crmarques/bootwright/internal/machine"

type PlanRequest struct {
	ContextName string
	Stages      []string
}

type StatusRequest struct {
	ContextName string
}

type ApplyRequest struct {
	ContextName      string
	Stages           []string
	Authorizations   []string
	SkipConfirmation bool
	SSH              machine.SSHOptions
}

type DestroyRequest struct {
	ContextName      string
	Authorizations   []string
	SkipConfirmation bool
	SSH              machine.SSHOptions
}

// Receipt is the stable machine-readable tail of every resolved lifecycle
// result. `preview` and `refused` are presentation markers with no operation;
// every other state is the durable value.
type Receipt struct {
	Operation string
	Verb      string
	State     string
	Next      string
}

// PlanStep is one frozen block as a preview presents it. Selection is empty
// when no stage selection is active, and otherwise says whether this
// invocation would start the block or why it would not.
type PlanStep struct {
	ID          string
	Description string
	Stage       string
	Impacts     []string
	State       string
	Selection   string
	WaitsOn     string
	// After names the steps this one waits for, by their place in the plan, so
	// an operator reads what orders the work from the list itself. A step that
	// waits for nothing names none.
	After []int
	// Wave is the earliest round this step can start in, counted from one.
	Wave int
}

const (
	StepStart       = "start"
	StepWaiting     = "waiting"
	StepNotSelected = "not-selected"
)

// PlanResult previews the next legal operation or the exact continuation point
// of an incomplete one. It allocates no identity and creates no log.
type PlanResult struct {
	Context ContextIdentity
	Verb    string
	Steps   []PlanStep
	Stages  []string
	// Waves is how many rounds the plan's own shape needs, and Widest is how
	// many of its steps share the fullest one. Together they say how much of
	// the work the graph lets run at once, which is what an operator reads to
	// know whether a long plan is a long queue or a wide one.
	Waves        int
	Widest       int
	Startable    int
	Deferred     int
	Continuation bool
	// Finalizes marks a preview whose verb only completes the operation's
	// interrupted finalization and then settles, running no block.
	Finalizes bool
	Receipt   Receipt
}

type MachineOutcome struct {
	Machine string
	Status  string
}

type GroupResult struct {
	BlockID     string
	GroupID     string
	Description string
	Status      string
	Machines    int
	Counts      GroupCounts
	Exceptions  []MachineOutcome
	Log         string
}

// GroupCounts always reports all seven members so a consumer never has to
// distinguish absent from zero.
type GroupCounts struct {
	Changed     int
	Unchanged   int
	Skipped     int
	Failed      int
	Unreachable int
	Canceled    int
	Unknown     int
}

type BlockResult struct {
	ID          string
	Description string
	Stage       string
	State       string
	Outcome     string
	Attempts    int
	Groups      []GroupResult
	// Unresolved is why an unproved block's outcome is still unknown and what
	// the operator does about it. Status reports it for each unknown or
	// running block, and it is nil for every other.
	Unresolved *Unresolved
}

type OperationResult struct {
	Context ContextIdentity
	Verb    string
	Steps   []PlanStep
	Blocks  []BlockResult
	Logs    []string
	// LogLocation is where this operation's logs are on the host, for an
	// operator to open. It is human presentation only and is absent from every
	// structured result, which keeps naming paths relative to the state root.
	LogLocation string
	// Settled marks a result that performed no work because durable state
	// already proves it. Its blocks are what an earlier operation completed,
	// never what this invocation did.
	Settled bool
	// Recovered names the only thing a settled invocation did before it
	// settled: RecoveredFinalization or RecoveredRelease, or empty when it did
	// nothing at all. It is never set on a result that performed work.
	Recovered string
	Receipt   Receipt
}

// What a settled invocation may have done before it settled. Either performs
// no effect: a finalization records what the block records already prove, and
// a release returns what an interrupted registration left unowned.
const (
	RecoveredFinalization = "finalization"
	RecoveredRelease      = "release"
)

// Progress phases, in the order an operation runs them. A check proves a
// precondition while no operation exists yet, so it has no log to follow and
// no frozen block to count against; an effect is the work the registered plan
// authorized. The zero value is an effect, because that is what a block
// reports.
const (
	EffectPhase = ""
	CheckPhase  = "check"
)

// ProgressEvent is one row of an operation's progress. Block identifies the
// step the row belongs to and Description names it: a frozen block during
// EffectPhase, the check itself during CheckPhase. Detail names the group or
// observation in flight within it.
type ProgressEvent struct {
	Phase       string
	Block       string
	Group       string
	Description string
	Detail      string
	Status      string
	Position    int
	Total       int
	// Completed of Declared are the block's presentation groups that have
	// reported a terminal outcome, out of the groups its frozen plan declares.
	// A check counts the blocks it has probed out of the blocks it must probe.
	Completed int
	Declared  int
}

type SetupCheck struct {
	ID     string
	Status string
}

type DesiredSummary struct {
	Revision    string
	Environment string
	Files       int
	Objects     int
}

// RealizationStatus is what status reports a selected cluster or shared
// service to be. A value outside these constants proves nothing.
type RealizationStatus string

const (
	// RealizationUnsupported is an object this executable does not realize: no
	// capability claims its kind, its capability refuses it, or it is a managed
	// service retained install-only.
	RealizationUnsupported RealizationStatus = "unsupported"
	// RealizationPending is an object this context has not realized: nothing
	// names it, its blocks have not started, or a removal took it back or
	// released it.
	RealizationPending RealizationStatus = "pending"
	// RealizationDone is an object an apply proved every block of.
	RealizationDone RealizationStatus = "done"
	// RealizationFailed is an object a block of which failed, under either verb.
	RealizationFailed RealizationStatus = "failed"
	// RealizationUnknown is an object a block of which is running or unknown,
	// or one no record proves what a removal did to: an object a replacing
	// removal has not started, or one of a completed removal whose block
	// record does not read done.
	RealizationUnknown RealizationStatus = "unknown"
)

type ClusterSummary struct {
	Name   string
	Kind   string
	Status RealizationStatus
}

type ServiceSummary struct {
	Kind    string
	Name    string
	Machine string
	Status  RealizationStatus
}

// SecretSummary counts the Secret objects the selected input declares and the
// Secret bindings the current operation's record holds, one per operation
// however many Secrets it covers, which is 0 once a completed removal
// finalized.
type SecretSummary struct {
	Declared int
	Bindings int
}

type LifecycleSummary struct {
	Operation string
	Verb      string
	State     string
	Next      string
	Blocks    []BlockResult
	Logs      []string
	// Executable is the identity of the build that registered this operation.
	// A removal is planned from what that build froze, so an operator reads the
	// remedy for a request this one cannot read before meeting the refusal.
	Executable string
}

type StatusResult struct {
	Context         ContextIdentity
	SetupChecks     []SetupCheck
	Desired         DesiredSummary
	Clusters        []ClusterSummary
	StorageClusters []ClusterSummary
	Shared          []ServiceSummary
	Secrets         SecretSummary
	NextSteps       []string
	Lifecycle       *LifecycleSummary
	// Contradictions names what the context's records contradict, in the
	// words a refusal that points at status names each of them.
	Contradictions []string
	// LogLocation is the host path of the reported operation's logs, presented
	// to a human and absent from the structured result.
	LogLocation string
}
