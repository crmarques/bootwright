package lifecycle

import (
	"time"

	"github.com/crmarques/bootwright/internal/machine"
)

type PlanRequest struct {
	ContextName string
}

type StatusRequest struct {
	ContextName   string
	Watch         bool
	WatchInterval time.Duration
}

type ApplyRequest struct {
	ContextName      string
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

type PlanStep struct {
	ID          string
	Description string
	Impacts     []string
	State       string
}

// PlanResult previews the next legal operation or the exact continuation point
// of an incomplete one. It allocates no identity and creates no log.
type PlanResult struct {
	Context      ContextIdentity
	Verb         string
	Steps        []PlanStep
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
	Receipt Receipt
}

type ProgressEvent struct {
	Block       string
	Group       string
	Description string
	Status      string
	Position    int
	Total       int
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
}
