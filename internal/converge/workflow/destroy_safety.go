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

func ProtectedKindSet(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, env := range state.Environments {
		for _, kind := range env.Spec.Safety.ProtectedKinds {
			out[kind] = true
		}
	}
	return out
}

func protectedKindsPresent(state v1alpha1.State) []string {
	protected := ProtectedKindSet(state)
	var present []string
	if protected[v1alpha1.KindContainerCluster] && len(state.ContainerClusters) > 0 {
		present = append(present, v1alpha1.KindContainerCluster)
	}
	if protected[v1alpha1.KindStorageCluster] && len(state.StorageClusters) > 0 {
		present = append(present, v1alpha1.KindStorageCluster)
	}
	if protected[v1alpha1.KindMachine] && len(state.Machines) > 0 {
		present = append(present, v1alpha1.KindMachine)
	}
	return present
}

func EvaluateDestroySafety(state v1alpha1.State, override bool) DestroySafetyDecision {
	var reasons []string
	for _, name := range ProtectedEnvironments(state) {
		reasons = append(reasons, fmt.Sprintf("Environment/%s spec.safety.destroyProtection=%s", name, v1alpha1.EnvironmentDestroyProtectionRequiredOverride))
	}
	for _, kind := range protectedKindsPresent(state) {
		reasons = append(reasons, fmt.Sprintf("spec.safety.protectedKinds includes %s and this teardown covers one", kind))
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
