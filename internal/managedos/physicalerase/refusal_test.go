package physicalerase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type eraseRefusingRunner struct {
	reason   string
	requests []lifecycle.RunRequest
}

func (r *eraseRefusingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return lifecycle.RunResult{}, request.Refusals[r.reason]
}

func eraseDestroyExecution(t *testing.T) (EraseRequest, lifecycle.Execution) {
	t.Helper()
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	execution := lifecycle.Execution{Operation: "remove-2", ApplyOperation: "apply-1", Context: "lab", Attempt: 1,
		Block:    reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Request: canonical}, RequestDigest: strings.Repeat("a", 64)},
		Material: fleetMaterial(), PrepareRemoval: func(context.Context, json.RawMessage) error { return nil }}
	originalInstall, _ := originalPhysicalErase(t)
	originalInstall.State = reconciliation.BlockDone
	execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{originalInstall}
	return request, execution
}

func TestPhysicalEraseNamesEachRefusalWithItsCauseAndTheDestroyRemedy(t *testing.T) {
	request, execution := eraseDestroyExecution(t)
	machine := request.Install.Identity.Object
	destroy := "bootwright destroy --context lab again"
	stop := "bootwright machine stop --context lab --name " + machine + " --force"
	retain := "bootwright destroy --context lab --authorize erase=" + machine + ":retain"
	for reason, want := range map[string][]string{
		eraseMachineRunning:     {"is running", stop, destroy},
		eraseIdentityMismatch:   {"answers as another system", destroy, retain},
		eraseHardwareMismatch:   {"hardware addresses", destroy},
		eraseTargetUnanswered:   {"did not answer the system read", destroy},
		eraseControllerRefused:  {"refused a virtual-media, boot or power request", stop, destroy},
		eraseControllerDeadline: {"within its time bound", destroy},
		erasePowerOffUnproved:   {"did not power off", stop, destroy},
		eraseWorkerFailed:       {"stopped without a complete receipt", stop, destroy},
		eraseWorkerUnreachable:  {"did not reach the RAM erase worker", request.Install.Address + " on port 22", stop, destroy, retain},
		eraseForeignWorker:      {"another request or removal attempt", stop, destroy, retain},
		eraseReceiptTimeout:     {"within the erase deadline", stop, destroy},
		eraseForeignCustody:     {"belongs to another request", destroy, retain},
		eraseCustodyDrift:       {"differs from what its removal recorded", destroy, retain},
	} {
		t.Run(reason, func(t *testing.T) {
			runner := &eraseRefusingRunner{reason: reason}
			capability := New(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: "example-system"}})
			result, err := capability.Destroy(context.Background(), execution)
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "lifecycle.state" || found[0].Object == nil || found[0].Object.Name != machine || result.Outcome != reconciliation.OutcomeFailed {
				t.Fatalf("refusal %s = %+v (%v), want one failed lifecycle.state of Machine/%s", reason, result, found, machine)
			}
			text := found[0].Message + " " + found[0].Remediation
			for _, part := range want {
				if !strings.Contains(text, part) {
					t.Fatalf("refusal %s = %q, want %q", reason, text, part)
				}
			}
			uuid, _ := base64.StdEncoding.DecodeString(runner.requests[0].MaterialValues["pinnedUUIDBase64"])
			if strings.Contains(text, string(uuid)) || strings.Contains(text, "lifecycle adapter") {
				t.Fatalf("refusal %s carries a pinned value or the generic adapter failure: %q", reason, text)
			}
		})
	}
	if len(eraseRefusals("lab", "", machine, "https://bmc", "192.0.2.10")) != 13 {
		t.Fatal("the erase refusal vocabulary is not the thirteen codes the collection names")
	}
}
