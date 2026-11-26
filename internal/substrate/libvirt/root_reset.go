package libvirt

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// RootResetOperation blanks a kept machine's root disk; its reading is the machine's observation.
const RootResetOperation = "root_reset"

const RootResetImplementation = "machine-libvirt-root-reset-v1"

// RootResetMark is the file beside the root disk naming the staged destroy and step that blanked it.
const RootResetMark = "root-reset.mark"

func RootResetContentDigest() string {
	return reconciliation.ContentDigestOf("bootwright.substrate.libvirt.machine-root-reset-v1", RootResetImplementation, machineRequestVersion,
		"blank-root-image-v1", RootResetMark)
}

type RootResetEvidence struct {
	Absent        bool           `json:"absent"`
	Answered      bool           `json:"answered"`
	Disks         []DiskEvidence `json:"disks"`
	Domain        string         `json:"domain"`
	Owned         bool           `json:"owned"`
	Postcondition bool           `json:"postcondition"`
	Request       string         `json:"request"`
	RootReset     string         `json:"rootReset"`
	State         string         `json:"state"`
	System        string         `json:"system"`
}

func (MachineCapability) RootResetContract() (string, string) {
	return RootResetImplementation, RootResetContentDigest()
}

func (MachineCapability) RootResetSecrets(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	return request.Placement.SecretReferences(), err
}

func (MachineCapability) RootResetRefusal(block reconciliation.Block) string {
	if block.ContentDigest != MachineContentDigest() {
		return "was realized under another machine contract than the one this build resets a root disk under; take it back with the build that realized it"
	}
	return ""
}

func (c MachineCapability) ResetRootDisk(ctx context.Context, reset lifecycle.RootReset) (reconciliation.Outcome, reconciliation.EffectState, error) {
	return c.rootDisk(ctx, reset, RootResetOperation)
}

func (c MachineCapability) ObserveRootDisk(ctx context.Context, reset lifecycle.RootReset) (reconciliation.EffectState, error) {
	_, effect, err := c.rootDisk(ctx, reset, lifecycle.ObserveOperation)
	return effect, err
}

func (c MachineCapability) rootDisk(ctx context.Context, reset lifecycle.RootReset, operation string) (reconciliation.Outcome, reconciliation.EffectState, error) {
	request, err := c.prepare(ctx, reset.Execution)
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(reset.Execution, lifecycle.Invocation{
		Implementation: MachineImplementation, Operation: operation, Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Values: map[string]string{"rootReset": reset.Removal, "rootResetStep": reset.Step},
	}))
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[RootResetEvidence](result.Evidence, maxEvidenceBytes, "root reset")
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	if evidence.Request != reset.Block.RequestDigest {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, libvirtEvidenceMismatch.Fail("the root reset evidence names another request", "")
	}
	return result.Outcome, RootResetEffect(evidence, request, reset.Removal+"/"+reset.Step), nil
}

// RootResetEffect reads a root disk's evidence: unknown unless the hypervisor
// answered for this context's own domain, completed only under the mark over
// a root disk at its frozen size, the domain shut off, and otherwise no effect.
func RootResetEffect(evidence RootResetEvidence, request MachineRequest, mark string) reconciliation.EffectState {
	if !evidence.Answered || evidence.Domain != request.Domain || !evidence.Owned || evidence.System != request.UUID {
		return reconciliation.EffectUnknown
	}
	root := request.Disks[0]
	if evidence.RootReset == mark && evidence.State == domainOff && len(evidence.Disks) == 1 && evidence.Disks[0].Name == root.Name &&
		evidence.Disks[0].Present && evidence.Disks[0].SizeGiB == root.SizeGiB {
		return reconciliation.EffectCompleted
	}
	return reconciliation.EffectNoEffect
}
