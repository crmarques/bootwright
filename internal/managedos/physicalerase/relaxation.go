package physicalerase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	metadataClearDescription = "overwrite and read back only the metadata regions of the declared physical disks of "
	retainDescription        = "retain the declared physical disks of "
	withdrawDeadline         = 10 * time.Minute
)

func (Capability) RelaxesMachine(block reconciliation.Block) bool {
	if block.Kind != installation.Kind || block.Implementation != EraseImplementation || block.ID != EraseBlockID(block.Object) {
		return false
	}
	request, err := DecodeEraseRequest(block.Request)
	return err == nil && request.Install.Identity.Object == block.Object
}

func (c Capability) Relax(_ context.Context, block reconciliation.Block, method string) (lifecycle.Removal, error) {
	token := "--authorize " + lifecycle.EraseAuthorization + block.Object + ":" + method
	if !c.RelaxesMachine(block) {
		return lifecycle.Removal{}, refusal("lifecycle.authorization", token+" names a block that is not the removal erase of Machine/"+block.Object, "repeat the command without "+token)
	}
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	without := "repeat bootwright destroy --context " + request.Install.Identity.Context + " without " + token
	switch method {
	case lifecycle.EraseRetain:
	case lifecycle.EraseMetadataClear:
		if !slices.ContainsFunc(request.Targets, func(target EraseTarget) bool { return target.Method != EraseMetadataClear }) {
			return lifecycle.Removal{}, refusal("lifecycle.authorization",
				token+" names Machine/"+block.Object+", whose onRemoval already clears only metadata on every disk it erases, so the relaxation changes nothing",
				without+", or name "+lifecycle.EraseAuthorization+block.Object+":"+lifecycle.EraseRetain+" to waive the erase")
		}
	default:
		return lifecycle.Removal{}, refusal("lifecycle.authorization", token+" names "+method+", and an erase relaxation is metadataClear or retain", without)
	}
	return eraseRemoval(request, method), nil
}

func eraseRemoval(request EraseRequest, relaxation string) lifecycle.Removal {
	machine := request.Install.Identity.Object
	removal := lifecycle.Removal{Description: eraseDescription + machine, Impacts: removalImpacts(request, relaxation), Consumes: []string{reconciliation.AuthorizationDataLoss}}
	switch relaxation {
	case lifecycle.EraseRetain:
		removal.Description = retainDescription + machine + " without erasing them; power and media state unproved"
		return removal
	case lifecycle.EraseMetadataClear:
		removal.Description = metadataClearDescription + machine
	}
	effective := request
	effective.Targets = effectiveTargets(request, relaxation)
	removal.Groups = eraseGroups(effective)
	return removal
}

func relaxedImpacts(request EraseRequest, relaxation string) []string {
	machine := request.Install.Identity.Object
	if relaxation == lifecycle.EraseRetain {
		return []string{
			"retain-disks " + machine + " (erase waived; data, installed OS and Ceph host state remain)",
			"unproved-power-and-media " + machine + " (power and media state unproved; an earlier removal's erase worker may still be running)",
		}
	}
	return []string{"relax-erase " + machine + " (--authorize " + lifecycle.EraseAuthorization + machine + ":" + relaxation + ")"}
}

func effectiveTargets(request EraseRequest, relaxation string) []EraseTarget {
	targets := slices.Clone(request.Targets)
	if relaxation == lifecycle.EraseMetadataClear {
		for index := range targets {
			targets[index].Method = EraseMetadataClear
		}
	}
	return targets
}

func relaxedRequest(request EraseRequest, relaxation string) (EraseRequest, error) {
	switch relaxation {
	case "", lifecycle.EraseMetadataClear:
		request.Targets = effectiveTargets(request, relaxation)
		return request, nil
	}
	return EraseRequest{}, refusal("lifecycle.state", "physical erase block carries the relaxation "+relaxation+", which this build does not perform", "restore the original removal records")
}

func eraseMACs(request EraseRequest) []string {
	macs := []string{}
	if request.Install.Target.Hardware == nil {
		return macs
	}
	for _, nic := range request.Install.Target.Hardware.Interfaces {
		macs = append(macs, strings.ToLower(nic.MACAddress))
	}
	slices.Sort(macs)
	return macs
}

func waivedRetention(data json.RawMessage, digest string) bool {
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](data, 32768, "physical erase")
	return err == nil && proof.Version == EraseImplementation && proof.Request == digest && proof.Prepared && proof.Removed && proof.Retained && proof.Receipt == nil
}

func waivedProof(request EraseRequest, digest string) EraseReceipt {
	receipt := EraseReceipt{Version: EraseImplementation, Request: digest, MACs: eraseMACs(request), Targets: []EraseCoverage{}}
	for _, target := range request.Targets {
		receipt.Targets = append(receipt.Targets, EraseCoverage{EraseTarget: target})
	}
	return receipt
}

func (c Capability) withdraw(ctx context.Context, execution lifecycle.Execution, request EraseRequest, operation string) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase runner is missing", "")
	}
	if execution.ApplyOperation == "" {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "physical erase has no original apply operation", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{
		"relaxationBase64": base64.StdEncoding.EncodeToString([]byte(lifecycle.EraseRetain)),
		"originalAttempt":  execution.Operation + "/" + execution.Block.ID + "/" + strconv.Itoa(execution.Attempt),
		"applyOperation":   execution.ApplyOperation,
	}
	install := request.Install
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: EraseImplementation, Operation: operation, Variable: eraseVariable, Canonical: canonical,
		Placement: install.Placement, Values: values, Deadline: withdrawDeadline,
		Refusals: eraseRefusals(execution.Context, execution.Continuation, install.Identity.Object, install.Target.Controller.Endpoint, install.Address,
			eraseCustody(execution.Context, install.Identity.Object, execution.ApplyOperation), erasePublication(request))}))
}

func (c Capability) destroyWaived(ctx context.Context, execution lifecycle.Execution, request EraseRequest) (lifecycle.Result, error) {
	result, err := c.withdraw(ctx, execution, request, "destroy")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if !waivedRetention(result.Evidence, execution.Block.RequestDigest) {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "physical erase waiver did not prove its leftover private publication and boot-private custody withdrawn", "continue the same destroy")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: result.Evidence}, nil
}

func (c Capability) observeWaived(ctx context.Context, execution lifecycle.Execution, request EraseRequest) (lifecycle.Observation, error) {
	result, err := c.withdraw(ctx, execution, request, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	effect := reconciliation.EffectUnknown
	if waivedRetention(result.Evidence, execution.Block.RequestDigest) {
		effect = reconciliation.EffectCompleted
	} else if eraseUnfinished(result.Evidence, execution.Block.RequestDigest) {
		effect = reconciliation.EffectPartial
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}
