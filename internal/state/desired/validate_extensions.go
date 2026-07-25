package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

var clusterAddonCapabilityRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateClusterAddons(state v1alpha1.State) []string {
	var errs []string
	for _, extension := range state.ClusterAddons {
		if e := validateName(v1alpha1.KindClusterAddon, extension.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("ClusterAddon/%s spec", extension.Metadata.Name)
		switch extension.Spec.Type {
		case "":
			errs = append(errs, prefix+".type is required")
		case v1alpha1.ClusterAddonTypeOLM:
			if extension.Spec.ManifestSet != nil {
				errs = append(errs, prefix+".type=olm must not set manifestSet")
			}
			errs = append(errs, validateClusterAddonOLM(extension)...)
		case v1alpha1.ClusterAddonTypeManifestSet:
			if extension.Spec.OLM != nil {
				errs = append(errs, prefix+".type=manifestSet must not set olm")
			}
			errs = append(errs, validateClusterAddonManifestSet(extension)...)
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}",
				prefix, extension.Spec.Type, v1alpha1.ClusterAddonTypeOLM, v1alpha1.ClusterAddonTypeManifestSet))
		}
		errs = append(errs, validateClusterAddonReadiness(extension)...)
		errs = append(errs, validateClusterAddonProvides(extension)...)
		errs = append(errs, validateClusterAddonRequires(extension)...)
		errs = append(errs, validateClusterAddonAccepts(extension)...)
		errs = append(errs, validateClusterAddonHooks(state, extension)...)
	}
	return errs
}

func validateClusterAddonAccepts(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	seen := map[string]bool{}
	for i, input := range extension.Spec.Accepts.Inputs {
		prefix := fmt.Sprintf("ClusterAddon/%s spec.accepts.inputs[%d]", extension.Metadata.Name, i)
		if input.Name == "" {
			errs = append(errs, prefix+".name is required")
		} else if seen[input.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", prefix, input.Name))
		}
		seen[input.Name] = true
		errs = append(errs, validateClusterAddonInputPresence(prefix, input)...)
		errs = append(errs, validateClusterAddonInputEffects(prefix+".effects", extension, input)...)
	}
	return errs
}

func validateClusterAddonInputPresence(prefix string, input v1alpha1.ClusterAddonAcceptedInput) []string {
	var errs []string
	arms := 0
	if input.ResourceRef != nil {
		arms++
		if input.ResourceRef.Kind == "" {
			errs = append(errs, prefix+".resourceRef.kind is required")
		} else if !knownResourceKind(input.ResourceRef.Kind) {
			errs = append(errs, fmt.Sprintf("%s.resourceRef.kind %q is not a known Bootwright kind", prefix, input.ResourceRef.Kind))
		}
	}
	if input.SecretRef != nil {
		arms++
	}
	if arms == 0 {
		errs = append(errs, prefix+" must set exactly one of resourceRef or secretRef")
	} else if arms > 1 {
		errs = append(errs, prefix+" must not set both resourceRef and secretRef")
	}
	return errs
}

func validateClusterAddonInputEffects(prefix string, extension v1alpha1.ClusterAddon, input v1alpha1.ClusterAddonAcceptedInput) []string {
	var errs []string
	for i, effect := range input.Effects {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		arms := 0
		if effect.StorageExportAttachment != nil {
			arms++
			if !clusterAddonProvides(extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
				errs = append(errs, owner+".storageExportAttachment requires the add-on to provide dataFoundation")
			}
			if input.ResourceRef == nil || input.ResourceRef.Kind != v1alpha1.KindStorageExport {
				errs = append(errs, owner+".storageExportAttachment requires a resourceRef input with kind StorageExport")
			}
		}
		if merge := effect.GlobalPullSecretMerge; merge != nil {
			arms++
			if input.SecretRef == nil {
				errs = append(errs, owner+".globalPullSecretMerge requires a secretRef input")
			}
			if merge.Registry == "" {
				errs = append(errs, owner+".globalPullSecretMerge.registry is required")
			}
			if merge.Username == "" {
				errs = append(errs, owner+".globalPullSecretMerge.username is required")
			}
		}
		if arms == 0 {
			errs = append(errs, owner+" must set exactly one of storageExportAttachment or globalPullSecretMerge")
		} else if arms > 1 {
			errs = append(errs, owner+" must not set more than one effect arm")
		}
	}
	return errs
}

func clusterAddonProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}

func clusterAddonCapabilityValid(capability string) bool {
	return clusterAddonCapabilityRe.MatchString(capability)
}

func validateClusterAddonProvides(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	seen := map[string]bool{}
	prefix := fmt.Sprintf("ClusterAddon/%s spec.provides", extension.Metadata.Name)
	for i, capability := range extension.Spec.Provides {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if capability == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if !clusterAddonCapabilityValid(capability) {
			errs = append(errs, fmt.Sprintf("%s %q must match ^[A-Za-z0-9][A-Za-z0-9._-]*$", owner, capability))
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

func validateClusterAddonRequires(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	seen := map[string]bool{}
	prefix := fmt.Sprintf("ClusterAddon/%s spec.requires", extension.Metadata.Name)
	for i, capability := range extension.Spec.Requires {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if capability == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if !clusterAddonCapabilityValid(capability) {
			errs = append(errs, fmt.Sprintf("%s %q must match ^[A-Za-z0-9][A-Za-z0-9._-]*$", owner, capability))
			continue
		}
		if seen[capability] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, capability))
			continue
		}
		seen[capability] = true
	}
	return errs
}

func validateClusterAddonOLM(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.olm", extension.Metadata.Name)
	if extension.Spec.OLM == nil {
		return []string{prefix + " is required when spec.type=olm"}
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
	if catalog := olm.CatalogSource; catalog != nil {
		if catalog.Name == "" {
			errs = append(errs, prefix+".catalogSource.name is required")
		}
		if catalog.Image == "" {
			errs = append(errs, prefix+".catalogSource.image is required")
		}
		if catalog.PollInterval != "" {
			if _, err := time.ParseDuration(catalog.PollInterval); err != nil {
				errs = append(errs, fmt.Sprintf("%s.catalogSource.pollInterval %q is not a valid duration", prefix, catalog.PollInterval))
			}
		}
		if podConfig := catalog.GRPCPodConfig; podConfig != nil {
			switch podConfig.SecurityContextConfig {
			case v1alpha1.CatalogSecurityContextLegacy, v1alpha1.CatalogSecurityContextRestricted:
			case "":
				errs = append(errs, prefix+".catalogSource.grpcPodConfig.securityContextConfig is required")
			default:
				errs = append(errs, fmt.Sprintf("%s.catalogSource.grpcPodConfig.securityContextConfig %q must be one of {%s, %s}",
					prefix, podConfig.SecurityContextConfig, v1alpha1.CatalogSecurityContextLegacy, v1alpha1.CatalogSecurityContextRestricted))
			}
		}
		if catalog.Name != "" && olm.Subscription.Source != "" && olm.Subscription.Source != catalog.Name {
			errs = append(errs, fmt.Sprintf("%s.subscription.source %q must match catalogSource.name %q", prefix, olm.Subscription.Source, catalog.Name))
		}
		if olm.Namespace.Create && olm.Subscription.SourceNamespace != "" && olm.Subscription.SourceNamespace == olm.Namespace.Name {
			errs = append(errs, fmt.Sprintf("%s.subscription.sourceNamespace %q must be a pre-existing namespace when catalogSource is set; the CatalogSource is applied and readiness-gated before the add-on creates %q", prefix, olm.Subscription.SourceNamespace, olm.Namespace.Name))
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
		if customResourceString(resource, "kind") == "Secret" {
			if _, ok := resource["data"]; ok {
				errs = append(errs, itemPrefix+" is a kind=Secret carrying inline data; desired state must not embed secret bytes — provide the Secret at apply time instead of inlining it")
			}
			if _, ok := resource["stringData"]; ok {
				errs = append(errs, itemPrefix+" is a kind=Secret carrying inline stringData; desired state must not embed secret bytes — provide the Secret at apply time instead of inlining it")
			}
		}
	}
	return errs
}

func validateClusterAddonManifestSet(extension v1alpha1.ClusterAddon) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.manifestSet", extension.Metadata.Name)
	if extension.Spec.ManifestSet == nil {
		return []string{prefix + " is required when spec.type=manifestSet"}
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
	if d, err := time.ParseDuration(extension.Spec.Readiness.Timeout); err != nil {
		errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, extension.Spec.Readiness.Timeout))
	} else if d <= 0 {
		errs = append(errs, fmt.Sprintf("%s.timeout %q must be greater than 0", prefix, extension.Spec.Readiness.Timeout))
	}
	for i, check := range extension.Spec.Readiness.Checks {
		owner := fmt.Sprintf("%s.checks[%d]", prefix, i)
		arms := 0
		if csv := check.CSVSucceeded; csv != nil {
			arms++
			if csv.Namespace == "" {
				errs = append(errs, owner+".csvSucceeded.namespace is required")
			}
			if csv.Subscription == "" {
				errs = append(errs, owner+".csvSucceeded.subscription is required")
			}
		}
		if condition := check.Condition; condition != nil {
			arms++
			if condition.APIVersion == "" {
				errs = append(errs, owner+".condition.apiVersion is required")
			}
			if condition.Kind == "" {
				errs = append(errs, owner+".condition.kind is required")
			}
			if condition.Name == "" {
				errs = append(errs, owner+".condition.name is required")
			}
			if condition.Condition.Type == "" {
				errs = append(errs, owner+".condition.condition.type is required")
			}
			if condition.Condition.Status == "" {
				errs = append(errs, owner+".condition.condition.status is required")
			}
		}
		if exists := check.ResourceExists; exists != nil {
			arms++
			if exists.APIVersion == "" {
				errs = append(errs, owner+".resourceExists.apiVersion is required")
			}
			if exists.Kind == "" {
				errs = append(errs, owner+".resourceExists.kind is required")
			}
			if exists.Name == "" {
				errs = append(errs, owner+".resourceExists.name is required")
			}
		}
		if arms == 0 {
			errs = append(errs, owner+" must set exactly one of csvSucceeded, condition, or resourceExists")
		} else if arms > 1 {
			errs = append(errs, owner+" must not set more than one readiness arm")
		}
	}
	return errs
}

