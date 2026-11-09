//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// readOnlyRunner is a sweeping runner bound to every operation, counting how
// often it starts its completing adapter.
func readOnlyRunner(t *testing.T) (Runner, *int) {
	t.Helper()
	started := 0
	runner := sweepingRunner(t, func() *exec.Cmd {
		started++
		return completing()
	})
	runner.playbooks = map[string]string{
		"artifact-server-nginx-v1/apply": "apply.yml", "artifact-server-nginx-v1/observe": "observe.yml", "artifact-server-nginx-v1/destroy": "destroy.yml",
	}
	return runner, &started
}

func assertNothingStaged(t *testing.T, runner Runner) {
	t.Helper()
	for _, parent := range []string{runner.jobParent, runner.scratchParent} {
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("a refused run left %d entries in %s", len(entries), parent)
		}
	}
}

func assertRefused(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message {
		t.Fatalf("the run reported %+v (%v), want %q", reported, err, message)
	}
}

// A read-only invocation that names any adapter operation but observation is
// refused before the runner claims a job, stages scratch or starts its
// adapter, and says it started none; one that names observation runs.
func TestAReadOnlyInvocationRunsOnlyObservation(t *testing.T) {
	for _, operation := range []string{"apply", "destroy"} {
		runner, started := readOnlyRunner(t)
		request := adapterRequest(t, &bytes.Buffer{})
		request.Operation, request.ReadOnly = operation, true
		result, err := runner.Run(context.Background(), request)
		assertRefused(t, err, "a read-only invocation may run only its capability's observation")
		assertNothingStaged(t, runner)
		if *started != 0 || !result.NotStarted {
			t.Fatalf("a read-only %s started its adapter, or did not say it started none", operation)
		}
	}
	runner, started := readOnlyRunner(t)
	request := adapterRequest(t, &bytes.Buffer{})
	request.Operation, request.ReadOnly = lifecycle.ObserveOperation, true
	if result, err := runner.Run(context.Background(), request); err != nil || *started != 1 || result.NotStarted {
		t.Fatalf("a read-only observation started its adapter %d times (%v, %+v)", *started, err, result)
	}
}

// An invocation no operation records, a quiescence probe or a comparison, may
// take over no produced material: one declaring an output is refused before
// any directory exists or its adapter starts, and one declaring none runs.
func TestAnUnrecordedInvocationTakesOverNoMaterial(t *testing.T) {
	runner, started := readOnlyRunner(t)
	request := adapterRequest(t, &bytes.Buffer{})
	request.Operation, request.ReadOnly, request.Unrecorded = lifecycle.ObserveOperation, true, true
	request.Outputs = []lifecycle.OutputFile{{Name: "kubeconfig", Variable: "kubeconfig"}}
	_, err := runner.Run(context.Background(), request)
	assertRefused(t, err, "an invocation no operation records may take over no material, so this observation cannot run outside an operation")
	assertNothingStaged(t, runner)
	if *started != 0 {
		t.Fatal("an unrecorded observation declaring an output started its adapter")
	}
	request.Outputs = nil
	if _, err := runner.Run(context.Background(), request); err != nil || *started != 1 {
		t.Fatalf("an unrecorded observation declaring no output started its adapter %d times (%v)", *started, err)
	}
}

// A lend mints access for one later run of the same execution and creates only
// the per-lend object its token is bound to, which its revoke deletes once
// that run returns, so a read-only or unrecorded execution, a quiescence probe
// or a comparison among them, may run both, the lend with its one output;
// nothing else gains that exemption, a revoke takes over no output, and only
// the implementation whose revoke deletes nothing but its own lend's object
// runs one.
func TestALendRunsInsideAReadOnlyOrUnrecordedExecution(t *testing.T) {
	outputs := []lifecycle.OutputFile{{Name: "kubeconfig", Variable: "kubeconfig"}}
	lend := lifecycle.RunRequest{Operation: lifecycle.LendOperation, ReadOnly: true, Unrecorded: true, Outputs: outputs}
	admitted := Runner{}.AdmitReadOnly("substrate-host-kubevirt-v1/"+lifecycle.RevokeOperation, "artifact-server-nginx-v1/apply")
	if err := admitted.checkReadOnly(lend); err != nil {
		t.Fatalf("a lend was refused: %v", err)
	}
	for _, operation := range []string{"apply", "destroy", lifecycle.ObserveOperation} {
		other := lend
		other.Operation = operation
		if admitted.checkReadOnly(other) == nil {
			t.Fatalf("an unrecorded %s took over an output", operation)
		}
	}
	revoke := lifecycle.RunRequest{Implementation: "substrate-host-kubevirt-v1", Operation: lifecycle.RevokeOperation, ReadOnly: true, Unrecorded: true}
	if err := admitted.checkReadOnly(revoke); err != nil {
		t.Fatalf("a revoke was refused: %v", err)
	}
	if (Runner{}).checkReadOnly(revoke) == nil {
		t.Fatal("a read-only revoke composition did not admit was admitted")
	}
	for _, implementation := range []string{"", "artifact-server-nginx-v1", "machine-kubevirt-v1"} {
		other := revoke
		other.Implementation = implementation
		if admitted.checkReadOnly(other) == nil {
			t.Fatalf("a read-only revoke of %q was admitted", implementation)
		}
	}
	apply := lifecycle.RunRequest{Implementation: "artifact-server-nginx-v1", Operation: "apply", ReadOnly: true}
	if admitted.checkReadOnly(apply) == nil {
		t.Fatal("an admitted operation other than a revoke ran read-only")
	}
	revoke.Outputs = outputs
	if admitted.checkReadOnly(revoke) == nil {
		t.Fatal("an unrecorded revoke took over an output")
	}
}
