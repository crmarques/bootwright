package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
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
		errs = append(errs, validateClusterAddonAccepts(extension)...)
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
		errs = append(errs, validateClusterAddonInputSchema(prefix+".schema", input.Schema)...)
		errs = append(errs, validateClusterAddonInputEffects(prefix+".effects", input.Effects)...)
		errs = append(errs, validateDataFoundationStorageInputSchema(extension.Metadata.Name, i, input)...)
	}
	return errs
}

func validateDataFoundationStorageInputSchema(addon string, index int, input v1alpha1.ClusterAddonAcceptedInput) []string {
	hasDataFoundationStorageEffect := false
	for _, effect := range input.Effects {
		if effect.Type == v1alpha1.ClusterAddonInputEffectStorageExportAttachment && effect.Provider == v1alpha1.ClusterAddonProvidesDataFoundation {
			hasDataFoundationStorageEffect = true
			break
		}
	}
	if !hasDataFoundationStorageEffect {
		return nil
	}
	var errs []string
	prefix := fmt.Sprintf("ClusterAddon/%s spec.accepts.inputs[%d].schema", addon, index)
	// The attachment machinery reads the binding value literally named
	// exportRef, so the schema must pin that exact property as a required
	// StorageExport reference.
	if property, ok := input.Schema.Properties["exportRef"]; !ok {
		errs = append(errs, prefix+".properties.exportRef is required for dataFoundation storage attachment inputs")
	} else if property.RefKind != v1alpha1.KindStorageExport {
		errs = append(errs, fmt.Sprintf("%s.properties.exportRef.refKind %q must be %q for dataFoundation storage attachment inputs", prefix, property.RefKind, v1alpha1.KindStorageExport))
	}
	requiresExportRef := false
	for _, name := range input.Schema.Required {
		if name == "exportRef" {
			requiresExportRef = true
			break
		}
	}
	if !requiresExportRef {
		errs = append(errs, fmt.Sprintf("%s.required must include %q for dataFoundation storage attachment inputs", prefix, "exportRef"))
	}
	for name := range input.Schema.Properties {
		if name != "exportRef" {
			errs = append(errs, fmt.Sprintf("%s.properties.%s is not supported for dataFoundation storage attachment inputs", prefix, name))
		}
	}
	return errs
}

func validateClusterAddonInputSchema(prefix string, schema v1alpha1.ClusterAddonInputSchema) []string {
	var errs []string
	switch schema.Type {
	case "", v1alpha1.ClusterAddonInputSchemaTypeObject:
	default:
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, schema.Type, v1alpha1.ClusterAddonInputSchemaTypeObject))
	}
	required := map[string]bool{}
	for i, name := range schema.Required {
		owner := fmt.Sprintf("%s.required[%d]", prefix, i)
		if name == "" {
			errs = append(errs, owner+" must not be empty")
		} else if required[name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
		} else if _, ok := schema.Properties[name]; !ok {
			errs = append(errs, fmt.Sprintf("%s %q is not declared in properties", owner, name))
		}
		required[name] = true
	}
	for name, property := range schema.Properties {
		owner := prefix + ".properties." + name
		if name == "" {
			errs = append(errs, prefix+".properties contains an empty property name")
		}
		if property.RefKind != "" {
			if !knownResourceKind(property.RefKind) {
				errs = append(errs, fmt.Sprintf("%s.refKind %q is not a known Bootwright kind", owner, property.RefKind))
			}
			if property.SecretRef {
				errs = append(errs, owner+" must not set both refKind and secretRef")
			}
			continue
		}
		if !property.SecretRef {
			errs = append(errs, owner+" must set refKind or secretRef")
		}
	}
	return errs
}

