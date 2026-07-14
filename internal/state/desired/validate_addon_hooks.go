package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/hooks"
)

func validateClusterAddonHooks(state v1alpha1.State, extension v1alpha1.ClusterAddon) []string {
	if len(extension.Spec.Hooks) == 0 {
		return nil
	}
	var errs []string
	machines := indexMachines(state.Machines)
	containers := indexContainerClusters(state.ContainerClusters)
	storage := indexStorageClusters(state.StorageClusters)
	baseDir := filepath.Dir(extension.SourcePath)
	seen := map[string]bool{}
	for i, hook := range extension.Spec.Hooks {
		prefix := fmt.Sprintf("ClusterAddon/%s spec.hooks[%d]", extension.Metadata.Name, i)
		if hook.Name == "" {
			errs = append(errs, prefix+".name is required")
		} else if !provisioningTokenRe.MatchString(hook.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a valid name", prefix, hook.Name))
		} else if seen[hook.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", prefix, hook.Name))
		}
		seen[hook.Name] = true

		errs = append(errs, validateHookLifecycle(prefix, extension, hook)...)
		errs = append(errs, validateHookVocab(prefix, hook)...)
		if hook.Playbook == "" && len(hook.Manifests) == 0 {
			errs = append(errs, prefix+" must set at least one of playbook or manifests")
		}
		if hook.Playbook != "" {
			errs = append(errs, validateContainedFile(prefix+".playbook", baseDir, hook.Playbook, true)...)
			if !hookPathHasSegment(hook.Playbook, "playbooks") {
				errs = append(errs, fmt.Sprintf("%s.playbook %q must live under a playbooks/ directory so the loader does not parse it as desired state", prefix, hook.Playbook))
			}
			if hook.RolesPath != "" {
				errs = append(errs, validateContainedDir(prefix+".rolesPath", baseDir, hook.RolesPath)...)
			}
			if hook.CollectionsPath != "" {
				errs = append(errs, validateContainedDir(prefix+".collectionsPath", baseDir, hook.CollectionsPath)...)
			}
			errs = append(errs, validateHookTarget(prefix, extension, hook, machines, containers, storage)...)
		} else {
			if len(hook.Outputs) > 0 {
				errs = append(errs, prefix+".outputs requires a playbook (only a playbook run can produce outputs)")
			}
			if hook.Target.BoundCluster || hook.Target.FromInput != nil || len(hook.Target.Clusters) > 0 || len(hook.Target.Machines) > 0 || hook.Target.Limit != "" {
				errs = append(errs, prefix+".target requires a playbook (manifests always apply to the bound cluster)")
			}
		}
		errs = append(errs, validateHookOutputs(prefix, hook)...)
		errs = append(errs, validateHookManifests(prefix, baseDir, extension, hook)...)
	}
	return errs
}

func hookPathHasSegment(path, segment string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(filepath.Clean(path)), "/"), segment)
}

func validateHookLifecycle(prefix string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonHook) []string {
	if !slices.Contains(v1alpha1.ClusterAddonHookLifecycles(), hook.Lifecycle) {
		return []string{fmt.Sprintf("%s.lifecycle %q must be one of %v", prefix, hook.Lifecycle, v1alpha1.ClusterAddonHookLifecycles())}
	}
	if hook.Lifecycle == v1alpha1.ClusterAddonHookPostOperatorReady && extension.Spec.Type != v1alpha1.ClusterAddonTypeOLM {
		return []string{fmt.Sprintf("%s.lifecycle postOperatorReady is only valid for spec.type=olm add-ons", prefix)}
	}
	return nil
}

func validateHookVocab(prefix string, hook v1alpha1.ClusterAddonHook) []string {
	var errs []string
	if hook.Run != "" && hook.Run != v1alpha1.ProvisioningPlaybookRunOnChange && hook.Run != v1alpha1.ProvisioningPlaybookRunAlways {
		errs = append(errs, fmt.Sprintf("%s.run %q must be %q or %q", prefix, hook.Run, v1alpha1.ProvisioningPlaybookRunOnChange, v1alpha1.ProvisioningPlaybookRunAlways))
	}
	if hook.FailureMode != "" && hook.FailureMode != v1alpha1.ProvisioningPlaybookFailureFail && hook.FailureMode != v1alpha1.ProvisioningPlaybookFailureContinue {
		errs = append(errs, fmt.Sprintf("%s.failureMode %q must be %q or %q", prefix, hook.FailureMode, v1alpha1.ProvisioningPlaybookFailureFail, v1alpha1.ProvisioningPlaybookFailureContinue))
	}
	if hook.Timeout != "" {
		if _, err := time.ParseDuration(hook.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("%s.timeout %q is not a valid duration", prefix, hook.Timeout))
		}
	}
	return errs
}