func validateClusterAddonProfiles(state v1alpha1.State) []string {
	var errs []string
	addons := indexClusterAddons(state.ClusterAddons)
	sets := indexClusterAddonProfiles(state.ClusterAddonProfiles)
	for _, set := range state.ClusterAddonProfiles {
		if e := validateName(v1alpha1.KindClusterAddonProfile, set.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if len(set.Spec.ProfileRefs) == 0 && len(set.Spec.AddonRefs) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonProfile/%s spec must include at least one of profileRefs or addonRefs", set.Metadata.Name))
		}
		for i, ref := range set.Spec.ProfileRefs {
			owner := fmt.Sprintf("ClusterAddonProfile/%s spec.profileRefs[%d]", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddonProfile", owner, ref.Name))
			}
		}
		for i, ref := range set.Spec.AddonRefs {
			owner := fmt.Sprintf("ClusterAddonProfile/%s spec.addonRefs[%d]", set.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := addons[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon%s", owner, ref.Name, unresolvedAddonRemedy(ref.Name)))
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
			errs = append(errs, fmt.Sprintf("ClusterAddonProfile/%s spec.profileRefs creates cycle: %s", name, strings.Join(cycle, " -> ")))
			return
		}
		set, ok := byName[name]
		if !ok {
			return
		}
		visiting[name] = true
		stack = append(stack, name)
		for _, ref := range set.Spec.ProfileRefs {
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
	loaded := selectedResourceKeys(state)
	effectiveApplications := map[string]string{}
	for _, binding := range state.ClusterAddonBindings {
		if e := validateName(v1alpha1.KindClusterAddonBinding, binding.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if binding.Spec.ClusterRef.Name == "" {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.clusterRef is required", binding.Metadata.Name))
		} else if _, ok := clusters[binding.Spec.ClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.clusterRef %q does not match any ContainerCluster", binding.Metadata.Name, binding.Spec.ClusterRef.Name))
		}
		if len(binding.Spec.ProfileRefs) == 0 && len(binding.Spec.AddonRefs) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec must include at least one of profileRefs or addonRefs", binding.Metadata.Name))
		}
		for i, ref := range binding.Spec.ProfileRefs {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.profileRefs[%d]", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddonProfile", owner, ref.Name))
			}
		}
		selected := map[string]bool{}
		direct := map[string]bool{}
		for i, ref := range binding.Spec.AddonRefs {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addonRefs[%d]", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if direct[ref.Name] {
				errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, ref.Name))
			} else if _, ok := addons[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon%s", owner, ref.Name, unresolvedAddonRemedy(ref.Name)))
			}
			direct[ref.Name] = true
		}
		for _, addon := range addoninputs.EffectiveBindingAddons(state, binding) {
			selected[addon.AddonRef.Name] = true
		}
		configs := map[string]bool{}
		for i, config := range binding.Spec.AddonConfigs {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addonConfigs[%d].addonRef", binding.Metadata.Name, i)
			if config.AddonRef.Name == "" {
				errs = append(errs, owner+" is required")
			} else if configs[config.AddonRef.Name] {
				errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, config.AddonRef.Name))
			} else if _, ok := addons[config.AddonRef.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon%s", owner, config.AddonRef.Name, unresolvedAddonRemedy(config.AddonRef.Name)))
			} else if !selected[config.AddonRef.Name] {
				errs = append(errs, fmt.Sprintf("%s %q must already be selected by profileRefs or addonRefs", owner, config.AddonRef.Name))
			}
			configs[config.AddonRef.Name] = true
			errs = append(errs, validateClusterAddonConfigInputs(binding.Metadata.Name, i, config)...)
		}
		for _, addon := range addoninputs.EffectiveBindingAddons(state, binding) {
			if addon.AddonRef.Name == "" {
				continue
			}
			applicationKey := binding.Spec.ClusterRef.Name + "/" + addon.AddonRef.Name
			if previous := effectiveApplications[applicationKey]; previous != "" {
				errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s applies ClusterAddon/%s to ContainerCluster/%s, already applied by ClusterAddonBinding/%s", binding.Metadata.Name, addon.AddonRef.Name, binding.Spec.ClusterRef.Name, previous))
			} else {
				effectiveApplications[applicationKey] = binding.Metadata.Name
			}
			extension, ok := addons[addon.AddonRef.Name]
			if !ok {
				continue
			}
			accepted := map[string]v1alpha1.ClusterAddonAcceptedInput{}
			for _, input := range extension.Spec.Accepts.Inputs {
				accepted[input.Name] = input
			}
			effectiveInputNames := map[string]bool{}
			for i, input := range addon.Inputs {
				owner := fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s inputs[%d]", binding.Metadata.Name, addon.AddonRef.Name, i)
				acceptedInput, ok := accepted[input.Name]
				if input.Name == "" {
					continue
				}
				if effectiveInputNames[input.Name] {
					errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, input.Name))
					continue
				}
				effectiveInputNames[input.Name] = true
				if !ok {
					errs = append(errs, fmt.Sprintf("%s.name %q is not declared by ClusterAddon/%s spec.accepts.inputs", owner, input.Name, addon.AddonRef.Name))
					continue
				}
				errs = append(errs, validateClusterAddonInputValue(owner+".value", input.Value, acceptedInput, loaded)...)
			}
			for _, acceptedInput := range extension.Spec.Accepts.Inputs {
				if acceptedInput.Required && !effectiveInputNames[acceptedInput.Name] {
					errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s does not supply required input %q", binding.Metadata.Name, addon.AddonRef.Name, acceptedInput.Name))
				}
			}
		}
		errs = append(errs, validateBindingCapabilityOrdering(binding, addons, state)...)
	}
	return errs
}

