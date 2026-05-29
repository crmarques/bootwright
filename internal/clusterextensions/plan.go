package clusterextensions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type BindingPlan struct {
	Binding    string
	Cluster    string
	Policy     v1alpha1.ClusterExtensionPolicy
	Extensions []ExtensionPlan
}

type ExtensionPlan struct {
	Name      string
	Binding   string
	Cluster   string
	Extension v1alpha1.ClusterExtension
	Policy    v1alpha1.ClusterExtensionPolicy
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
	for _, binding := range state.ClusterExtensionBindings {
		names, err := expandBindingExtensionNames(state, binding)
		if err != nil {
			return nil, err
		}
		for _, cluster := range binding.Spec.ClusterSelector.Names {
			plan := BindingPlan{
				Binding: binding.Metadata.Name,
				Cluster: cluster,
				Policy:  binding.Spec.Policy,
			}
			for _, name := range names {
				extension, ok := extensions[name]
				if !ok {
					return nil, fmt.Errorf("ClusterExtensionBinding/%s references missing ClusterExtension/%s", binding.Metadata.Name, name)
				}
				plan.Extensions = append(plan.Extensions, ExtensionPlan{
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

func ResourceSummaries(extension v1alpha1.ClusterExtension) []ResourceSummary {
	switch extension.Spec.Type {
	case v1alpha1.ClusterExtensionTypeOLMOperator:
		resources, _ := OLMResources(extension)
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
	case v1alpha1.ClusterExtensionTypeManifestSet:
		out := make([]ResourceSummary, 0, len(extension.Spec.ManifestSet.Manifests))
		for _, manifest := range extension.Spec.ManifestSet.Manifests {
			out = append(out, ResourceSummary{Kind: "Manifest", Name: manifest.Path, Source: "file"})
		}
		return out
	default:
		return nil
	}
}

func expandBindingExtensionNames(state v1alpha1.State, binding v1alpha1.ClusterExtensionBinding) ([]string, error) {
	var expanded []string
	for _, ref := range binding.Spec.ExtensionSets {
		names, err := ExpandSet(state, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("ClusterExtensionBinding/%s spec.extensionSets[%s]: %w", binding.Metadata.Name, ref.Name, err)
		}
		expanded = append(expanded, names...)
	}
	for _, ref := range binding.Spec.Extensions {
		expanded = append(expanded, ref.Name)
	}
	return firstOccurrence(expanded), nil
}

func ExpandSet(state v1alpha1.State, name string) ([]string, error) {
	sets := setIndex(state)
	return expandSet(sets, name, nil)
}

func expandSet(sets map[string]v1alpha1.ClusterExtensionSet, name string, stack []string) ([]string, error) {
	set, ok := sets[name]
	if !ok {
		return nil, fmt.Errorf("ClusterExtensionSet/%s not found", name)
	}
	for _, item := range stack {
		if item == name {
			return nil, fmt.Errorf("cycle detected: %s -> %s", strings.Join(stack, " -> "), name)
		}
	}
	stack = append(stack, name)
	var out []string
	for _, ref := range set.Spec.ExtensionSets {
		names, err := expandSet(sets, ref.Name, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
	}
	for _, ref := range set.Spec.Extensions {
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

func extensionIndex(state v1alpha1.State) map[string]v1alpha1.ClusterExtension {
	out := map[string]v1alpha1.ClusterExtension{}
	for _, extension := range state.ClusterExtensions {
		out[extension.Metadata.Name] = extension
	}
	return out
}

func setIndex(state v1alpha1.State) map[string]v1alpha1.ClusterExtensionSet {
	out := map[string]v1alpha1.ClusterExtensionSet{}
	for _, set := range state.ClusterExtensionSets {
		out[set.Metadata.Name] = set
	}
	return out
}