func validateHookTarget(prefix string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonHook, machines map[string]v1alpha1.Machine, containers map[string]v1alpha1.ContainerCluster, storage map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	target := hook.Target
	modes := 0
	if target.BoundCluster {
		modes++
	}
	if target.FromInput != nil {
		modes++
	}
	if len(target.Clusters) > 0 || len(target.Machines) > 0 {
		modes++
	}
	if modes == 0 {
		errs = append(errs, prefix+".target must select boundCluster, fromInput, or a clusters/machines list")
	} else if modes > 1 {
		errs = append(errs, prefix+".target must set exactly one of boundCluster, fromInput, or the static lists")
	}
	if target.Limit != "" && target.Limit != v1alpha1.ClusterAddonHookTargetLimitFirstReachable && target.Limit != v1alpha1.ClusterAddonHookTargetLimitAll {
		errs = append(errs, fmt.Sprintf("%s.target.limit %q must be %q or %q", prefix, target.Limit, v1alpha1.ClusterAddonHookTargetLimitFirstReachable, v1alpha1.ClusterAddonHookTargetLimitAll))
	}
	for i, name := range target.Clusters {
		if _, ok := containers[name]; ok {
			continue
		}
		if _, ok := storage[name]; ok {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.target.clusters[%d] %q does not match any ContainerCluster or StorageCluster", prefix, i, name))
	}
	for i, name := range target.Machines {
		if _, ok := machines[name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.target.machines[%d] %q does not match any Machine", prefix, i, name))
		}
	}
	if target.FromInput != nil {
		errs = append(errs, validateHookInputRef(prefix+".target.fromInput", extension, *target.FromInput)...)
	}
	return errs
}

func validateHookInputRef(prefix string, extension v1alpha1.ClusterAddon, from v1alpha1.ClusterAddonHookInputTarget) []string {
	accepted, ok := acceptedInputByName(extension, from.Input)
	if !ok {
		return []string{fmt.Sprintf("%s.input %q does not name a declared spec.accepts.inputs entry", prefix, from.Input)}
	}
	property, ok := accepted.Schema.Properties[from.Property]
	if !ok || property.RefKind == "" {
		return []string{fmt.Sprintf("%s.property %q must name a refKind-typed schema property of input %q", prefix, from.Property, from.Input)}
	}
	if !slices.Contains(hooks.SupportedTargetRefKinds(), property.RefKind) {
		return []string{fmt.Sprintf("%s.property %q refKind %q is not a supported target kind %v", prefix, from.Property, property.RefKind, hooks.SupportedTargetRefKinds())}
	}
	if property.RefKind == v1alpha1.KindStorageExport && !inputHasStorageExportAttachment(accepted) {
		return []string{fmt.Sprintf("%s targets a StorageExport through input %q, so that input must declare a storageExportAttachment effect; without it the export's Ceph cluster and nodes are not pulled into the add-on's apply scope and the hook fails at apply time with an unresolved-reference error", prefix, from.Input)}
	}
	return nil
}

func inputHasStorageExportAttachment(input v1alpha1.ClusterAddonAcceptedInput) bool {
	for _, effect := range input.Effects {
		if effect.Type == v1alpha1.ClusterAddonInputEffectStorageExportAttachment {
			return true
		}
	}
	return false
}

func validateHookOutputs(prefix string, hook v1alpha1.ClusterAddonHook) []string {
	var errs []string
	seen := map[string]bool{}
	for i, output := range hook.Outputs {
		owner := fmt.Sprintf("%s.outputs[%d]", prefix, i)
		if output.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if !provisioningTokenRe.MatchString(output.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a valid name", owner, output.Name))
		} else if seen[output.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, output.Name))
		}
		seen[output.Name] = true
		if _, pathErrs := validateContainedPath(owner+".file", output.File); len(pathErrs) > 0 {
			errs = append(errs, pathErrs...)
		}
		if output.Format != "" && output.Format != v1alpha1.ClusterAddonHookOutputFormatText && output.Format != v1alpha1.ClusterAddonHookOutputFormatJSON {
			errs = append(errs, fmt.Sprintf("%s.format %q must be %q or %q", owner, output.Format, v1alpha1.ClusterAddonHookOutputFormatText, v1alpha1.ClusterAddonHookOutputFormatJSON))
		}
	}
	return errs
}

