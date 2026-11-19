package libvirt

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Admission queries the hypervisor before the emulated BMC or its secrets
// need to exist.
func (MachineCapability) StartPowerSecrets(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	return request.Placement.SecretReferences(), err
}

func (c MachineCapability) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	execution := probe.Execution()
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	unproved := lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "the hypervisor did not prove the domain's identity and power"}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	values := map[string]string{"startPower": "true"}
	if execution.ApplyOperation == "" {
		values["bootstrap"] = "true"
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: MachineImplementation, Operation: "observe", Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Values: values,
	}))
	if err != nil {
		return unproved, ctx.Err()
	}
	observed, err := decodeMachineEvidence(result.Evidence, execution.Block.RequestDigest)
	switch {
	case err != nil || !observed.Answered:
		return unproved, nil
	case observed.HostIdle:
		if observed.Absent || observed.Domain != "" || observed.Owned || observed.State != "" || observed.System != "" {
			return unproved, nil
		}
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "no hypervisor or guest process runs on the provider host"}, nil
	case observed.Absent && observed.Domain == "" && observed.State == "" && observed.System == "" && !observed.Owned:
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the provider host proves the domain absent"}, nil
	case observed.Absent || observed.Domain != request.Domain || !observed.Owned || observed.System != request.UUID:
		return unproved, nil
	case observed.State == domainOff:
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the domain is shut off"}, nil
	case observed.State == "":
		return unproved, nil
	default:
		return lifecycle.Quiescence{State: lifecycle.Live, Reason: "the hypervisor reports the domain in use",
			Stop: "bootwright machine stop --context " + execution.Context + " --name " + request.Identity.Object}, nil
	}
}
