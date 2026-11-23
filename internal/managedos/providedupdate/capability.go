package providedupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Capability struct{ runner Runner }

func New(runner Runner) Capability { return Capability{runner: runner} }

type Evidence struct {
	Packages      int    `json:"packages"`
	Postcondition bool   `json:"postcondition"`
	Rebooted      bool   `json:"rebooted"`
	Release       string `json:"release"`
	Request       string `json:"request"`
	State         string `json:"state"`
	Version       string `json:"version"`
}

func (Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	var plan lifecycle.CapabilityPlan
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.Preparation != nil {
		return plan, nil
	}
	if input.State == nil {
		return plan, requestInvalid.Fail("provided OS update planning requires compiled state", "")
	}
	for _, object := range input.State.Effective().OfKind(api.Machine) {
		release := machine.ProvidedUpdateRelease(object)
		if release == "" {
			continue
		}
		placement, err := machine.PlacementFor(object, input.Controller)
		if err != nil {
			return plan, err
		}
		id := Identity{Block: blockID(object.Name()), Context: input.Context.Name, Object: object.Name()}
		policy := machine.ProvidedUpdatePolicy(object)
		request := Request{Identity: id, Placement: placement, Policy: policy, Release: release, Version: Version}
		data, err := reconciliation.Freeze(request, "provided OS update")
		if err != nil {
			return plan, err
		}
		if _, err := decodeRequest(data); err != nil {
			return plan, err
		}
		description := "update " + id.Object + " within RHEL " + release
		impacts := []string{"update packages and reboot if changed on " + id.Object}
		if policy == PolicyToRelease {
			description = "update " + id.Object + " to RHEL " + release
			impacts = []string{"update packages to RHEL " + release + " and reboot if changed on " + id.Object}
		}
		if input.Verb == reconciliation.Destroy {
			description, impacts = retentionDescription(id.Object), nil
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: id.Block, Description: description, Stage: reconciliation.StageMachines,
			Kind: string(api.Machine), Object: id.Object, Family: machine.ProvidedUpdateFamily,
			Implementation: Implementation, ContentDigest: ContentDigest(), Request: data,
			Requires: []reconciliation.ObjectRef{{Kind: string(api.Machine), Object: id.Object, Family: machine.HardwareFamily}},
			Impacts:  impacts,
		})
		plan.Secrets = append(plan.Secrets, placement.SecretReferences()...)
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func (Capability) Claimed(catalog api.Catalog) []string {
	var objects []string
	for _, object := range catalog.OfKind(api.Machine) {
		if machine.ProvidedUpdateRelease(object) != "" {
			objects = append(objects, object.Identity())
		}
	}
	return objects
}

func (Capability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	request, err := decodeBlock(block)
	if err != nil {
		return err
	}
	return request.Placement.ProveHostPin(contextName, material)
}

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := decodeBlock(block)
	return request.Placement.SecretReferences(), err
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := decodeBlock(block)
	return request.Placement.SecretClasses(), err
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, state, err := c.run(ctx, execution, "apply", 45*time.Minute)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if state != reconciliation.EffectCompleted || !result.Outcome.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, updateUnproved.Fail("Machine/"+execution.Block.Object+" did not prove its update", "")
	}
	return lifecycle.Result{Outcome: result.Outcome, Evidence: result.Evidence}, nil
}

func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, state, err := c.run(ctx, execution, "observe", 2*time.Minute)
	return lifecycle.Observation{Effect: state, Outcome: reconciliation.OutcomeUnchanged, Evidence: result.Evidence}, err
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, deadline time.Duration) (lifecycle.RunResult, reconciliation.EffectState, error) {
	unknown := reconciliation.EffectUnknown
	request, err := decodeBlock(execution.Block)
	if err != nil {
		return lifecycle.RunResult{}, unknown, err
	}
	if c.runner == nil || execution.ApplyOperation == "" {
		return lifecycle.RunResult{}, unknown, requestInvalid.Fail("provided OS update requires its adapter and original apply identity", "")
	}
	owner := reconciliation.ContentDigestOf(request.Version, request.Identity.Context, request.Identity.Block, execution.ApplyOperation)
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: "bootwright_provided_update",
		Canonical: execution.Block.Request, Placement: request.Placement, Deadline: deadline,
		Values: map[string]string{"owner": owner}, Refusals: refusals(request),
	}))
	if err != nil {
		return result, unknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[Evidence](result.Evidence, 4096, "provided OS update")
	if err != nil || evidence.Version != EvidenceVersion || evidence.Request != execution.Block.RequestDigest ||
		evidence.Release != request.Release || evidence.Packages < 0 {
		return result, unknown, nil
	}
	switch evidence.State {
	case "absent":
		if evidence.Packages == 0 && !evidence.Rebooted && !evidence.Postcondition {
			return result, reconciliation.EffectNoEffect, nil
		}
	case "pending", "upgraded":
		if (evidence.State == "upgraded") == (evidence.Packages > 0) && !evidence.Postcondition && !evidence.Rebooted {
			return result, reconciliation.EffectPartial, nil
		}
	case "complete":
		if evidence.Rebooted == (evidence.Packages > 0) {
			if evidence.Postcondition {
				return result, reconciliation.EffectCompleted, nil
			}
			return result, reconciliation.EffectPartial, nil
		}
	}
	return result, unknown, nil
}

func refusals(request Request) map[string]error {
	result := make(map[string]error)
	major, _, _ := strings.Cut(request.Release, ".")
	mismatch := "the installed system must already run RHEL " + request.Release +
		"; set spec.os.provided.updates.policy to toRelease to update it from an earlier RHEL " + major + " minor release"
	if request.Policy == PolicyToRelease {
		mismatch = "the installed system must run RHEL " + request.Release + " or an earlier RHEL " + major + " minor release"
	}
	for reason, message := range map[string]string{
		"update-release-mismatch":    mismatch,
		"update-release-unavailable": "the enabled repositories offer no RHEL " + request.Release + " release package to update to",
		"update-release-drift":       "the resolved package transaction would leave RHEL " + request.Release,
		"update-no-repository":       "no enabled update repository supplies packages",
		"update-foreign":             "the update receipt could not be attributed to this operation",
		"update-unavailable":         "the host could not resolve or download the release-scoped update",
		"update-failed":              "the package update did not settle",
		"update-reboot":              "the host did not prove that it rebooted after updating",
	} {
		result[reason] = updateRefused.Fail(fmt.Sprintf("Machine/%s: %s", request.Identity.Object, message), "")
	}
	return result
}

func (Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := decodeBlock(block)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: retentionDescription(request.Identity.Object)}, nil
}

func retentionDescription(machine string) string { return "retain the installed updates on " + machine }

func (Capability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "removal retains the Machine and its installed packages"}, nil
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	observation, err := c.ObserveRemoval(ctx, execution)
	return lifecycle.Result{Outcome: observation.Outcome, Evidence: observation.Evidence}, err
}

func (Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeCanceled}, err
	}
	request, err := decodeBlock(execution.Block)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeFailed}, err
	}
	evidence, err := json.Marshal(Evidence{Version: EvidenceVersion, State: "retained", Postcondition: true, Request: execution.Block.RequestDigest, Release: request.Release})
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, err
}

func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := decodeBlock(block)
	if verb != reconciliation.Apply || err != nil || len(data) == 0 {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the release update on Machine "+block.Object, lifecycle.PlacedOn(request.Placement),
		updateUnproved.Fail("Machine/"+block.Object+" did not prove its release-scoped update and any required reboot", "check the host's installed release and boot before repeating the operation"))
}
