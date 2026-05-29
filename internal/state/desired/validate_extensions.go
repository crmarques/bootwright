package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterExtensions(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]bool{}
	for _, extension := range state.ClusterExtensions {
		if e := validateName(v1alpha1.KindClusterExtension, extension.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[extension.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterExtension %q", extension.Metadata.Name))
		}
		seen[extension.Metadata.Name] = true
		prefix := fmt.Sprintf("ClusterExtension/%s spec", extension.Metadata.Name)
		switch extension.Spec.Type {
		case "":
			errs = append(errs, prefix+".type is required")
		case v1alpha1.ClusterExtensionTypeOLMOperator:
			if extension.Spec.ManifestSet != nil {
				errs = append(errs, prefix+".type=olm-operator must not set manifestSet")
			}
			errs = append(errs, validateClusterExtensionOLM(extension)...)
		case v1alpha1.ClusterExtensionTypeManifestSet:
			if extension.Spec.OLM != nil {
				errs = append(errs, prefix+".type=manifest-set must not set olm")
			}
			errs = append(errs, validateClusterExtensionManifestSet(extension)...)
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}",
				prefix, extension.Spec.Type, v1alpha1.ClusterExtensionTypeOLMOperator, v1alpha1.ClusterExtensionTypeManifestSet))
		}
		errs = append(errs, validateClusterExtensionReadiness(extension)...)
		errs = append(errs, validateClusterExtensionProvides(extension)...)
	}
	return errs
}

func validateClusterExtensionProvides(extension v1alpha1.ClusterExtension) []string {
	var errs []string
	seen := map[string]bool{}
	prefix := fmt.Sprintf("ClusterExtension/%s spec.provides", extension.Metadata.Name)
	for i, capability := range extension.Spec.Provides {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		switch capability {
		case v1alpha1.ClusterExtensionProvidesKubeVirt:
		case "":
			errs = append(errs, owner+" must not be empty")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s %q must be %q", owner, capability, v1alpha1.ClusterExtensionProvidesKubeVirt))
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

func validateClusterExtensionOLM(extension v1alpha1.ClusterExtension) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterExtension/%s spec.olm", extension.Metadata.Name)
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

func validateClusterExtensionManifestSet(extension v1alpha1.ClusterExtension) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterExtension/%s spec.manifestSet", extension.Metadata.Name)
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
			errs = append(errs, fmt.Sprintf("%s %q must be relative to the ClusterExtension file", owner, value))
			continue
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s %q must stay within the ClusterExtension file directory", owner, value))
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

func validateClusterExtensionReadiness(extension v1alpha1.ClusterExtension) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterExtension/%s spec.readiness", extension.Metadata.Name)
	if _, err := time.ParseDuration(extension.Spec.Readiness.Timeout); err != nil {
		errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, extension.Spec.Readiness.Timeout))
	}
	for i, check := range extension.Spec.Readiness.Checks {
		owner := fmt.Sprintf("%s.checks[%d]", prefix, i)
		switch check.Type {
		case v1alpha1.ClusterExtensionReadinessCSVSucceeded:
			if check.Namespace == "" {
				errs = append(errs, owner+".namespace is required")
			}
			if check.Subscription == "" {
				errs = append(errs, owner+".subscription is required")
			}
		case v1alpha1.ClusterExtensionReadinessCondition:
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
		case v1alpha1.ClusterExtensionReadinessResourceExists:
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
				v1alpha1.ClusterExtensionReadinessCSVSucceeded,
				v1alpha1.ClusterExtensionReadinessCondition,
				v1alpha1.ClusterExtensionReadinessResourceExists))
		}
	}
	return errs
}

