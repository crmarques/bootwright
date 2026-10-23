package ceph

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// proveDisks borrows the substrate's frozen volume proof. A matching guest
// path or size alone never grants destructive custody over an OSD device.
func proveDisks(e lifecycle.Execution, r Request, readers DependencyProofs) error {
	if readers == nil {
		return failure("lifecycle.state", "Ceph dependency proof readers are not bound", "")
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	for _, host := range r.Hosts {
		if host.Physical {
			if host.Hardware == nil {
				return failure("lifecycle.state", "physical Ceph host lacks its original hardware proof", "")
			}
			continue
		}
		for _, disk := range host.Devices {
			matched := false
			for _, proof := range proofs {
				if proof.Kind != "Machine" || proof.Object != host.Machine || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
					continue
				}
				if readers.DataDisk(proof.Block, proof.Evidence, disk.Path, disk.Serial, disk.SizeBytes, disk.Rotational) {
					if matched {
						return failure("lifecycle.state", "Ceph data disk has ambiguous original ownership proof", "")
					}
					matched = true
				}
			}
			if !matched {
				return failure("lifecycle.state", "Ceph data disk lacks its original apply's owned libvirt volume proof", "restore the original apply evidence; device path and size alone cannot authorize an OSD")
			}
		}
	}
	return nil
}
