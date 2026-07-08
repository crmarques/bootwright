// Package hooks holds the dependency-light helpers for ClusterAddon hooks:
// lifecycle selection, manifest token extraction/rendering, plan-time target
// resolution, output paths, and content digests. It imports only the API types
// (and internal/addons/inputs for binding-input value resolution) so both the
// desired-state validator and the add-on apply engine can share it without
// pulling in secrets, storage, or converge.
package hooks

import "github.com/crmarques/bootwright/api/v1alpha1"

// At returns the add-on's hooks anchored to the given lifecycle, in declared
// order.
func At(addon v1alpha1.ClusterAddon, lifecycle string) []v1alpha1.ClusterAddonHook {
	var out []v1alpha1.ClusterAddonHook
	for _, hook := range addon.Spec.Hooks {
		if hook.Lifecycle == lifecycle {
			out = append(out, hook)
		}
	}
	return out
}

// HasLifecycle reports whether the add-on declares any hook at the lifecycle.
func HasLifecycle(addon v1alpha1.ClusterAddon, lifecycle string) bool {
	return len(At(addon, lifecycle)) > 0
}

// HasAlwaysAt reports whether the add-on declares a run: always hook at any of
// the given lifecycles. The apply engine consults it to decide whether the
// idempotency pre-check may short-circuit the apply (an always hook at preApply
// or postOperatorReady must run even when the add-on is already ready).
func HasAlwaysAt(addon v1alpha1.ClusterAddon, lifecycles ...string) bool {
	want := map[string]bool{}
	for _, lifecycle := range lifecycles {
		want[lifecycle] = true
	}
	for _, hook := range addon.Spec.Hooks {
		if want[hook.Lifecycle] && v1alpha1.ClusterAddonHookRun(hook) == v1alpha1.ProvisioningPlaybookRunAlways {
			return true
		}
	}
	return false
}
