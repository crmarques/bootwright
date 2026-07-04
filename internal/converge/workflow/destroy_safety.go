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

// ProtectedKindSet returns the union of every in-scope Environment's
// spec.safety.protectedKinds — the object kinds that require --override to destroy or
// destructively rebuild even when destroyProtection is allow. Shared by the destroy
// gate and the apply-override gate so the granular boundary is enforced identically.
func ProtectedKindSet(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, env := range state.Environments {
		for _, kind := range env.Spec.Safety.ProtectedKinds {
			out[kind] = true
		}
	}
	return out
}

// protectedKindsPresent returns the protected kinds that actually have an object in
// the (scope-filtered) state, so the destroy gate names only kinds it would touch.
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
	// Granular tightening: a protected kind present in the (scope-filtered) teardown
	// requires --override even when the fleet default is allow. The scope filter means
	// destroying a scratch ContainerCluster stays friction-free while a protected
	// StorageCluster in scope is gated.
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
