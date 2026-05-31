package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

type BindingPlan struct {
	Binding string
	Cluster string
	Policy  v1alpha1.ClusterAddonPolicy
	Addons  []ExtensionPlan
}

type ExtensionPlan struct {
	Name      string
	Binding   string
	Cluster   string
	Extension v1alpha1.ClusterAddon
	Policy    v1alpha1.ClusterAddonPolicy
}

type ResourceSummary struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
}

func BindingPlans(state v1alpha1.State) ([]BindingPlan, error) {
	addons := extensionIndex(state)
	var out []BindingPlan
	for _, binding := range state.ClusterAddonBindings {
		names, err := expandBindingExtensionNames(state, binding)
		if err != nil {
			return nil, err
		}
		for _, cluster := range binding.Spec.ContainerClusterSelector.Names {
			plan := BindingPlan{
				Binding: binding.Metadata.Name,
				Cluster: cluster,
				Policy:  binding.Spec.Policy,
			}
			for _, name := range names {
				extension, ok := addons[name]
				if !ok {
					return nil, fmt.Errorf("ClusterAddonBinding/%s references missing ClusterAddon/%s", binding.Metadata.Name, name)
				}
				plan.Addons = append(plan.Addons, ExtensionPlan{
					Name:      name,
					Binding:   binding.Metadata.Name,
					Cluster:   cluster,
					Extension: extension,
					Policy:    binding.Spec.Policy,
				})
			}
			out = append(out, plan)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cluster != out[j].Cluster {
			return out[i].Cluster < out[j].Cluster
		}
		return out[i].Binding < out[j].Binding
	})
	return out, nil
}

func ResourceSummaries(extension v1alpha1.ClusterAddon) []ResourceSummary {
	switch extension.Spec.Type {
	case v1alpha1.ClusterAddonTypeOLMOperator:
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

func expandBindingExtensionNames(state v1alpha1.State, binding v1alpha1.ClusterAddonBinding) ([]string, error) {
	var expanded []string
	for _, ref := range binding.Spec.Profiles {
		names, err := ExpandSet(state, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("ClusterAddonBinding/%s spec.profiles[%s]: %w", binding.Metadata.Name, ref.Name, err)
		}
		expanded = append(expanded, names...)
	}
	for _, ref := range binding.Spec.Addons {
		expanded = append(expanded, ref.Name)
	}
	return firstOccurrence(expanded), nil
}

func ExpandSet(state v1alpha1.State, name string) ([]string, error) {
	sets := setIndex(state)
	return expandSet(sets, name, nil)
}

func expandSet(sets map[string]v1alpha1.ClusterAddonProfile, name string, stack []string) ([]string, error) {
	set, ok := sets[name]
	if !ok {
		return nil, fmt.Errorf("ClusterAddonProfile/%s not found", name)
	}
	for _, item := range stack {
		if item == name {
			return nil, fmt.Errorf("cycle detected: %s -> %s", strings.Join(stack, " -> "), name)
		}
	}
	stack = append(stack, name)
	var out []string
	for _, ref := range set.Spec.Profiles {
		names, err := expandSet(sets, ref.Name, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
	}
	for _, ref := range set.Spec.Addons {
		out = append(out, ref.Name)
	}
	return out, nil
}

func firstOccurrence(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range in {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func extensionIndex(state v1alpha1.State) map[string]v1alpha1.ClusterAddon {
	out := map[string]v1alpha1.ClusterAddon{}
	for _, extension := range state.ClusterAddons {
		out[extension.Metadata.Name] = extension
	}
	return out
}

func setIndex(state v1alpha1.State) map[string]v1alpha1.ClusterAddonProfile {
	out := map[string]v1alpha1.ClusterAddonProfile{}
	for _, set := range state.ClusterAddonProfiles {
		out[set.Metadata.Name] = set
	}
	return out
}
