package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// unexplained is what an observation that proved nothing says when its
// capability cannot say more from what it recorded.
var unexplained = Unresolved{
	Reason: "its observation proved neither the effect nor its absence",
	Remedy: "restore the target it ran against so an observation can read it",
}

// unobservedBy is what an unproved block says before any resolution observed
// it: an attempt whose outcome was lost, or whose executor died, is observed
// by command, the next invocation of its verb, before anything else starts.
func unobservedBy(command string) Unresolved {
	return Unresolved{
		Reason: "its attempt's outcome was not recorded and no observation has read it yet",
		Remedy: "run " + command + ", which observes it before anything else starts",
	}
}

// explain says why one block's observation proved nothing, from the evidence
// that observation recorded, for the verb the operation froze. The capability
// that froze the block names the reason when it can, and otherwise the engine
// gives the one every such observation shares. Evidence an observation did not
// return reaches the capability empty, whether it was never recorded or was
// recorded as null, so the refusal after an observation and a later status
// read the same thing.
func (s Service) explain(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) Unresolved {
	if bytes.Equal(bytes.TrimSpace(evidence), []byte("null")) {
		evidence = nil
	}
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return unexplained
	}
	reporter, ok := capability.(UnresolvedReporter)
	if !ok {
		return unexplained
	}
	if unresolved, ok := reporter.Unresolved(verb, block, evidence); ok && unresolved.Reason != "" && unresolved.Remedy != "" {
		return unresolved
	}
	return unexplained
}

// PlacedOn names the host a block's placement runs its adapter against, with
// the address it is reached at when that is not this controller.
func PlacedOn(placement machineref.Placement) string {
	if placement.Local() || placement.Address == "" {
		return "Machine " + placement.Machine
	}
	return "Machine " + placement.Machine + " at " + placement.Address
}

// Drifted is why evidence a capability reads as this request's, which none of
// the verb's checks accept, left its block unknown: the subject on its host is
// not what the verb froze, named by the check that decided. For an apply that
// is the realized target's first difference from the frozen request; for a
// removal, what keeps the target from reading as this context's own. Either
// way the remedy is to restore it. A removal that supersedes an apply resolves
// the apply's block by the removal's own reading (D119), so a target that has
// drifted from the apply's request but still reads as this context's own is
// removed rather than refused. A refusal without a diagnostic explains nothing.
func Drifted(verb reconciliation.Verb, subject, host string, refused error) (Unresolved, bool) {
	reported := diagnostics.Of(refused)
	if len(reported) == 0 || reported[0].Message == "" || subject == "" || host == "" {
		return Unresolved{}, false
	}
	target := subject + " on " + host
	reason := target + " is not what its " + string(verb) + " froze: " + reported[0].Message
	if verb == reconciliation.Destroy {
		return Unresolved{Reason: reason, Remedy: "restore " + target + " so its observation reads it as this context's own"}, true
	}
	return Unresolved{Reason: reason, Remedy: "restore " + target + " to what its frozen request names"}, true
}

// unresolvedFailure is the diagnostic of one effect an observation left
// unknown. It names the block, why it stayed unknown and what the operator
// does before repeating command, the exact command that observes it again.
func unresolvedFailure(block string, unresolved Unresolved, command string) error {
	return failure("lifecycle.unknown",
		"the outcome of "+block+" is still unknown: "+unresolved.Reason,
		unresolved.Remedy+", then repeat "+command+" to observe it again")
}

// unresolvedOf reads why one unproved block of an operation is still unknown:
// what the last resolution of its last attempt recorded, when one observed it,
// read for the verb the operation froze. Each resolution supersedes the one
// before it, so that record is the one that left the block unknown. A block
// no resolution observed names command, which observes it.
func (s Service) unresolvedOf(ctx context.Context, store OperationStore, operation operationstore.Operation, block reconciliation.Block, command string) (Unresolved, error) {
	record, err := store.Block(ctx, operation.ID, block.ID)
	if err != nil {
		return Unresolved{}, err
	}
	if record.Attempts < 1 {
		return unobservedBy(command), nil
	}
	resolution, resolved, err := store.LastResolution(ctx, operation.ID, block.ID, record.Attempts)
	if err != nil {
		return Unresolved{}, err
	}
	if !resolved || resolution.Phase != "observed" {
		return unobservedBy(command), nil
	}
	if failed := resolution.Failure; failed != nil {
		remedy := failed.Remediation
		if remedy == "" {
			remedy = unexplained.Remedy
		}
		return Unresolved{Reason: "its observation could not run: " + failed.Message, Remedy: remedy}, nil
	}
	explained := s.explain(operation.Verb, block, resolution.Evidence)
	// A fresh removal resolves an apply's block by its own check, so what it
	// recorded may be read only by the removal's reading of the same evidence.
	if explained == unexplained && operation.Verb == reconciliation.Apply {
		explained = s.explain(reconciliation.Destroy, block, resolution.Evidence)
	}
	return explained, nil
}

// observationFailure is what a resolution records of an observation that could
// not run: the first diagnostic it reported, its text cut to the record's
// bounds. An undiagnosed failure, or one whose code the record cannot hold,
// records none.
func observationFailure(err error) *operationstore.ObservationFailure {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return nil
	}
	first := reported[0]
	message := bounded(first.Message)
	if first.Code == "" || len(first.Code) > operationstore.MaxFailureCode || !utf8.ValidString(first.Code) || message == "" {
		return nil
	}
	return &operationstore.ObservationFailure{Code: first.Code, Message: message, Remediation: bounded(first.Remediation)}
}

// bounded cuts text to the failure bound at a rune boundary, replacing any
// invalid byte sequence first.
func bounded(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	if len(text) <= operationstore.MaxFailureText {
		return text
	}
	cut := operationstore.MaxFailureText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// explainUnproved names, for each block of a result that is unknown or still
// running, why its outcome is unproved, and command, the exact command that
// observes a block no resolution has.
func (s Service) explainUnproved(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, blocks []BlockResult, command string) error {
	for index := range blocks {
		if !unproved(reconciliation.BlockState(blocks[index].State)) {
			continue
		}
		block, ok := plan.Block(blocks[index].ID)
		if !ok {
			continue
		}
		unresolved, err := s.unresolvedOf(ctx, store, operation, block, command)
		if err != nil {
			return err
		}
		blocks[index].Unresolved = &unresolved
	}
	return nil
}
