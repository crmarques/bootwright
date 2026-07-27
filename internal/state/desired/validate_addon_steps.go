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

func validateClusterAddonSteps(state v1alpha1.State, extension v1alpha1.ClusterAddon) []string {
	if len(extension.Spec.Steps) == 0 {
		return nil
	}
	var errs []string
	machines := indexMachines(state.Machines)
	containers := indexContainerClusters(state.ContainerClusters)
	storage := indexStorageClusters(state.StorageClusters)
	baseDir := filepath.Dir(extension.SourcePath)
	seen := map[string]bool{}
	for i, hook := range extension.Spec.Steps {
		prefix := fmt.Sprintf("ClusterAddon/%s spec.steps[%d]", extension.Metadata.Name, i)
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
			if hook.Target.BoundCluster != nil || hook.Target.FromInput != nil || hook.Target.Static != nil || hook.Target.Limit != "" {
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

func validateHookLifecycle(prefix string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonStep) []string {
	switch {
	case hook.Gates != "" && hook.Follows != "":
		return []string{fmt.Sprintf("%s must set exactly one of gates or follows, not both", prefix)}
	case hook.Gates == "" && hook.Follows == "":
		return []string{fmt.Sprintf("%s must set gates %v or follows %v", prefix, v1alpha1.ClusterAddonStepGates(), v1alpha1.ClusterAddonStepFollows())}
	}
	anchor, gating := v1alpha1.ClusterAddonStepAnchor(hook)
	if gating {
		if !slices.Contains(v1alpha1.ClusterAddonStepGates(), anchor) {
			return []string{fmt.Sprintf("%s.gates %q must be one of %v", prefix, anchor, v1alpha1.ClusterAddonStepGates())}
		}
		if hook.OnFailure == v1alpha1.PlaybookFailureContinue {
			return []string{fmt.Sprintf("%s.gates cannot be combined with onFailure continue: a gate that lets the add-on proceed on failure is not a gate", prefix)}
		}
		return nil
	}
	if !slices.Contains(v1alpha1.ClusterAddonStepFollows(), anchor) {
		return []string{fmt.Sprintf("%s.follows %q must be one of %v", prefix, anchor, v1alpha1.ClusterAddonStepFollows())}
	}
	if anchor == v1alpha1.ClusterAddonStepFollowsOperatorReady && extension.Spec.Type != v1alpha1.ClusterAddonTypeOLM {
		return []string{fmt.Sprintf("%s.follows operatorReady is only valid for spec.type=olm add-ons", prefix)}
	}
	return nil
}

func validateHookVocab(prefix string, hook v1alpha1.ClusterAddonStep) []string {
	var errs []string
	if hook.Run != "" && hook.Run != v1alpha1.PlaybookRunOnChange && hook.Run != v1alpha1.PlaybookRunAlways {
		errs = append(errs, fmt.Sprintf("%s.run %q must be %q or %q", prefix, hook.Run, v1alpha1.PlaybookRunOnChange, v1alpha1.PlaybookRunAlways))
	}
	if hook.OnFailure != "" && hook.OnFailure != v1alpha1.PlaybookFailureFail && hook.OnFailure != v1alpha1.PlaybookFailureContinue {
		errs = append(errs, fmt.Sprintf("%s.onFailure %q must be %q or %q", prefix, hook.OnFailure, v1alpha1.PlaybookFailureFail, v1alpha1.PlaybookFailureContinue))
	}
	if hook.Timeout != "" {
		if d, err := time.ParseDuration(hook.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("%s.timeout %q is not a valid duration", prefix, hook.Timeout))
		} else if d <= 0 {
			errs = append(errs, fmt.Sprintf("%s.timeout %q must be greater than 0", prefix, hook.Timeout))
		}
	}
	return errs
}

func validateHookTarget(prefix string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonStep, machines map[string]v1alpha1.Machine, containers map[string]v1alpha1.ContainerCluster, storage map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	target := hook.Target
	modes := 0
	if target.BoundCluster != nil {
		modes++
	}
	if target.FromInput != nil {
		modes++
	}
	if target.Static != nil {
		modes++
	}
	if modes == 0 {
		errs = append(errs, prefix+".target must select boundCluster, fromInput, or static")
	} else if modes > 1 {
		errs = append(errs, prefix+".target must set exactly one of boundCluster, fromInput, or static")
	}
	if target.Limit != "" && target.Limit != v1alpha1.ClusterAddonStepTargetLimitFirstReachable && target.Limit != v1alpha1.ClusterAddonStepTargetLimitAll {
		errs = append(errs, fmt.Sprintf("%s.target.limit %q must be %q or %q", prefix, target.Limit, v1alpha1.ClusterAddonStepTargetLimitFirstReachable, v1alpha1.ClusterAddonStepTargetLimitAll))
	}
	if target.Static != nil {
		for i, name := range target.Static.Clusters {
			if _, ok := containers[name]; ok {
				continue
			}
			if _, ok := storage[name]; ok {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s.target.static.clusters[%d] %q does not match any ContainerCluster or StorageCluster", prefix, i, name))
		}
		for i, name := range target.Static.Machines {
			machine, ok := machines[name]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.target.static.machines[%d] %q does not match any Machine", prefix, i, name))
				continue
			}
			if machine.Spec.Access.SSH == nil {
				errs = append(errs, fmt.Sprintf("%s.target.static.machines[%d] %q has no spec.access.ssh, so the hook has no way to reach it; target a Machine that configures SSH access", prefix, i, name))
			}
		}
		if len(target.Static.Clusters) == 0 && len(target.Static.Machines) == 0 {
			errs = append(errs, prefix+".target.static must include clusters or machines")
		}
	}
	if target.FromInput != nil {
		errs = append(errs, validateHookInputRef(prefix+".target.fromInput", extension, *target.FromInput)...)
	}
	return errs
}

func validateHookInputRef(prefix string, extension v1alpha1.ClusterAddon, from v1alpha1.ClusterAddonStepInputTarget) []string {
	accepted, ok := acceptedInputByName(extension, from.Input)
	if !ok {
		return []string{fmt.Sprintf("%s.input %q does not name a declared spec.accepts.inputs entry", prefix, from.Input)}
	}
	if accepted.ResourceRef == nil {
		return []string{fmt.Sprintf("%s.input %q must name a resourceRef input", prefix, from.Input)}
	}
	if !slices.Contains(hooks.SupportedTargetRefKinds(), accepted.ResourceRef.Kind) {
		return []string{fmt.Sprintf("%s.input %q resourceRef kind %q is not a supported target kind %v", prefix, from.Input, accepted.ResourceRef.Kind, hooks.SupportedTargetRefKinds())}
	}
	if accepted.ResourceRef.Kind == v1alpha1.KindStorageExport && !inputHasStorageExportAttachment(accepted) {
		return []string{fmt.Sprintf("%s targets a StorageExport through input %q, so that input must declare a storageExportAttachment effect; without it the export's Ceph cluster and nodes are not pulled into the add-on's apply scope and the hook fails at apply time with an unresolved-reference error", prefix, from.Input)}
	}
	return nil
}

func inputHasStorageExportAttachment(input v1alpha1.ClusterAddonAcceptedInput) bool {
	for _, effect := range input.Effects {
		if effect.StorageExportAttachment != nil {
			return true
		}
	}
	return false
}

func validateHookOutputs(prefix string, hook v1alpha1.ClusterAddonStep) []string {
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
		if output.Format != "" && output.Format != v1alpha1.ClusterAddonStepOutputFormatText && output.Format != v1alpha1.ClusterAddonStepOutputFormatJSON {
			errs = append(errs, fmt.Sprintf("%s.format %q must be %q or %q", owner, output.Format, v1alpha1.ClusterAddonStepOutputFormatText, v1alpha1.ClusterAddonStepOutputFormatJSON))
		}
	}
	return errs
}

func validateHookManifests(prefix, baseDir string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonStep) []string {
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
		if stray, err := hooks.StrayTokenScalars(raw); err == nil {
			for _, value := range stray {
				errs = append(errs, fmt.Sprintf("%s.path %q has a scalar %q that looks like a token but is not the entire value; a token must be the whole scalar (e.g. \"{{ output foo }}\", not embedded in other text) or it is applied as literal text", owner, manifest.Path, value))
			}
		}
		for _, token := range tokens {
			if tokenConsumesOutput(token) {
				consumesOutput = true
			}
			errs = append(errs, validateHookToken(owner, extension, hook, token, outputs, secretRefs)...)
		}
	}
	if consumesOutput && v1alpha1.ClusterAddonStepFailureMode(hook) == v1alpha1.PlaybookFailureContinue {
		errs = append(errs, prefix+" consumes a hook output in its manifests, so failureMode must be fail")
	}
	return errs
}

func tokenConsumesOutput(token hooks.Token) bool {
	return token.Kind == hooks.TokenOutput
}

func validateHookToken(owner string, extension v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonStep, token hooks.Token, outputs, secretRefs map[string]bool) []string {
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
		return validateHookInputToken(owner, extension, token)
	default:
		return []string{fmt.Sprintf("%s has unknown token kind %q", owner, token.Kind)}
	}
	return nil
}

func validateHookInputToken(owner string, extension v1alpha1.ClusterAddon, token hooks.Token) []string {
	accepted, ok := acceptedInputByName(extension, token.Arg)
	if !ok {
		return []string{fmt.Sprintf("%s references undeclared input %q", owner, token.Arg)}
	}
	if token.Kind == hooks.TokenExportDetails && (accepted.ResourceRef == nil || accepted.ResourceRef.Kind != v1alpha1.KindStorageExport) {
		return []string{fmt.Sprintf("%s exportDetails token must reference a StorageExport resourceRef input", owner)}
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
