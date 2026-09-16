package lifecycle

import (
	"time"

	"github.com/crmarques/bootwright/internal/machine"
)

type PlanRequest struct {
	ContextName string
	Stages      []string
}

type StatusRequest struct {
	ContextName   string
	Watch         bool
	WatchInterval time.Duration
}

type ApplyRequest struct {
	ContextName      string
	Stages           []string
	Authorizations   []string
	SkipConfirmation bool
	Verbose          bool
	SSH              machine.SSHOptions
}

type DestroyRequest struct {
	ContextName      string
	Authorizations   []string
	SkipConfirmation bool
	Verbose          bool
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
}

const (
	StepStart       = "start"
	StepWaiting     = "waiting"
	StepNotSelected = "not-selected"
)

// PlanResult previews the next legal operation or the exact continuation point
// of an incomplete one. It allocates no identity and creates no log.
type PlanResult struct {
	Context      ContextIdentity
	Verb         string
	Steps        []PlanStep
	Stages       []string
	Startable    int
	Deferred     int
	Continuation bool
	Receipt      Receipt
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
	Receipt Receipt
}

// ProgressEvent is one row of an operation's progress. Description names the
// block; Detail names the group or observation in flight within it.
type ProgressEvent struct {
	Block       string
	Group       string
	Description string
	Detail      string
	Status      string
	Position    int
	Total       int
	// Completed of Declared are the block's presentation groups that have
	// reported a terminal outcome, out of the groups its frozen plan declares.
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

type ClusterSummary struct {
	Name   string
	Kind   string
	Status string
}

type ServiceSummary struct {
	Kind    string
	Name    string
	Machine string
	Status  string
}

type SecretSummary struct {
	Declared int
	Bound    int
}

type LifecycleSummary struct {
	Operation string
	Verb      string
	State     string
	Next      string
	Blocks    []BlockResult
	Logs      []string
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
	// LogLocation is the host path of the reported operation's logs, presented
	// to a human and absent from the structured result.
	LogLocation string
}
