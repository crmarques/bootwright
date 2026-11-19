package kubevirt

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (c MachineCapability) StartPowerSecrets(block reconciliation.Block) ([]string, error) {
	return c.SecretReferences(block)
}

// StartPower reads the VM and instance with external credentials. Hosted access would create a Secret.
func (c MachineCapability) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	execution := probe.Execution()
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	if request.HostCluster != "" {
		return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "power on hosted KubeVirt cannot be proved without creating access on the host cluster"}, nil
	}
	unproved := lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "the host cluster did not prove the VirtualMachine's identity and power"}
	var clusterUID, vmUID string
	if len(execution.PreviousEvidence) != 0 {
		previous, err := decodeMachineEvidence(execution.PreviousEvidence, execution.Block.RequestDigest)
		if err != nil {
			return unproved, nil
		}
		held := receipt(previous, "VirtualMachine", request.Name)
		if held != nil && previous.Owned && !previous.Foreign && previous.VirtualMachine == request.Name && previous.KubeSystemUID != "" {
			clusterUID, vmUID = previous.KubeSystemUID, held.UID
		}
	}
	for _, record := range execution.ApplyProved {
		if record.Implementation == HostImplementation && record.Object == request.Provider {
			pin, err := provedHost(execution.ApplyProved, request.Provider)
			if err != nil || clusterUID != "" && clusterUID != pin.KubeSystemUID {
				return unproved, nil
			}
			clusterUID = pin.KubeSystemUID
			break
		}
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: MachineImplementation, Operation: "observe", Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Materials: kubeconfig(request.KubeconfigRef), Deadline: machineDeadline,
		Values: map[string]string{"startPower": "true"},
	}))
	if err != nil {
		return unproved, ctx.Err()
	}
	observed, err := decodeMachineEvidence(result.Evidence, execution.Block.RequestDigest)
	switch {
	case err != nil || !observed.Answered || observed.Foreign || !origin(observed.Server) || observed.KubeSystemUID == "":
		return unproved, nil
	case clusterUID != "" && clusterUID != observed.KubeSystemUID:
		return unproved, nil
	case observed.Absent && observed.VirtualMachine == "" && observed.Instance == "" && len(observed.Receipts) == 0:
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the host reports the VirtualMachine and its instance absent"}, nil
	}
	held := receipt(observed, "VirtualMachine", request.Name)
	if observed.Absent || vmUID == "" || observed.VirtualMachine != request.Name || held == nil || held.UID != vmUID {
		return unproved, nil
	}
	if instanceRunning(observed.Instance) {
		return lifecycle.Quiescence{State: lifecycle.Live, Reason: "its VirtualMachineInstance is in use",
			Stop: "bootwright machine stop --context " + execution.Context + " --name " + request.Identity.Object}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the host reports the VirtualMachine stopped"}, nil
}