func validateBindingCapabilityOrdering(binding v1alpha1.ClusterAddonBinding, addons map[string]v1alpha1.ClusterAddon, state v1alpha1.State) []string {
	var errs []string
	var names []string
	var provides, requires [][]string
	for _, item := range addoninputs.EffectiveBindingAddons(state, binding) {
		ext, ok := addons[item.AddonRef.Name]
		if !ok {
			continue
		}
		names = append(names, item.AddonRef.Name)
		provides = append(provides, ext.Spec.Provides)
		requires = append(requires, ext.Spec.Requires)
	}
	providedBy := map[string][]int{}
	for i, caps := range provides {
		for _, capability := range caps {
			providedBy[capability] = append(providedBy[capability], i)
		}
	}
	indegree := make([]int, len(names))
	successors := make([][]int, len(names))
	for r, caps := range requires {
		linked := map[int]bool{}
		seen := map[string]bool{}
		for _, capability := range caps {
			if capability == "" || !clusterAddonCapabilityValid(capability) || seen[capability] {
				continue
			}
			seen[capability] = true
			if len(providedBy[capability]) == 0 {
				errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s requires capability %q but no add-on in this binding provides it", binding.Metadata.Name, names[r], capability))
				continue
			}
			for _, p := range providedBy[capability] {
				if p == r || linked[p] {
					continue
				}
				linked[p] = true
				successors[p] = append(successors[p], r)
				indegree[r]++
			}
		}
	}
	emitted := make([]bool, len(names))
	drained := 0
	for {
		progressed := false
		for i := range names {
			if emitted[i] || indegree[i] != 0 {
				continue
			}
			emitted[i] = true
			drained++
			for _, r := range successors[i] {
				indegree[r]--
			}
			progressed = true
		}
		if !progressed {
			break
		}
	}
	if drained < len(names) {
		errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s has a ClusterAddon spec.requires/spec.provides cycle", binding.Metadata.Name))
	}
	return errs
}

