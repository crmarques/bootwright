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

func EvaluateDestroySafety(state v1alpha1.State, override bool) DestroySafetyDecision {
	var reasons []string
	for _, env := range state.Environments {
		if env.Spec.Safety.DestroyProtection != v1alpha1.EnvironmentDestroyProtectionRequiredOverride {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("Environment/%s spec.safety.destroyProtection=%s", env.Metadata.Name, v1alpha1.EnvironmentDestroyProtectionRequiredOverride))
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
