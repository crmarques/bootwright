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
			result = append(result, lifecycle.RefusalOf(binding, addon.Identity()+" is an authored add-on package, which needs the custom-package lifecycle this executable does not offer", "select a built-in package under spec.packages of "+binding.Identity()+", or remove "+addon.Identity()+" from its selection"))
		}
	}
	if c.facts == nil {
		return lifecycle.SortRefusals(result)
	}
	selected, _ := addons.Instances(catalog, c.registry)
	controllerName, _ := lifecycle.ControllerMachine(catalog)
	for _, selection := range selected {
		_, _, err := Render(selection, c.facts(catalog, selection.Instance.Cluster), controllerName)
		if err == nil {
			continue
		}
		bound := ""
		for _, candidate := range []string{"64 owned objects", "32 checks", "96 steps", "request bound"} {
			if strings.Contains(err.Error(), candidate) {
				bound = candidate
				break
			}
		}
		if bound != "" {
			binding, ok := catalog.Find(api.ClusterAddonBinding, selection.Instance.Binding)
			if ok {
				result = append(result, lifecycle.RefusalOf(binding, fmt.Sprintf("the program of package %s for %s passes its %s", selection.Package.Name, selection.Instance.Cluster, bound), "reduce the configuration of package "+selection.Package.Name+" in "+binding.Identity()))
			}
		}
	}
	return lifecycle.SortRefusals(result)
}
