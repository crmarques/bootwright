package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Capability struct {
	runner   Runner
	registry addons.Registry
	facts    ClusterFacts
}

func New(runner Runner, registry addons.Registry, facts ClusterFacts) Capability {
	return Capability{runner: runner, registry: registry, facts: facts}
}

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, fmt.Errorf("add-on planning needs compiled state")
	}
	if refusals := c.Unsupported(input.State); len(refusals) > 0 {
		return lifecycle.CapabilityPlan{}, refusal("selected add-on cannot be realized", "select a built-in package")
	}
	selected, issues := addons.Instances(input.State.Effective(), c.registry)
	if len(issues) > 0 {
		return lifecycle.CapabilityPlan{}, refusal(issues[0].Issue.Message, "correct the add-on binding")
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}, Secrets: []string{}}
	if len(selected) == 0 {
		return plan, nil
	}
	controllerName, err := lifecycle.ControllerMachine(input.State.Effective())
	if err != nil {
		return plan, err
	}
	if input.Controller != "" && input.Controller != controllerName {
		return plan, refusal("add-on controller differs from the operation controller", "correct the controller selection")
	}
	if c.facts == nil {
		return plan, refusal("add-on cluster facts are unavailable", "")
	}
	for _, selection := range selected {
		request, requires, err := Render(selection, c.facts(input.State.Effective(), selection.Instance.Cluster), controllerName)
		if err != nil {
			return plan, unrenderableRefusal(selection, err)
		}
		if err := validatePlacements(request, input.State.Effective()); err != nil {
			return plan, err
		}
		data, err := request.Canonical()
		if err != nil {
			return plan, unrenderableRefusal(selection, err)
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: BlockID(request.Package, request.Cluster), Description: "install add-on " + request.Package + " on " + request.Cluster, Stage: reconciliation.StageAddOns, Requires: requires, Impacts: []string{"install-addon " + request.Package + " " + request.Cluster}, Groups: groups(request), Kind: Kind, Object: Object(request.Cluster, request.Package), Implementation: Implementation, ContentDigest: ContentDigest(), Request: data})
		if input.Verb == reconciliation.Destroy {
			index := len(plan.Definitions) - 1
			removal, err := c.Removal(ctx, reconciliation.Block{BlockDefinition: plan.Definitions[index]})
			if err != nil {
				return plan, err
			}
			plan.Definitions[index].Description = removal.Description
			plan.Definitions[index].Impacts = removal.Impacts
			plan.Definitions[index].Groups = removal.Groups
		}
		plan.Secrets = append(plan.Secrets, request.Secrets...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}
func groups(request Request) []reconciliation.Group {
	return []reconciliation.Group{{ID: "install-addon", Description: "install and check add-on " + request.Package, Machines: []string{request.Placement.Machine}}}
}
func (c Capability) ProducedInputs(block reconciliation.Block) ([]lifecycle.ProducedInput, error) {
	request, err := DecodeRequest(block.Request)
	return request.ProducedInputs, err
}
func (c Capability) HostObject(block reconciliation.Block) (reconciliation.ObjectRef, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return reconciliation.ObjectRef{}, err
	}
	return reconciliation.ObjectRef{Kind: "OpenShiftCluster", Object: request.Cluster}, nil
}
func (c Capability) StageTail(block reconciliation.BlockDefinition) (reconciliation.ObjectRef, bool, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return reconciliation.ObjectRef{}, false, err
	}
	pkg, ok := c.registry.Find(request.Package)
	return reconciliation.ObjectRef{Kind: "OpenShiftCluster", Object: request.Cluster}, ok && pkg.Last, nil
}
func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: "retire add-on " + request.Package + " with " + request.Cluster + "; its objects leave with the cluster disks or stay on a cluster that keeps running", Impacts: []string{}}, nil
}
func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, "apply")
	safe := driver.SafeEvidence(result.Evidence, execution.Block.RequestDigest, evidenceVersion)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err), Evidence: safe}, err
	}
	effect, err := effectOf(result.Evidence, request, execution.Block.RequestDigest)
	if err != nil || effect != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown, Evidence: safe}, refusal("add-on adapter did not prove completion", "repeat apply to observe its objects")
	}
	outcome := reconciliation.OutcomeChanged
	if result.Outcome == "unchanged" {
		outcome = reconciliation.OutcomeUnchanged
	} else if result.Outcome != "changed" {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown, Evidence: safe}, refusal("add-on adapter returned an unknown outcome", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, request, err := c.run(ctx, execution, "observe")
	result.Evidence = driver.SafeEvidence(result.Evidence, execution.Block.RequestDigest, evidenceVersion)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, err
	}
	effect, err := effectOf(result.Evidence, request, execution.Block.RequestDigest)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}