func validateClusterAddonInputEffects(prefix string, effects []v1alpha1.ClusterAddonInputEffect) []string {
	var errs []string
	for i, effect := range effects {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		switch effect.Type {
		case v1alpha1.ClusterAddonInputEffectStorageExportAttachment:
			if effect.Provider != v1alpha1.ClusterAddonProvidesDataFoundation {
				errs = append(errs, fmt.Sprintf("%s.provider %q must be %q when type is %q", owner, effect.Provider, v1alpha1.ClusterAddonProvidesDataFoundation, effect.Type))
			}
		case "":
			errs = append(errs, owner+".type is required")
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q is not supported", owner, effect.Type))
		}
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
	if _, err := time.ParseDuration(extension.Spec.Readiness.Timeout); err != nil {
		errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, extension.Spec.Readiness.Timeout))
	}
	for i, check := range extension.Spec.Readiness.Checks {
		owner := fmt.Sprintf("%s.checks[%d]", prefix, i)
		switch check.Type {
		case v1alpha1.ClusterAddonReadinessCSVSucceeded:
			if check.APIVersion != "" || check.Kind != "" || check.Name != "" || check.Condition != nil {
				errs = append(errs, owner+".type=csvSucceeded must not set apiVersion, kind, name, or condition")
			}
			if check.Namespace == "" {
				errs = append(errs, owner+".namespace is required")
			}
			if check.Subscription == "" {
				errs = append(errs, owner+".subscription is required")
			}
		case v1alpha1.ClusterAddonReadinessCondition:
			if check.Subscription != "" {
				errs = append(errs, owner+".subscription is only valid when type=csvSucceeded")
			}
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
			if check.Subscription != "" {
				errs = append(errs, owner+".subscription is only valid when type=csvSucceeded")
			}
			if check.Condition != nil {
				errs = append(errs, owner+".condition is only valid when type=condition")
			}
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
	loaded := selectedResourceKeys(state)
	seen := map[string]bool{}
	effectiveApplications := map[string]string{}
	for _, binding := range state.ClusterAddonBindings {
		if e := validateName(v1alpha1.KindClusterAddonBinding, binding.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[binding.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterAddonBinding %q", binding.Metadata.Name))
		}
		seen[binding.Metadata.Name] = true
		if binding.Spec.ClusterRef.Name == "" {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.clusterRef is required", binding.Metadata.Name))
		} else if _, ok := clusters[binding.Spec.ClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec.clusterRef %q does not match any ContainerCluster", binding.Metadata.Name, binding.Spec.ClusterRef.Name))
		}
		if len(binding.Spec.AddonProfiles) == 0 && len(binding.Spec.Addons) == 0 {
			errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s spec must include at least one of addonProfiles or addons", binding.Metadata.Name))
		}
		for i, ref := range binding.Spec.AddonProfiles {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addonProfiles[%d].name", binding.Metadata.Name, i)
			if ref.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := sets[ref.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddonProfile", owner, ref.Name))
			}
		}
		for i, addon := range binding.Spec.Addons {
			owner := fmt.Sprintf("ClusterAddonBinding/%s spec.addons[%d].name", binding.Metadata.Name, i)
			if addon.Name == "" {
				errs = append(errs, owner+" is required")
			} else if _, ok := addons[addon.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s %q does not match any ClusterAddon", owner, addon.Name))
			}
			inputNames := map[string]bool{}
			for j, input := range addon.Inputs {
				inputOwner := fmt.Sprintf("ClusterAddonBinding/%s spec.addons[%d].inputs[%d]", binding.Metadata.Name, i, j)
				if input.Name == "" {
					errs = append(errs, inputOwner+".name is required")
				} else if inputNames[input.Name] {
					errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", inputOwner, input.Name))
				}
				inputNames[input.Name] = true
			}
		}
		for _, addon := range addoninputs.EffectiveBindingAddons(state, binding) {
			if addon.Name == "" {
				continue
			}
			applicationKey := binding.Spec.ClusterRef.Name + "/" + addon.Name
			if previous := effectiveApplications[applicationKey]; previous != "" {
				errs = append(errs, fmt.Sprintf("ClusterAddonBinding/%s applies ClusterAddon/%s to ContainerCluster/%s, already applied by ClusterAddonBinding/%s", binding.Metadata.Name, addon.Name, binding.Spec.ClusterRef.Name, previous))
			} else {
				effectiveApplications[applicationKey] = binding.Metadata.Name
			}
			extension, ok := addons[addon.Name]
			if !ok {
				continue
			}
			accepted := map[string]v1alpha1.ClusterAddonAcceptedInput{}
			for _, input := range extension.Spec.Accepts.Inputs {
				accepted[input.Name] = input
			}
			effectiveInputNames := map[string]bool{}
			for i, input := range addon.Inputs {
				owner := fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s inputs[%d]", binding.Metadata.Name, addon.Name, i)
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
					errs = append(errs, fmt.Sprintf("%s.name %q is not declared by ClusterAddon/%s spec.accepts.inputs", owner, input.Name, addon.Name))
					continue
				}
				errs = append(errs, validateClusterAddonInputValues(owner+".values", input.Values, acceptedInput.Schema, loaded)...)
			}
		}
	}
	return errs
}

func validateClusterAddonInputValues(prefix string, values map[string]any, schema v1alpha1.ClusterAddonInputSchema, loaded map[resourceKey]bool) []string {
	var errs []string
	if schema.Type != "" && schema.Type != v1alpha1.ClusterAddonInputSchemaTypeObject {
		return errs
	}
	for _, name := range schema.Required {
		if _, ok := values[name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.%s is required", prefix, name))
		}
	}
	for name := range values {
		if _, ok := schema.Properties[name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.%s is not declared by the input schema", prefix, name))
		}
	}
	for name, property := range schema.Properties {
		raw, ok := values[name]
		if !ok {
			continue
		}
		owner := prefix + "." + name
		if property.RefKind != "" || property.SecretRef {
			nameValue, ok := raw.(string)
			if !ok {
				errs = append(errs, owner+" must be a plain name string")
				continue
			}
			if nameValue == "" {
				errs = append(errs, owner+" is required")
				continue
			}
			// secretRef values resolve against Environment spec.secrets in the
			// comprehensive secret walk; refKind values resolve here.
			if property.RefKind != "" && !loaded[resourceKey{kind: property.RefKind, name: nameValue}] {
				errs = append(errs, fmt.Sprintf("%s %q does not match any %s", owner, nameValue, property.RefKind))
			}
		}
	}
	return errs
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
			extension, ok := addons[item.Name]
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