func validateClusterAddonConfigInputs(binding string, configIndex int, config v1alpha1.ClusterAddonBindingAddonConfig) []string {
	var errs []string
	inputNames := map[string]bool{}
	for j, input := range config.Inputs {
		owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addonConfigs[%d].inputs[%d]", binding, configIndex, j)
		if input.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if inputNames[input.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, input.Name))
		}
		inputNames[input.Name] = true
		if input.Value == "" {
			errs = append(errs, owner+".value is required")
		}
	}
	return errs
}

func validateClusterAddonInputValue(owner, value string, accepted v1alpha1.ClusterAddonAcceptedInput, loaded map[resourceKey]bool) []string {
	if value == "" {
		return []string{owner + " is required"}
	}
	if accepted.ResourceRef != nil && !loaded[resourceKey{kind: accepted.ResourceRef.Kind, name: value}] {
		return []string{fmt.Sprintf("%s %q does not match any %s", owner, value, accepted.ResourceRef.Kind)}
	}
	return nil
}

func providedClusterCapabilities(state v1alpha1.State) map[string]map[string]bool {
	addons := indexClusterAddons(state.ClusterAddons)
	out := map[string]map[string]bool{}
	for _, binding := range state.ClusterAddonBindings {
		cluster := binding.Spec.ClusterRef.Name
		if cluster == "" {
			continue
		}
		if out[cluster] == nil {
			out[cluster] = map[string]bool{}
		}
		for _, item := range addoninputs.EffectiveBindingAddons(state, binding) {
			extension, ok := addons[item.AddonRef.Name]
			if !ok {
				continue
			}
			for _, capability := range extension.Spec.Provides {
				out[cluster][capability] = true
			}
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
