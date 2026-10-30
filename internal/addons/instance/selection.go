package instance

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	catalog := state.Effective()
	result := []lifecycle.Refusal{}
	for _, binding := range catalog.OfKind(api.ClusterAddonBinding) {
		selected, _ := addons.ExpandBinding(binding, catalog)
		for _, addon := range selected {
			result = append(result, lifecycle.RefusalOf(binding, addon.Identity()+" is an authored add-on, which needs the custom-package lifecycle this executable does not offer", "select a built-in package under spec.packages of "+binding.Identity()+", or remove "+addon.Identity()+" from its selection"))
		}
	}
	if c.facts == nil {
		return lifecycle.SortRefusals(result)
	}
	selected, _ := addons.Instances(catalog, c.registry)
	controllerName, _ := lifecycle.ControllerMachine(catalog)
	for _, selection := range selected {
		request, _, err := Render(selection, c.facts(catalog, selection.Instance.Cluster), controllerName)
		if err == nil {
			_, err = request.Canonical()
		}
		if err == nil {
			continue
		}
		binding, ok := catalog.Find(api.ClusterAddonBinding, selection.Instance.Binding)
		if !ok {
			continue
		}
		reason, remedy := unrenderable(selection, binding.Identity(), err)
		result = append(result, lifecycle.RefusalOf(binding, reason, remedy))
	}
	return lifecycle.SortRefusals(result)
}

func unrenderable(selection addons.SelectedInstance, binding string, err error) (string, string) {
	name, cluster := selection.Package.Name, selection.Instance.Cluster
	for _, bound := range []string{"64 owned objects", "32 checks", "96 steps", "request bound"} {
		if strings.Contains(err.Error(), bound) {
			return fmt.Sprintf("the program of package %s for %s passes its %s", name, cluster, bound), "reduce the configuration of package " + name + " in " + binding
		}
	}
	return fmt.Sprintf("package %s cannot be rendered for %s: %s", name, cluster, err), "correct the configuration of package " + name + " in " + binding
}

func unrenderableRefusal(selection addons.SelectedInstance, err error) error {
	reason, remedy := unrenderable(selection, string(api.ClusterAddonBinding)+"/"+selection.Instance.Binding, err)
	return refusal(reason, remedy)
}
