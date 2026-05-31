package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterAddons(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]bool{}
	for _, extension := range state.ClusterAddons {
		if e := validateName(v1alpha1.KindClusterAddon, extension.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[extension.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterAddon %q", extension.Metadata.Name))
		}
		seen[extension.Metadata.Name] = true
		prefix := fmt.Sprintf("ClusterAddon/%s spec", extension.Metadata.Name)
		switch extension.Spec.Type {
		case "":
			errs = append(errs, prefix+".type is required")
		case v1alpha1.ClusterAddonTypeOLMOperator:
			if extension.Spec.ManifestSet != nil {
				errs = append(errs, prefix+".type=olm-operator must not set manifestSet")
			}
			errs = append(errs, validateClusterAddonOLM(extension)...)
		case v1alpha1.ClusterAddonTypeManifestSet:
			if extension.Spec.OLM != nil {
				errs = append(errs, prefix+".type=manifest-set must not set olm")
			}
			errs = append(errs, validateClusterAddonManifestSet(extension)...)
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}",
				prefix, extension.Spec.Type, v1alpha1.ClusterAddonTypeOLMOperator, v1alpha1.ClusterAddonTypeManifestSet))
		}
		errs = append(errs, validateClusterAddonReadiness(extension)...)
		errs = append(errs, validateClusterAddonProvides(extension)...)
	}
	return errs
}

func validateClusterAddonProvides(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	seen := map[string]bool{}
	prefix := fmt.Sprintf("ClusterAddon/%s spec.provides", extension.Metadata.Name)
	for i, capability := range extension.Spec.Provides {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		switch capability {
		case v1alpha1.ClusterAddonProvidesKubeVirt, v1alpha1.ClusterAddonProvidesDataFoundation:
		case "":
			errs = append(errs, owner+" must not be empty")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s %q must be one of {%s, %s}", owner, capability, v1alpha1.ClusterAddonProvidesKubeVirt, v1alpha1.ClusterAddonProvidesDataFoundation))
			continue
		}
		if seen[capability] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, capability))
			continue
		}
		seen[capability] = true
	}
	if len(extension.Spec.Provides) > 0 && len(extension.Spec.Readiness.Checks) == 0 {
		errs = append(errs, fmt.Sprintf("%s requires at least one readiness check", prefix))
	}
	return errs
}

func validateClusterAddonOLM(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.olm", extension.Metadata.Name)
	if extension.Spec.OLM == nil {
		return []string{prefix + " is required when spec.type=olm-operator"}
	}
	olm := extension.Spec.OLM
	if olm.Namespace.Name == "" {
		errs = append(errs, prefix+".namespace.name is required")
	}
	if group := olm.OperatorGroup; group != nil {
		if group.Name == "" {
			errs = append(errs, prefix+".operatorGroup.name is required")
		}
		for i, name := range group.TargetNamespaces {
			if name == "" {
				errs = append(errs, fmt.Sprintf("%s.operatorGroup.targetNamespaces[%d] must not be empty", prefix, i))
			}
		}
	}
	sub := olm.Subscription
	required := []struct {
		field string
		value string
	}{
		{"name", sub.Name},
		{"package", sub.Package},
		{"channel", sub.Channel},
		{"source", sub.Source},
		{"sourceNamespace", sub.SourceNamespace},
		{"installPlanApproval", sub.InstallPlanApproval},
	}
	for _, req := range required {
		if req.value == "" {
			errs = append(errs, fmt.Sprintf("%s.subscription.%s is required", prefix, req.field))
		}
	}
	switch sub.InstallPlanApproval {
	case "", v1alpha1.InstallPlanApprovalAutomatic, v1alpha1.InstallPlanApprovalManual:
	default:
		errs = append(errs, fmt.Sprintf("%s.subscription.installPlanApproval %q must be one of {%s, %s}",
			prefix, sub.InstallPlanApproval, v1alpha1.InstallPlanApprovalAutomatic, v1alpha1.InstallPlanApprovalManual))
	}
	for i, resource := range olm.CustomResources {
		itemPrefix := fmt.Sprintf("%s.customResources[%d]", prefix, i)
		if customResourceString(resource, "apiVersion") == "" {
			errs = append(errs, itemPrefix+".apiVersion is required")
		}
		if customResourceString(resource, "kind") == "" {
			errs = append(errs, itemPrefix+".kind is required")
		}
		metadata, ok := customResourceMap(resource, "metadata")
		if !ok {
			errs = append(errs, itemPrefix+".metadata is required")
			continue
		}
		if customResourceString(metadata, "name") == "" {
			errs = append(errs, itemPrefix+".metadata.name is required")
		}
		if customResourceString(metadata, "namespace") == "" {
			errs = append(errs, itemPrefix+".metadata.namespace is required in MVP")
		}
	}
	return errs
}