func validateHookManifests(prefix, baseDir string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonHook) []string {
	var errs []string
	outputs := map[string]bool{}
	for _, output := range hook.Outputs {
		outputs[output.Name] = true
	}
	secretRefs := map[string]bool{}
	for _, ref := range hook.SecretRefs {
		secretRefs[ref.Name] = true
	}
	consumesOutput := false
	for i, manifest := range hook.Manifests {
		owner := fmt.Sprintf("%s.manifests[%d]", prefix, i)
		fileErrs := validateContainedFile(owner+".path", baseDir, manifest.Path, true)
		errs = append(errs, fileErrs...)
		if len(fileErrs) > 0 {
			continue
		}
		if !hookPathHasSegment(manifest.Path, "manifests") {
			errs = append(errs, fmt.Sprintf("%s.path %q must live under a manifests/ directory so the loader does not parse it as desired state", owner, manifest.Path))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(baseDir, manifest.Path))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s.path %q could not be read: %v", owner, manifest.Path, err))
			continue
		}
		tokens, err := hooks.ExtractTokens(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s.path %q is not a valid manifest: %v", owner, manifest.Path, err))
			continue
		}
		for _, token := range tokens {
			if tokenConsumesOutput(token) {
				consumesOutput = true
			}
			errs = append(errs, validateHookToken(owner, extension, hook, token, outputs, secretRefs)...)
		}
	}
	if consumesOutput && v1alpha1.ClusterAddonHookFailureMode(hook) == v1alpha1.ProvisioningPlaybookFailureContinue {
		errs = append(errs, prefix+" consumes a hook output in its manifests, so failureMode must be fail")
	}
	return errs
}

func tokenConsumesOutput(token hooks.Token) bool {
	return token.Kind == hooks.TokenOutput
}

func validateHookToken(owner string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonHook, token hooks.Token, outputs, secretRefs map[string]bool) []string {
	switch token.Kind {
	case hooks.TokenCluster:
		return nil
	case hooks.TokenOutput:
		if !outputs[token.Arg] {
			return []string{fmt.Sprintf("%s references undeclared output %q", owner, token.Arg)}
		}
	case hooks.TokenSecret:
		if !secretRefs[token.Arg] {
			return []string{fmt.Sprintf("%s references secret %q not listed in the hook secretRefs", owner, token.Arg)}
		}
	case hooks.TokenInput, hooks.TokenExportDetails:
		return validateHookInputPropertyToken(owner, extension, token)
	default:
		return []string{fmt.Sprintf("%s has unknown token kind %q", owner, token.Kind)}
	}
	return nil
}

func validateHookInputPropertyToken(owner string, extension v1alpha1.ClusterAddon, token hooks.Token) []string {
	input, property, ok := hooks.SplitInputProperty(token.Arg)
	if !ok {
		return []string{fmt.Sprintf("%s token %q must reference input.property", owner, token.Arg)}
	}
	accepted, ok := acceptedInputByName(extension, input)
	if !ok {
		return []string{fmt.Sprintf("%s references undeclared input %q", owner, input)}
	}
	if _, ok := accepted.Schema.Properties[property]; !ok {
		return []string{fmt.Sprintf("%s references undeclared property %q of input %q", owner, property, input)}
	}
	return nil
}

func acceptedInputByName(extension v1alpha1.ClusterAddon, name string) (v1alpha1.ClusterAddonAcceptedInput, bool) {
	for _, input := range extension.Spec.Accepts.Inputs {
		if input.Name == name {
			return input, true
		}
	}
	return v1alpha1.ClusterAddonAcceptedInput{}, false
}
