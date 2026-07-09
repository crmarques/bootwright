package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

type BindingPlan struct {
	Binding string
	Cluster string
	Policy  addons.ClusterAddonPolicy
	Addons  []ExtensionPlan
}

type ExtensionPlan struct {
	Name      string
	Binding   string
	Cluster   string
	Extension v1alpha1.ClusterAddon
	Policy    addons.ClusterAddonPolicy
	// Inputs are the binding-supplied values for this add-on. They are part of
	// the add-on's desired state (hooks and effects resolve against them), so
	// the desired hash folds them in — editing an input re-applies an
	// otherwise-ready add-on.
	Inputs []v1alpha1.ClusterAddonBindingInput
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
		names, err := expandBindingExtensionNames(state, binding)
		if err != nil {
			return nil, err
		}
		cluster := binding.Spec.ClusterRef.Name
		plan := BindingPlan{
			Binding: binding.Metadata.Name,
			Cluster: cluster,
			Policy:  addons.DefaultPolicy(),
		}
		// Appending mirrors inputs.EffectiveBindingAddons, which merges duplicate
		// addonRef entries — the hash must see the same input list the executor
		// resolves.
		inputsByName := map[string][]v1alpha1.ClusterAddonBindingInput{}
		for _, addon := range binding.Spec.Addons {
			if addon.AddonRef.Name != "" {
				inputsByName[addon.AddonRef.Name] = append(inputsByName[addon.AddonRef.Name], addon.Inputs...)
			}
		}
		for _, name := range names {
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
				Inputs:    inputsByName[name],
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

// orderByCapabilities returns the add-ons stably reordered so each add-on
// declaring spec.requires comes after the in-binding add-ons that provide those
// capabilities. It is a stable topological sort: add-ons with no requires/
// provides edge between them keep their original (binding/profile-expansion)
// order — only a requirement that would otherwise resolve too late forces a
// move. A requires whose capability no add-on in the binding provides imposes
// no edge here (validation reports the unsatisfied requirement separately); a
// requires/provides cycle is an error.
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
	// Kahn's algorithm, draining ready nodes in original index order so the
	// result is deterministic and preserves binding order for independent add-ons.
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

func expandBindingExtensionNames(state v1alpha1.State, binding v1alpha1.ClusterAddonBinding) ([]string, error) {
	var expanded []string
	for _, ref := range binding.Spec.AddonProfileRefs {
		names, err := ExpandSet(state, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("ClusterAddonBinding/%s spec.addonProfileRefs[%s]: %w", binding.Metadata.Name, ref.Name, err)
		}
		expanded = append(expanded, names...)
	}
	for _, addon := range binding.Spec.Addons {
		if contains(expanded, addon.AddonRef.Name) {
			continue
		}
		expanded = append(expanded, addon.AddonRef.Name)
	}
	return firstOccurrence(expanded), nil
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
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
	for _, ref := range set.Spec.ProfileRefs {
		names, err := expandSet(sets, ref.Name, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
	}
	for _, ref := range set.Spec.AddonRefs {
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