func validateClusterAddonManifestSet(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.manifestSet", extension.Metadata.Name)
	if extension.Spec.ManifestSet == nil {
		return []string{prefix + " is required when spec.type=manifest-set"}
	}
	manifests := extension.Spec.ManifestSet.Manifests
	if len(manifests) == 0 {
		return []string{prefix + ".manifests must include at least one manifest"}
	}
	baseDir := filepath.Dir(extension.SourcePath)
	for i, manifest := range manifests {
		owner := fmt.Sprintf("%s.manifests[%d].path", prefix, i)
		value := manifest.Path
		if strings.TrimSpace(value) == "" {
			errs = append(errs, owner+" is required")
			continue
		}
		if strings.TrimSpace(value) != value {
			errs = append(errs, fmt.Sprintf("%s %q must not contain leading or trailing whitespace", owner, value))
			continue
		}
		if filepath.IsAbs(value) {
			errs = append(errs, fmt.Sprintf("%s %q must be relative to the ClusterAddon file", owner, value))
			continue
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s %q must stay within the ClusterAddon file directory", owner, value))
			continue
		}
		if !isYAMLFile(clean) {
			errs = append(errs, fmt.Sprintf("%s %q is not a .yaml or .yml file", owner, value))
			continue
		}
		path := filepath.Join(baseDir, clean)
		info, err := os.Lstat(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s %q does not exist: %v", owner, value, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Sprintf("%s %q must not be a symlink", owner, value))
			continue
		}
		if info.IsDir() {
			errs = append(errs, fmt.Sprintf("%s %q must name a manifest file, got directory", owner, value))
		}
	}
	return errs
}

func validateClusterAddonReadiness(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.readiness", extension.Metadata.Name)
	if _, err := time.ParseDuration(extension.Spec.Readiness.Timeout); err != nil {
		errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, extension.Spec.Readiness.Timeout))
	}
	for i, check := range extension.Spec.Readiness.Checks {
		owner := fmt.Sprintf("%s.checks[%d]", prefix, i)
		switch check.Type {
		case v1alpha1.ClusterAddonReadinessCSVSucceeded:
			if check.Namespace == "" {
				errs = append(errs, owner+".namespace is required")
			}
			if check.Subscription == "" {
				errs = append(errs, owner+".subscription is required")
			}
		case v1alpha1.ClusterAddonReadinessCondition:
			if check.APIVersion == "" {
				errs = append(errs, owner+".apiVersion is required")
			}
			if check.Kind == "" {
				errs = append(errs, owner+".kind is required")
			}
			if check.Name == "" {
				errs = append(errs, owner+".name is required")
			}
			if check.Condition == nil {
				errs = append(errs, owner+".condition is required")
				continue
			}
			if check.Condition.Type == "" {
				errs = append(errs, owner+".condition.type is required")
			}
			if check.Condition.Status == "" {
				errs = append(errs, owner+".condition.status is required")
			}
		case v1alpha1.ClusterAddonReadinessResourceExists:
			if check.APIVersion == "" {
				errs = append(errs, owner+".apiVersion is required")
			}
			if check.Kind == "" {
				errs = append(errs, owner+".kind is required")
			}
			if check.Name == "" {
				errs = append(errs, owner+".name is required")
			}
		case "":
			errs = append(errs, owner+".type is required")
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s, %s}",
				owner, check.Type,
				v1alpha1.ClusterAddonReadinessCSVSucceeded,
				v1alpha1.ClusterAddonReadinessCondition,
				v1alpha1.ClusterAddonReadinessResourceExists))
		}
	}
	return errs
}

