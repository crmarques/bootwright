package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// MaterialFile names one operation-scoped file a bound Secret part is written
// to. Anything not listed never leaves bounded memory.
type MaterialFile struct {
	Name     string
	Part     secrets.Part
	Secret   string
	Variable string
}

// OutputFile names one file a run on the controller may leave for the runner
// to read back into RunResult.Produced. Variable is the key the adapter finds
// that file's path under; the file never outlives the run.
type OutputFile struct {
	Name     string
	Variable string
}

// Produced is one output a run left, read back as bounded memory the caller
// owns and clears. It is material, never evidence.
type Produced struct {
	Name     string
	Material secrets.Material
}

// ClearProduced clears every output's material.
func ClearProduced(produced []Produced) {
	for _, output := range produced {
		output.Material.Clear()
	}
}

// RunRequest is one authorized adapter invocation, in terms every capability
// shares. Implementation selects the automation, so two implementations of one
// kind never run each other's playbook. Material is bounded memory owned by the
// caller; the adapter writes it only to operation-scoped files it removes, and
// never to arguments, environment, evidence or logs.
type RunRequest struct {
	Implementation string
	Operation      string
	Variable       string
	Digest         string
	Canonical      []byte
	Placement      machineref.Placement
	Materials      []MaterialFile
	// MaterialValues are non-secret values the adapter needs beside the
	// material paths, such as a certificate fingerprint a probe compares.
	MaterialValues map[string]string
	Outputs        []OutputFile
	Launch         prerequisites.PythonLaunch
	Bundle         prerequisites.BundleLocation
	Area           prerequisites.BundleArea
	Material       map[string]secrets.Material
	Log            func(context.Context, operationstore.LogRecord) error
	Progress       func(context.Context, string, string)
	// Output receives the adapter process's own standard output and error.
	Output prerequisites.RunOutput
	// OutputRemediation is what a failure that output explains tells an
	// operator to read. Only the caller knows where the output is named, and a
	// run that names none offers none.
	OutputRemediation string
	// Deadline bounds the run: the capability derives it from the waits its
	// frozen request budgets, and the runner holds it to MaxDeadline. Zero
	// keeps the runner's default.
	Deadline time.Duration
}

// MaxDeadline is the longest any adapter run may take, whatever deadline its
// request states. A capability that states one derives it from the budgets its
// request froze, and its own tests hold what it derives to this ceiling; a
// cluster installation's deadline grows with its nodes, and selection refuses
// one that would pass this ceiling rather than letting it be clamped. An
// operation runs one block at a time, so this is also the longest one block
// holds back the rest of its operation; a budget that needs more is one to
// shorten, not a reason to raise this.
const MaxDeadline = 6 * time.Hour

type RunResult struct {
	Outcome  string
	Evidence json.RawMessage
	Produced []Produced
}

// Invocation is what a capability supplies to turn one authorized attempt into
// one adapter run: the automation it selects, the frozen bytes it crosses with,
// where that work runs, and the extra material and values its own automation
// needs. Everything else the engine already owns.
type Invocation struct {
	Implementation string
	Operation      string
	Variable       string
	Canonical      []byte
	Placement      machineref.Placement
	Materials      []MaterialFile
	Values         map[string]string
	Outputs        []OutputFile
	// Deadline is the run's own deadline, derived from the budgets the frozen
	// request carries; zero keeps the runner's default.
	Deadline time.Duration
}

// RunFor builds the adapter request one attempt authorizes. A capability never
// assembles one itself, so none can widen what crosses the boundary or
// substitute an identity the attempt did not freeze.
func RunFor(execution Execution, invocation Invocation) RunRequest {
	return RunRequest{
		Implementation:    invocation.Implementation,
		Operation:         invocation.Operation,
		Variable:          invocation.Variable,
		Digest:            execution.Block.RequestDigest,
		Canonical:         invocation.Canonical,
		Placement:         invocation.Placement,
		Materials:         append(slices.Clone(invocation.Materials), Materials(invocation.Placement)...),
		MaterialValues:    invocation.Values,
		Outputs:           slices.Clone(invocation.Outputs),
		Launch:            execution.Launch,
		Bundle:            execution.Bundle,
		Area:              execution.Area,
		Material:          execution.Material,
		Log:               execution.Log,
		Progress:          execution.Progress,
		Output:            execution.Output,
		Deadline:          invocation.Deadline,
		OutputRemediation: attemptOutputRemediation,
	}
}

// attemptOutputRemediation points an adapter failure at the output an attempt
// keeps beside its own log.
const attemptOutputRemediation = "read the adapter output retained beside this attempt's log"

// Materials lists exactly which bound parts a placement needs on disk. A local
// placement needs none; the SSH arm needs its identity and host key.
func Materials(placement machineref.Placement) []MaterialFile {
	var files []MaterialFile
	if placement.PrivateKeyRef != "" {
		files = append(files, MaterialFile{Name: "id", Part: secrets.PrivateKeyPart, Secret: placement.PrivateKeyRef, Variable: "identity"})
	}
	if placement.KnownHostsRef != "" {
		files = append(files, MaterialFile{Name: "known_hosts", Part: secrets.ValuePart, Secret: placement.KnownHostsRef, Variable: "knownHosts"})
	}
	return files
}

// AttemptOutcome maps an adapter failure to the outcome it justifies. The
// runner separates a failure it diagnosed — the adapter exited within its
// authorized boundary and said why, so a repeat is the capability's own
// idempotent path — from one it cannot account for, such as a cancellation or
// a lost result. Only a diagnosed failure that is not itself flagged unknown
// leaves the block failed; anything else stays unknown, because a repeat could
// race an effect still in flight.
//
// The distinction matters because nothing retries, destroys or deletes past an
// unknown block: reporting every adapter failure that way strands the context
// with no path forward.
func AttemptOutcome(err error) reconciliation.Outcome {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return reconciliation.OutcomeUnknown
	}
	for _, entry := range reported {
		if entry.Code == "lifecycle.unknown" {
			return reconciliation.OutcomeUnknown
		}
	}
	return reconciliation.OutcomeFailed
}
