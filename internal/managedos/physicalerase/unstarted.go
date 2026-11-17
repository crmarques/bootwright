package physicalerase

import (
	"bytes"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// PhysicalEraseUnstarted proves retaining cleanup of a positively unstarted
// physical installation. Missing records are unknown, never no-effect proof.
func PhysicalEraseUnstarted(original []lifecycle.BlockEvidence, removed []lifecycle.RemovalEvidence, machine string) (bool, error) {
	var installs, policies []lifecycle.BlockEvidence
	for _, proof := range original {
		if proof.Kind != installation.Kind || proof.Object != machine {
			continue
		}
		switch {
		case installation.IsInstallation(proof.Implementation):
			installs = append(installs, proof)
		case IsErase(proof.Implementation):
			policies = append(policies, proof)
		}
	}
	unknown := func() (bool, error) {
		return false, refusal("lifecycle.state", "physical no-install cleanup has no exact original installation and erase-policy proof", "restore the original apply and removal records")
	}
	if len(installs) != 1 {
		return unknown()
	}
	install := installs[0]
	if !installation.OriginalInstall(install, machine) {
		return unknown()
	}
	request, err := installation.DecodeRequest(install.Block.Request)
	if err != nil {
		return false, err
	}
	if !request.Target.Physical || request.Identity.Object != machine || request.Identity.Block != installation.BlockID(machine) {
		return unknown()
	}
	if install.State != reconciliation.BlockPending || len(install.Evidence) != 0 {
		return false, nil
	}
	if !request.EraseOnRemoval {
		if len(policies) != 0 {
			return unknown()
		}
		return true, nil
	}
	if len(policies) != 1 {
		return unknown()
	}
	policy := policies[0]
	if !installation.OriginalApply(policy, machine, EraseImplementation, EraseBlockID(machine)) {
		return unknown()
	}
	erasure, err := DecodeEraseRequest(policy.Block.Request)
	if err != nil {
		return false, err
	}
	if erasure.InstallDigest != install.Block.RequestDigest || erasure.Install.Identity.Object != machine {
		return unknown()
	}
	var completions []lifecycle.RemovalEvidence
	for _, pair := range removed {
		if pair.Removed.Block.ID == policy.Block.ID {
			completions = append(completions, pair)
		}
	}
	if policy.State == reconciliation.BlockPending && len(policy.Evidence) == 0 && len(completions) == 0 {
		return true, nil
	}
	if policy.State != reconciliation.BlockDone || len(completions) != 1 || eraseEvidence(policy.Evidence, erasure, policy.Block.RequestDigest, false) != nil {
		return unknown()
	}
	pair := completions[0]
	if !installation.OriginalApply(pair.Applied, machine, EraseImplementation, EraseBlockID(machine)) || pair.Applied.State != policy.State || !bytes.Equal(pair.Applied.Evidence, policy.Evidence) || pair.Applied.Block.RequestDigest != policy.Block.RequestDigest ||
		!bytes.Equal(pair.Applied.Block.Request, policy.Block.Request) || pair.Removed.State != reconciliation.BlockDone || pair.Removed.Verb != reconciliation.Destroy ||
		pair.Removed.Kind != installation.Kind || pair.Removed.Object != machine || pair.Removed.Implementation != EraseImplementation ||
		pair.Removed.Block.ID != policy.Block.ID || pair.Removed.Block.Kind != installation.Kind || pair.Removed.Block.Object != machine || pair.Removed.Block.Implementation != EraseImplementation ||
		pair.Removed.Block.RequestDigest != policy.Block.RequestDigest || !bytes.Equal(pair.Removed.Block.Request, policy.Block.Request) {
		return unknown()
	}
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](pair.Removed.Evidence, 32768, "physical erase retention")
	if err != nil {
		return false, err
	}
	if proof.Version != EraseImplementation || proof.Request != policy.Block.RequestDigest || !proof.Prepared || !proof.Removed || !proof.Retained || proof.Receipt != nil {
		return unknown()
	}
	return true, nil
}