func validateClusterExtensionSets(state v1alpha1.State) []string {
	var errs []string
	extensions := indexClusterExtensions(state.ClusterExtensions)
	sets := indexClusterExtensionSets(state.ClusterExtensionSets)
	seen := map[string]bool{}
	for _, set := range state.ClusterExtensionSets {
		if e := validateName(v1alpha1.KindClusterExtensionSet, set.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[set.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterExtensionSet %q", set.Metadata.Name))
		}
		seen[set.Metadata.Name] = true
		if len(set.Spec.ExtensionSets) == 0 && len(set.Spec.Extensions) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterExtensionSet/%s spec must include at least one of extensionSets or extensions", set.Metadata.Name))
		}
		for i, ref := range set.Spec.ExtensionSets {
			owner := fmt.Sprintf("ClusterExtensionSet/%s spec.extensionSets[%d].name", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterExtensionSet", owner, ref.Name))
			}
		}
		for i, ref := range set.Spec.Extensions {
			owner := fmt.Sprintf("ClusterExtensionSet/%s spec.extensions[%d].name", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := extensions[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterExtension", owner, ref.Name))
			}
		}
	}
	errs = append(errs, validateClusterExtensionSetCycles(state.ClusterExtensionSets)...)
	return errs
}

func validateClusterExtensionSetCycles(sets []v1alpha1.ClusterExtensionSet) []string {
	byName := map[string]v1alpha1.ClusterExtensionSet{}
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
			errs = append(errs, fmt.Sprintf("ClusterExtensionSet/%s spec.extensionSets creates cycle: %s", name, strings.Join(cycle, " -> ")))
			return
		}
		set, ok := byName[name]
		if !ok {
			return
		}
		visiting[name] = true
		stack = append(stack, name)
		for _, ref := range set.Spec.ExtensionSets {
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

func validateClusterExtensionBindings(state v1alpha1.State) []string {
	var errs []string
	clusters := indexContainerClusters(state.ContainerClusters)
	extensions := indexClusterExtensions(state.ClusterExtensions)
	sets := indexClusterExtensionSets(state.ClusterExtensionSets)
	seen := map[string]bool{}
	for _, binding := range state.ClusterExtensionBindings {
		if e := validateName(v1alpha1.KindClusterExtensionBinding, binding.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[binding.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterExtensionBinding %q", binding.Metadata.Name))
		}
		seen[binding.Metadata.Name] = true
		if len(binding.Spec.ClusterSelector.Names) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterExtensionBinding/%s spec.clusterSelector.names must include at least one ContainerCluster", binding.Metadata.Name))
		}
		selected := map[string]bool{}
		for i, name := range binding.Spec.ClusterSelector.Names {
			owner := fmt.Sprintf("ClusterExtensionBinding/%s spec.clusterSelector.names[%d]", binding.Metadata.Name, i)
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
		if phase := binding.Spec.ApplyAfter.Phase; phase != "" && phase != v1alpha1.ClusterExtensionApplyPhaseClusterInstalled {
			errs = append(errs, fmt.Sprintf("ClusterExtensionBinding/%s spec.applyAfter.phase %q must be %q",
				binding.Metadata.Name, phase, v1alpha1.ClusterExtensionApplyPhaseClusterInstalled))
		}
		if len(binding.Spec.ExtensionSets) == 0 && len(binding.Spec.Extensions) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterExtensionBinding/%s spec must include at least one of extensionSets or extensions", binding.Metadata.Name))
		}
		for i, ref := range binding.Spec.ExtensionSets {
			owner := fmt.Sprintf("ClusterExtensionBinding/%s spec.extensionSets[%d].name", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterExtensionSet", owner, ref.Name))
			}
		}
		for i, ref := range binding.Spec.Extensions {
			owner := fmt.Sprintf("ClusterExtensionBinding/%s spec.extensions[%d].name", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := extensions[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterExtension", owner, ref.Name))
			}
		}
		if binding.Spec.Policy.Prune {
			errs = append(errs, fmt.Sprintf("ClusterExtensionBinding/%s spec.policy.prune=true is not supported in MVP", binding.Metadata.Name))
		}
		if binding.Spec.Policy.ContinueOnError {
			errs = append(errs, fmt.Sprintf("ClusterExtensionBinding/%s spec.policy.continueOnError=true is not supported in MVP", binding.Metadata.Name))
		}
	}
	return errs
}

func providedClusterCapabilities(state v1alpha1.State) map[string]map[string]bool {
	extensions := indexClusterExtensions(state.ClusterExtensions)
	out := map[string]map[string]bool{}
	for _, binding := range state.ClusterExtensionBindings {
		names := bindingProvidedExtensionNames(state, binding)
		for _, cluster := range binding.Spec.ClusterSelector.Names {
			if out[cluster] == nil {
				out[cluster] = map[string]bool{}
			}
			for _, name := range names {
				extension, ok := extensions[name]
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

func bindingProvidedExtensionNames(state v1alpha1.State, binding v1alpha1.ClusterExtensionBinding) []string {
	sets := indexClusterExtensionSets(state.ClusterExtensionSets)
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
		for _, ref := range set.Spec.ExtensionSets {
			visitSet(ref.Name, nextStack)
		}
		for _, ref := range set.Spec.Extensions {
			if !seen[ref.Name] {
				seen[ref.Name] = true
				out = append(out, ref.Name)
			}
		}
	}
	for _, ref := range binding.Spec.ExtensionSets {
		visitSet(ref.Name, map[string]bool{})
	}
	for _, ref := range binding.Spec.Extensions {
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
