package vsphere

import (
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Kind           = "Machine"
	Implementation = "machine-vsphere-v1"
	requestVersion = "machine-vsphere-v1"
)

func ContentDigest() string {
	return reconciliation.ContentDigestOf("bootwright.substrate.vsphere.machine-v1", Implementation, requestVersion,
		"verified-session+atomic-operation-owner+full-clone+efi+stable-disk-uuid+vmxnet3+vtpm+owned-vmdk-set")
}

var unproved = diagnostics.Stop{Code: "vsphere.machine-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
	Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat, Meaning: "vCenter could not prove the requested Machine identity, devices or state", Remedy: "restore access to the original vCenter and Machine and repeat the operation"}

func Stops() []diagnostics.Stop { return []diagnostics.Stop{unproved} }
