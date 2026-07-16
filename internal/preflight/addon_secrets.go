package preflight

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/secrets"
)

func addonSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	seen := map[string]bool{}
	for _, effective := range addoninputs.EffectiveAddons(state) {
		accepted := map[string]v1alpha1.ClusterAddonAcceptedInput{}
		for _, in := range effective.Extension.Spec.Accepts.Inputs {
			accepted[in.Name] = in
		}
		for _, input := range effective.Addon.Inputs {
			acc, ok := accepted[input.Name]
			if !ok || acc.SecretRef == nil || input.Value == "" || seen[input.Value] {
				continue
			}
			seen[input.Value] = true
			out = append(out, secretRefRequirement{
				refName: input.Value,
				label:   fmt.Sprintf("add-on %s input %s secretRef", effective.Addon.AddonRef.Name, input.Name),
				phases:  []string{"add-ons"},
			})
		}
	}
	return out
}

func AddonSecretChecks(state v1alpha1.State, secretsDir string, deps Deps) []Check {
	requirements := resolveSecretRequirementSources(state, addonSecretRefRequirements(state))
	if len(requirements) == 0 {
		return nil
	}
	needsSecretsDir := false
	for _, req := range requirements {
		if req.source == secretRefSourceGenerated || req.source == secretRefSourceContext {
			needsSecretsDir = true
			break
		}
	}
	idx := secret.NewIndex(state)
	var checks []Check
	if needsSecretsDir {
		checks = append(checks, secretsDirCheck(secretsDir, deps))
	}
	for _, req := range requirements {
		checks = append(checks, secretRequirementCheck(req, idx, secretsDir, deps)...)
	}
	return checks
}
