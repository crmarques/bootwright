package kubevirt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const RootResetOperation = "root_reset"

const RootResetImplementation = "machine-kubevirt-root-reset-v1"

const RootResetMark = "bootwright.io/root-reset"

const maxRootResetEvidence = 16 << 10

func RootResetContentDigest() string {
	return reconciliation.ContentDigestOf("bootwright.substrate.kubevirt.machine-root-reset-v1", RootResetImplementation, machineRequestVersion,
		"root-volume-deleted-by-uid", "template-recreates-blank", "uid-tested-mark", RootResetMark, "bound-new-volume", "kubeconfig-ref-only")
}

type RootVolumeEvidence struct {
	Blank        bool   `json:"blank"`
	Name         string `json:"name"`
	Phase        string `json:"phase"`
	Present      bool   `json:"present"`
	Storage      string `json:"storage"`
	StorageClass string `json:"storageClass"`
	UID          string `json:"uid"`
	Volume       string `json:"volume"`
}

type RootResetEvidence struct {
	Answered       bool               `json:"answered"`
	Instance       string             `json:"instance"`
	KubeSystemUID  string             `json:"kubeSystemUID"`
	Owned          bool               `json:"owned"`
	Postcondition  bool               `json:"postcondition"`
	Request        string             `json:"request"`
	Root           RootVolumeEvidence `json:"root"`
	RootReset      string             `json:"rootReset"`
	VirtualMachine string             `json:"virtualMachine"`
}

func (MachineCapability) RootResetContract() (string, string) {
	return RootResetImplementation, RootResetContentDigest()
}

func (MachineCapability) RootResetSecrets(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	return request.SecretReferences(), err
}

func (MachineCapability) RootResetRefusal(block reconciliation.Block) string {
	if block.ContentDigest != MachineContentDigest() {
		return "was realized under another machine contract than the one this build resets a root disk under; take it back with the build that realized it"
	}
	request, err := DecodeMachineRequest(block.Request)
	switch {
	case err != nil:
		return "has a machine request this build cannot read"
	case request.TemplateSnapshot != nil:
		return "has a template-backed root disk that cannot be reset to blank; use a destroy that removes the Machine"
	case request.HostCluster != "":
		return "is hosted on OpenShiftCluster/" + request.HostCluster + ", whose access no root disk reset is lent yet (B1890)"
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

type rootTarget struct {
	pinned       proof
	machine      string
	originalRoot string
}

func (c MachineCapability) rootTargetOf(execution lifecycle.Execution, request MachineRequest) (rootTarget, error) {
	pinned, err := provedHost(execution.ApplyProved, request.Provider)
	if err != nil {
		return rootTarget{}, err
	}
	previous, err := decodeMachineEvidence(execution.PreviousEvidence, execution.Block.RequestDigest)
	held := receipt(previous, "VirtualMachine", request.Name)
	if err != nil || held == nil || held.UID == "" || !previous.Owned || previous.KubeSystemUID != pinned.KubeSystemUID {
		return rootTarget{}, kubevirtOwnershipUnavailable.Fail("the VirtualMachine UID the Machine block's apply recorded is unavailable, so its root disk is not reset", "")
	}
	pinned.Receipts = previous.Receipts
	target := rootTarget{pinned: pinned, machine: held.UID}
	if root := receipt(previous, "DataVolume", request.RootVolume); root != nil {
		target.originalRoot = root.UID
	}
	return target, nil
}

func (c MachineCapability) rootDisk(ctx context.Context, reset lifecycle.RootReset, operation string) (reconciliation.Outcome, reconciliation.EffectState, error) {
	request, err := c.prepare(ctx, reset.Execution)
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	if refusal := c.RootResetRefusal(reset.Block); refusal != "" {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, kubevirtOwnershipUnavailable.Fail("Machine/"+request.Identity.Object+" "+refusal, "")
	}
	target, err := c.rootTargetOf(reset.Execution, request)
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	encoded, err := json.Marshal(target.pinned)
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, kubevirtEvidenceUnencodable.Fail("the KubeVirt machine's pinned host cannot be encoded", "")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(reset.Execution, lifecycle.Invocation{
		Implementation: MachineImplementation, Operation: operation, Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Materials: kubeconfig(request.KubeconfigRef), Deadline: machineDeadline,
		Values: map[string]string{"rootReset": reset.Removal, "rootResetStep": reset.Step, "pinned": string(encoded)},
	}))
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[RootResetEvidence](result.Evidence, maxRootResetEvidence, "root reset")
	if err != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, err
	}
	if evidence.Request != reset.Block.RequestDigest {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, kubevirtEvidenceMismatch.Fail("the root reset evidence names another request", "")
	}
	return result.Outcome, rootResetEffect(evidence, request, target, reset.Removal+"/"+reset.Step), nil
}

func rootResetEffect(evidence RootResetEvidence, request MachineRequest, target rootTarget, mark string) reconciliation.EffectState {
	if !evidence.Answered || !evidence.Owned || evidence.KubeSystemUID != target.pinned.KubeSystemUID || evidence.VirtualMachine != target.machine {
		return reconciliation.EffectUnknown
	}
	class := request.StorageClass
	if class == "" {
		class = target.pinned.StorageClass
	}
	root := evidence.Root
	if evidence.RootReset == mark && !instanceRunning(evidence.Instance) && root.Present && root.Blank && root.Phase == "Succeeded" &&
		root.Name == request.RootVolume && root.Storage == fmt.Sprintf("%dGi", request.DiskGiB) && root.StorageClass == class &&
		root.UID != "" && root.UID != target.originalRoot && root.Volume != "" {
		return reconciliation.EffectCompleted
	}
	return reconciliation.EffectNoEffect
}

var _ lifecycle.RootDiskReset = MachineCapability{}