func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{}, err
	}
	if _, err := DecodeRequest(execution.Block.Request); err != nil {
		return lifecycle.Result{}, err
	}
	data, err := retirement(execution.Block.RequestDigest)
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: data}, err
}
func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, err := c.Destroy(ctx, execution)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
}
func (c Capability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{}, err
	}
	if _, err := DecodeRequest(probe.Block.Request); err != nil {
		return lifecycle.Quiescence{}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "add-on objects are retired with their cluster"}, nil
}
func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, Request, error) {
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if err := validateNamespaces(request, c.registry); err != nil {
		return lifecycle.RunResult{}, request, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	placements := []co.SecretPlacement{}
	for _, step := range program.Steps {
		if step.Place != nil {
			placements = append(placements, step.Place.SecretPlacement)
		}
	}
	result, err := driver.Run(ctx, c.runner, execution, operation, driver.Config{Placements: placements, Implementation: Implementation, Variable: "bootwright_addon_instance", EvidenceVersion: evidenceVersion, Placement: request.Placement, Tool: controller.InstalledTool{Kind: request.Tool.Kind, Compatibility: request.Tool.Compatibility, Version: request.Tool.Version, Executable: "oc"}, BudgetSeconds: request.BudgetSeconds, ProducedInput: request.ProducedInputs[0]})
	return result, request, err
}
func (c Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	return c.unresolved(verb, block, data, "destroy and apply again")
}

func (c Capability) UnresolvedAdded(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	return c.unresolved(verb, block, data, "remove or correct the object on the cluster")
}

func (c Capability) unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage, remedy string) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || verb == reconciliation.Destroy {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeEvidence(data, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	for _, object := range evidence.Observations {
		if object.Ownership == co.Foreign || object.Ownership == co.Unknown {
			return lifecycle.Unresolved{Reason: object.Kind + "/" + object.Namespace + "/" + object.Name + " is foreign or could not be read", Remedy: remedy}, true
		}
	}
	for _, olm := range evidence.OLM {
		return lifecycle.Unresolved{Reason: "Subscription/" + olm.Namespace + "/" + olm.Subscription + " reports CSV " + olm.InstalledCSV + " without proving the declared installation", Remedy: remedy}, true
	}
	return lifecycle.Unresolved{Reason: "add-on " + request.Package + " on " + request.Cluster + " did not prove its objects and checks", Remedy: remedy}, true
}

func (c Capability) ReadsMaterialOnlyInApply(block reconciliation.Block) bool {
	_, err := DecodeRequest(block.Request)
	return err == nil
}

func (c Capability) AdmitAdded(lineage reconciliation.Plan, added reconciliation.Block) error {
	request, err := DecodeRequest(added.Request)
	if err != nil {
		return refusal("the add-on instance "+added.ID+" cannot be added: "+err.Error(), "")
	}
	cluster := request.ProducedInputs[0]
	if !slices.ContainsFunc(lineage.Blocks, func(block reconciliation.Block) bool {
		return block.Kind == cluster.Kind && block.Object == cluster.Object
	}) {
		return refusal("the add-on "+request.Package+" cannot be added: the completed apply did not install the cluster "+request.Cluster, "declare it on a cluster that apply installed, or take the apply back and apply again")
	}
	return nil
}

func (c Capability) RetiresOffline() bool { return true }
