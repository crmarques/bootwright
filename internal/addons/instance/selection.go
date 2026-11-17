package instance

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type rendering struct {
	request   Request
	requires  []reconciliation.ObjectRef
	err       error
	data      []byte
	canonical error
}

type assessment struct {
	refusals  []lifecycle.Refusal
	instanced bool
	selected  []addons.SelectedInstance
	issues    []addons.SelectionIssue
	renders   map[int]rendering
}

var shapeRows = map[addons.ShapeRefusal][2]string{
	addons.ShapeManifestSet: {"<addon> declares a manifest set, which this executable does not install", "remove spec.manifestSet from <addon> and declare an olm arm, or remove <addon> from the selection of <binding>"},
	addons.ShapeSteps:       {"<addon> declares steps, which this executable does not run", "remove spec.steps from <addon>, or remove <addon> from the selection of <binding>"},
	addons.ShapeExternal:    {"<addon> declares an external namespace, which this executable does not install into", "set spec.olm.namespace.management of <addon> to managed, or remove <addon> from the selection of <binding>"},
	addons.ShapeNoGroup:     {"<addon> declares no OperatorGroup, which this executable needs to install its operator", "declare spec.olm.operatorGroup of <addon>, or remove <addon> from the selection of <binding>"},
	addons.ShapeTargets:     {"<addon> targets namespaces other than its own or all, which this executable does not install", "set spec.olm.operatorGroup.targetNamespaces of <addon> to its own namespace or an empty list, or remove <addon> from the selection of <binding>"},
}

func fill(text string, values map[string]string) string {
	for placeholder, value := range values {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}

func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return c.assess(state).refusals
}

func (c Capability) assess(state *compilation.State) assessment {
	catalog := state.Effective()
	result := []lifecycle.Refusal{}
	for _, binding := range catalog.OfKind(api.ClusterAddonBinding) {
		selected, _ := addons.ExpandBinding(binding, catalog)
		for _, addon := range selected {
			if reason, remedy, refused := authoredRefusal(binding, addon); refused {
				result = append(result, lifecycle.RefusalOf(binding, reason, remedy))
			}
		}
	}
	if c.facts == nil {
		return assessment{refusals: lifecycle.SortRefusals(result)}
	}
	selected, issues := addons.Instances(catalog, c.registry)
	assessed := assessment{instanced: true, selected: selected, issues: issues, renders: map[int]rendering{}}
	controllerName, _ := lifecycle.ControllerMachine(catalog)
	for index, selection := range selected {
		facts := c.facts(catalog, selection.Instance.Cluster)
		binding, ok := catalog.Find(api.ClusterAddonBinding, selection.Instance.Binding)
		if hostedNamespace(selection.Package, facts) && ok {
			values := map[string]string{"<addon>": "ClusterAddon/" + selection.Package.Name, "<namespace>": selection.Package.OLM.Namespace, "<cluster>": selection.Instance.Cluster}
			result = append(result, lifecycle.RefusalOf(binding, fill("<addon> uses namespace <namespace>, which KubeVirt hosts for <cluster>", values), fill("set spec.olm.namespace.name of <addon> to a namespace that is not hosted for <cluster>", values)))
			continue
		}
		request, requires, err := Render(selection, facts, controllerName)
		rendered := rendering{request: request, requires: requires, err: err}
		if err == nil {
			rendered.data, rendered.canonical = request.Canonical()
			err = rendered.canonical
		}
		assessed.renders[index] = rendered
		if err == nil || !ok {
			continue
		}
		reason, remedy := unrenderable(selection, binding.Identity(), err)
		result = append(result, lifecycle.RefusalOf(binding, reason, remedy))
	}
	assessed.refusals = lifecycle.SortRefusals(result)
	return assessed
}

func authoredRefusal(binding, addon api.Object) (string, string, bool) {
	values := map[string]string{"<addon>": addon.Identity(), "<binding>": binding.Identity(), "<cluster>": binding.Spec().Get("clusterRef").Text()}
	if row, found := shapeRows[addons.AuthoredShapeRefusal(addon)]; found {
		return fill(row[0], values), fill(row[1], values), true
	}
	if _, err := addons.Authored(addon); err != nil {
		values["<error>"] = err.Error()
		return fill("<addon> cannot be rendered for <cluster>: <error>", values), fill("correct <addon>", values), true
	}
	return "", "", false
}

var renderBounds = []string{"64 owned objects", "32 checks", "96 steps", "request bound"}

func unrenderable(selection addons.SelectedInstance, binding string, err error) (string, string) {
	name, cluster := selection.Package.Name, selection.Instance.Cluster
	authored := selection.Package.Descriptor != ""
	for _, bound := range renderBounds {
		if !strings.Contains(err.Error(), bound) {
			continue
		}
		if authored {
			return fmt.Sprintf("the program of ClusterAddon/%s for %s passes its %s", name, cluster, bound), "reduce the custom resources or readiness checks of ClusterAddon/" + name
		}
		return fmt.Sprintf("the program of package %s for %s passes its %s", name, cluster, bound), "reduce the configuration of package " + name + " in " + binding
	}
	if authored {
		return fmt.Sprintf("ClusterAddon/%s cannot be rendered for %s: %s", name, cluster, err), "correct ClusterAddon/" + name
	}
	return fmt.Sprintf("package %s cannot be rendered for %s: %s", name, cluster, err), "correct the configuration of package " + name + " in " + binding
}

func unrenderableRefusal(selection addons.SelectedInstance, err error) error {
	reason, remedy := unrenderable(selection, string(api.ClusterAddonBinding)+"/"+selection.Instance.Binding, err)
	return refusal(reason, remedy)
}