func validateClusterAddonProfiles(state v1alpha1.State) []string {
	var errs []string
	addons := indexClusterAddons(state.ClusterAddons)
	sets := indexClusterAddonProfiles(state.ClusterAddonProfiles)
	seen := map[string]bool{}
	for _, set := range state.ClusterAddonProfiles {
		if e := validateName(v1alpha1.KindClusterAddonProfile, set.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[set.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterAddonProfile %q", set.Metadata.Name))
		}
		seen[set.Metadata.Name] = true
		if len(set.Spec.Profiles) == 0 && len(set.Spec.Addons) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonProfile/%s spec must include at least one of profiles or addons", set.Metadata.Name))
		}
		for i, ref := range set.Spec.Profiles {
			owner := fmt.Sprintf("ClusterAddonProfile/%s spec.profiles[%d].name", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddonProfile", owner, ref.Name))
			}
		}
		for i, ref := range set.Spec.Addons {
			owner := fmt.Sprintf("ClusterAddonProfile/%s spec.addons[%d].name", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := addons[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon", owner, ref.Name))
			}
		}
	}
	errs = append(errs, validateClusterAddonProfileCycles(state.ClusterAddonProfiles)...)
	return errs
}

func validateClusterAddonProfileCycles(sets []v1alpha1.ClusterAddonProfile) []string {
	byName := map[string]v1alpha1.ClusterAddonProfile{}
	for _, set := range sets {
		byName[set.Metadata.Name] = set
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var errs []string
	var visit func(string, []string)
	visit = func(name string, stack []string) {
		if visited[name] {
			return
		}
		if visiting[name] {
			cycle := append(stack, name)
			errs = append(errs, fmt.Sprintf("ClusterAddonProfile/%s spec.profiles creates cycle: %s", name, strings.Join(cycle, " -> ")))
			return
		}
		set, ok := byName[name]
		if !ok {
			return
		}
		visiting[name] = true
		stack = append(stack, name)
		for _, ref := range set.Spec.Profiles {
			visit(ref.Name, stack)
		}
		visiting[name] = false
		visited[name] = true
	}
	for _, set := range sets {
		visit(set.Metadata.Name, nil)
	}
	return errs
}

func validateClusterAddonBindings(state v1alpha1.State) []string {
	var errs []string
	clusters := indexContainerClusters(state.ContainerClusters)
	addons := indexClusterAddons(state.ClusterAddons)
	sets := indexClusterAddonProfiles(state.ClusterAddonProfiles)
	seen := map[string]bool{}
	for _, binding := range state.ClusterAddonBindings {
		if e := validateName(v1alpha1.KindClusterAddonBinding, binding.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[binding.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterAddonBinding %q", binding.Metadata.Name))
		}
		seen[binding.Metadata.Name] = true
		if len(binding.Spec.ContainerClusterSelector.Names) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.containerClusterSelector.names must include at least one ContainerCluster", binding.Metadata.Name))
		}
		selected := map[string]bool{}
		for i, name := range binding.Spec.ContainerClusterSelector.Names {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.containerClusterSelector.names[%d]", binding.Metadata.Name, i)
			if name == "" {
				errs = append(errs, owner+" must not be empty")
				continue
			}
			if selected[name] {
				errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
				continue
			}
			selected[name] = true
			if _, ok := clusters[name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ContainerCluster", owner, name))
			}
		}
		if phase := binding.Spec.ApplyAfter.Phase; phase != "" && phase != v1alpha1.ClusterAddonApplyPhaseContainerClusterInstalled {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.applyAfter.phase %q must be %q",
				binding.Metadata.Name, phase, v1alpha1.ClusterAddonApplyPhaseContainerClusterInstalled))
		}
		if len(binding.Spec.Profiles) == 0 && len(binding.Spec.Addons) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec must include at least one of profiles or addons", binding.Metadata.Name))
		}
		for i, ref := range binding.Spec.Profiles {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.profiles[%d].name", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddonProfile", owner, ref.Name))
			}
		}
		for i, ref := range binding.Spec.Addons {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addons[%d].name", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := addons[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon", owner, ref.Name))
			}
		}
		if binding.Spec.Policy.Prune {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.policy.prune=true is not supported in MVP", binding.Metadata.Name))
		}
		if binding.Spec.Policy.ContinueOnError {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.policy.continueOnError=true is not supported in MVP", binding.Metadata.Name))
		}
	}
	return errs
}

func providedClusterCapabilities(state v1alpha1.State) map[string]map[string]bool {
	addons := indexClusterAddons(state.ClusterAddons)
	out := map[string]map[string]bool{}
	for _, binding := range state.ClusterAddonBindings {
		names := bindingProvidedExtensionNames(state, binding)
		for _, cluster := range binding.Spec.ContainerClusterSelector.Names {
			if out[cluster] == nil {
				out[cluster] = map[string]bool{}
			}
			for _, name := range names {
				extension, ok := addons[name]
				if !ok {
					continue
				}
				for _, capability := range extension.Spec.Provides {
					out[cluster][capability] = true
				}
			}
		}
	}
	return out
}

func bindingProvidedExtensionNames(state v1alpha1.State, binding v1alpha1.ClusterAddonBinding) []string {
	sets := indexClusterAddonProfiles(state.ClusterAddonProfiles)
	seen := map[string]bool{}
	var out []string
	var visitSet func(string, map[string]bool)
	visitSet = func(name string, stack map[string]bool) {
		if stack[name] {
			return
		}
		set, ok := sets[name]
		if !ok {
			return
		}
		nextStack := map[string]bool{}
		for key, value := range stack {
			nextStack[key] = value
		}
		nextStack[name] = true
		for _, ref := range set.Spec.Profiles {
			visitSet(ref.Name, nextStack)
		}
		for _, ref := range set.Spec.Addons {
			if !seen[ref.Name] {
				seen[ref.Name] = true
				out = append(out, ref.Name)
			}
		}
	}
	for _, ref := range binding.Spec.Profiles {
		visitSet(ref.Name, map[string]bool{})
	}
	for _, ref := range binding.Spec.Addons {
		if !seen[ref.Name] {
			seen[ref.Name] = true
			out = append(out, ref.Name)
		}
	}
	return out
}

func customResourceMap(in map[string]any, key string) (map[string]any, bool) {
	value, ok := in[key]
	if !ok {
		return nil, false
	}
	out, ok := value.(map[string]any)
	return out, ok
}

func customResourceString(in map[string]any, key string) string {
	value, ok := in[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}
