package plan

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

type BindingPlan struct {
	Binding string
	Cluster string
	Policy  addons.ClusterAddonPolicy
	Addons  []ExtensionPlan
}

type ExtensionPlan struct {
	Name        string
	Binding     string
	Cluster     string
	Extension   v1alpha1.ClusterAddon
	Policy      addons.ClusterAddonPolicy
	Inputs      []v1alpha1.ClusterAddonBindingInput
	DesiredHash string
}

func (p ExtensionPlan) ComputeDesiredHash() (string, error) {
	return extensionrender.DesiredHash(p.Extension, p.Policy, p.Inputs)
}

type ResourceSummary struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
}

func BindingPlans(state v1alpha1.State) ([]BindingPlan, error) {
	extensions := extensionIndex(state)
	var out []BindingPlan
	for _, binding := range state.ClusterAddonBindings {
		cluster := binding.Spec.ClusterRef.Name
		plan := BindingPlan{
			Binding: binding.Metadata.Name,
			Cluster: cluster,
			Policy:  addons.DefaultPolicy(),
		}
		for _, item := range addoninputs.EffectiveBindingAddons(state, binding) {
			name := item.AddonRef.Name
			if name == "" {
				continue
			}
			extension, ok := extensions[name]
			if !ok {
				return nil, fmt.Errorf("ClusterAddonBinding/%s references missing ClusterAddon/%s", binding.Metadata.Name, name)
			}
			plan.Addons = append(plan.Addons, ExtensionPlan{
				Name:      name,
				Binding:   binding.Metadata.Name,
				Cluster:   cluster,
				Extension: extension,
				Policy:    addons.DefaultPolicy(),
				Inputs:    item.Inputs,
			})
		}
		ordered, err := orderByCapabilities(plan.Addons)
		if err != nil {
			return nil, fmt.Errorf("ClusterAddonBinding/%s: %w", binding.Metadata.Name, err)
		}
		plan.Addons = ordered
		out = append(out, plan)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cluster != out[j].Cluster {
			return out[i].Cluster < out[j].Cluster
		}
		return out[i].Binding < out[j].Binding
	})
	return out, nil
}

func orderByCapabilities(plans []ExtensionPlan) ([]ExtensionPlan, error) {
	n := len(plans)
	if n < 2 {
		return plans, nil
	}
	providers := map[string][]int{}
	for i, plan := range plans {
		for _, capability := range plan.Extension.Spec.Provides {
			providers[capability] = append(providers[capability], i)
		}
	}
	indegree := make([]int, n)
	successors := make([][]int, n)
	for r, plan := range plans {
		linked := map[int]bool{}
		for _, capability := range plan.Extension.Spec.Requires {
			for _, p := range providers[capability] {
				if p == r || linked[p] {
					continue
				}
				linked[p] = true
				successors[p] = append(successors[p], r)
				indegree[r]++
			}
		}
	}
	emitted := make([]bool, n)
	out := make([]ExtensionPlan, 0, n)
	for len(out) < n {
		progressed := false
		for i := 0; i < n; i++ {
			if emitted[i] || indegree[i] != 0 {
				continue
			}
			emitted[i] = true
			out = append(out, plans[i])
			for _, r := range successors[i] {
				indegree[r]--
			}
			progressed = true
			break
		}
		if !progressed {
			return nil, fmt.Errorf("ClusterAddon spec.requires/spec.provides ordering has a cycle")
		}
	}
	return out, nil
}

func ResourceSummaries(extension v1alpha1.ClusterAddon) []ResourceSummary {
	switch extension.Spec.Type {
	case v1alpha1.ClusterAddonTypeOLM:
		resources, _ := extensionrender.OLMResources(extension)
		out := make([]ResourceSummary, 0, len(resources))
		for _, resource := range resources {
			out = append(out, ResourceSummary{
				Kind:      resource.Kind,
				Namespace: resource.Namespace,
				Name:      resource.Name,
				Source:    "generated",
			})
		}
		return out
	case v1alpha1.ClusterAddonTypeManifestSet:
		out := make([]ResourceSummary, 0, len(extension.Spec.ManifestSet.Manifests))
		for _, manifest := range extension.Spec.ManifestSet.Manifests {
			out = append(out, ResourceSummary{Kind: "Manifest", Name: manifest.Path, Source: "file"})
		}
		return out
	default:
		return nil
	}
}

func extensionIndex(state v1alpha1.State) map[string]v1alpha1.ClusterAddon {
	out := map[string]v1alpha1.ClusterAddon{}
	for _, extension := range state.ClusterAddons {
		out[extension.Metadata.Name] = extension
	}
	return out
}
