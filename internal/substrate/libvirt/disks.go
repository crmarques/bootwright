package libvirt

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func ProvedDataDisk(block reconciliation.Block, evidence []byte, guestPath, serial string, sizeBytes int64) bool {
	if block.Kind != MachineKind || block.Implementation != MachineImplementation {
		return false
	}
	request, err := DecodeMachineRequest(block.Request)
	if err != nil || ValidateMachinePresence(evidence, request, block.RequestDigest) != nil {
		return false
	}
	for _, disk := range request.Disks {
		if disk.Name != "root" && disk.Serial != "" && "/dev/"+disk.Target == guestPath && disk.Serial == serial && int64(disk.SizeGiB)<<30 == sizeBytes {
			return true
		}
	}
	return false
}
