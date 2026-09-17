package lifecycle

import (
	"context"
	"encoding/json"

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
	Sudo           string
	Launch         prerequisites.PythonLaunch
	Bundle         prerequisites.BundleLocation
	Area           prerequisites.BundleArea
	Material       map[string]secrets.Material
	Log            func(context.Context, operationstore.LogRecord) error
	Progress       func(context.Context, string, string)
	// Output receives the adapter process's own standard output and error.
	Output prerequisites.RunOutput
}

type RunResult struct {
	Outcome  string
	Evidence json.RawMessage
}

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
