package workflow

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type DestroySafetyDecision struct {
	RequiredOverride bool
	Reasons          []string
}

// ProtectedEnvironments returns the names of in-scope Environments whose
// spec.safety.destroyProtection is requiredOverride. It is the single source of
// truth for destroy protection, shared by the destroy gate and the apply gate
// that refuses destructive --override rebuilds on protected environments.
func ProtectedEnvironments(state v1alpha1.State) []string {
	var names []string
	for _, env := range state.Environments {
		if env.Spec.Safety.DestroyProtection != v1alpha1.EnvironmentDestroyProtectionRequiredOverride {
			continue
		}
		names = append(names, env.Metadata.Name)
	}
	return names
}

func EvaluateDestroySafety(state v1alpha1.State, override bool) DestroySafetyDecision {
	var reasons []string
	for _, name := range ProtectedEnvironments(state) {
		reasons = append(reasons, fmt.Sprintf("Environment/%s spec.safety.destroyProtection=%s", name, v1alpha1.EnvironmentDestroyProtectionRequiredOverride))
	}
	return DestroySafetyDecision{
		RequiredOverride: len(reasons) > 0 && !override,
		Reasons:          reasons,
	}
}

func (d DestroySafetyDecision) Summary() string {
	if len(d.Reasons) == 0 {
		return ""
	}
	return strings.Join(d.Reasons, "; ")
}
