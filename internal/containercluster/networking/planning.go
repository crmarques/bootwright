package networking

import (
	"context"
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Capability struct {
	runner   Runner
	registry addons.Registry
}

func New(runner Runner, registry addons.Registry) Capability {
	return Capability{runner: runner, registry: registry}
}
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}, Secrets: []string{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, fmt.Errorf("cluster network planning needs compiled state")
	}
	catalog := input.State.Effective()
	selected, issues := addons.Instances(catalog, c.registry)
	if len(issues) > 0 {
		return plan, fmt.Errorf("cluster network provider selection: %s", issues[0].Issue.Message)
	}
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		if cluster.Spec().Get("networking", "secondaryNetworks").Len() == 0 {
			continue
		}
		controller, err := lifecycle.ControllerMachine(catalog)
		if err != nil {
			return plan, err
		}
		if input.Controller != "" && input.Controller != controller {
			return plan, fmt.Errorf("cluster network controller differs from operation controller")
		}
		request, err := renderRequest(cluster, catalog, controller)
		if err != nil {
			return plan, err
		}
		data, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		requires := []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: cluster.Name()}}
		for _, instance := range selected {
			if instance.Instance.Cluster == cluster.Name() && slices.Contains(instance.Package.Provides, "nmstate") {
				requires = append(requires, reconciliation.ObjectRef{Kind: "ClusterAddonBinding", Object: cluster.Name() + "." + instance.Package.Name})
			}
		}
		if len(requires) == 1 {
			return plan, fmt.Errorf("secondary networks of %s require an executable nmstate package", cluster.Identity())
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: BlockID(cluster.Name()), Description: "configure secondary networks on " + cluster.Name(), Stage: reconciliation.StageAddOns, Requires: requires, Impacts: []string{"configure-secondary-networks " + cluster.Name()}, Groups: []reconciliation.Group{{ID: "configure-networks", Description: "apply and check secondary networks", Machines: []string{controller}}}, Kind: Kind, Object: cluster.Name(), Implementation: Implementation, ContentDigest: ContentDigest(), Request: data})
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
	}
	return plan, nil
}
func renderRequest(cluster api.Object, catalog api.Catalog, controller string) (Request, error) {
	program, err := Render(cluster, catalog)
	if err != nil {
		return Request{}, err
	}
	data, err := co.Freeze(program, co.Options{})
	if err != nil {
		return Request{}, err
	}
	return Request{BudgetSeconds: 1800, Cluster: cluster.Name(), Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: controller}, ProducedInputs: []lifecycle.ProducedInput{{Kind: "OpenShiftCluster", Object: cluster.Name(), Output: "kubeconfig"}}, Program: data, Tool: Tool{Kind: "openshift-clients", Compatibility: "openshift", Version: cluster.Spec().Get("distribution", "release", "version").Text()}, Version: requestVersion}, nil
}
func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	result := []lifecycle.Refusal{}
	for _, cluster := range state.Effective().OfKind(api.OpenShiftCluster) {
		if cluster.Spec().Get("networking", "secondaryNetworks").Len() == 0 {
			continue
		}
		if _, err := Render(cluster, state.Effective()); err != nil {
			result = append(result, lifecycle.RefusalOf(cluster, err.Error(), "reduce or correct the secondary-network declarations"))
		}
	}
	return lifecycle.SortRefusals(result)
}
func (c Capability) ProducedInputs(block reconciliation.Block) ([]lifecycle.ProducedInput, error) {
	r, err := DecodeRequest(block.Request)
	return r.ProducedInputs, err
}
