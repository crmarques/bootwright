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

type DestroySafetyScope struct {
	TearsMachines bool
	TearsClusters bool
}

func protectedKindsPresent(state v1alpha1.State, storageWorkNames []string, scope DestroySafetyScope) []string {
	protected := ProtectedKindSet(state)
	var present []string
	if scope.TearsClusters && protected[v1alpha1.KindContainerCluster] && len(state.ContainerClusters) > 0 {
		present = append(present, v1alpha1.KindContainerCluster)
	}
	if (scope.TearsClusters || scope.TearsMachines) && protected[v1alpha1.KindStorageCluster] && storageInTeardown(state, storageWorkNames) {
		present = append(present, v1alpha1.KindStorageCluster)
	}
	if scope.TearsMachines && protected[v1alpha1.KindMachine] && len(state.Machines) > 0 {
		present = append(present, v1alpha1.KindMachine)
	}
	return present
}

func storageInTeardown(state v1alpha1.State, storageWorkNames []string) bool {
	if storageWorkNames == nil {
		return len(state.StorageClusters) > 0
	}
	return len(storageWorkNames) > 0
}

func EvaluateDestroySafety(state v1alpha1.State, override bool, storageWorkNames []string, scope DestroySafetyScope) DestroySafetyDecision {
	var reasons []string
	for _, name := range ProtectedEnvironments(state) {
		reasons = append(reasons, fmt.Sprintf("Environment/%s spec.safety.destroyProtection=%s", name, v1alpha1.EnvironmentDestroyProtectionRequiredOverride))
	}
	for _, kind := range protectedKindsPresent(state, storageWorkNames, scope) {
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
