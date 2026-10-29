package kubevirt

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// PinnedIdentity carries the kube-system UID as UUID and the VirtualMachine's UID as Serial.
func PinnedIdentity(machine string, blocks []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	for _, block := range blocks {
		if block.Implementation != MachineImplementation || block.Object != machine || block.Verb != reconciliation.Apply || block.State != reconciliation.BlockDone {
			continue
		}
		request, err := DecodeMachineRequest(block.Block.Request)
		if err != nil {
			return machineref.HardwareIdentity{}, false, err
		}
		evidence, err := decodeMachineEvidence(block.Evidence, block.Block.RequestDigest)
		if err != nil {
			return machineref.HardwareIdentity{}, false, err
		}
		held := receipt(evidence, "VirtualMachine", request.Name)
		if held == nil || evidence.KubeSystemUID == "" || !evidence.Owned {
			return machineref.HardwareIdentity{}, false, nil
		}
		return machineref.HardwareIdentity{UUID: evidence.KubeSystemUID, Serial: held.UID}, true, nil
	}
	return machineref.HardwareIdentity{}, false, nil
}
