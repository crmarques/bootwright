package hooks

import "github.com/crmarques/bootwright/api/v1alpha1"

func At(addon v1alpha1.ClusterAddon, lifecycle string) []v1alpha1.ClusterAddonHook {
	var out []v1alpha1.ClusterAddonHook
	for _, hook := range addon.Spec.Hooks {
		if hook.Lifecycle == lifecycle {
			out = append(out, hook)
		}
	}
	return out
}

func HasLifecycle(addon v1alpha1.ClusterAddon, lifecycle string) bool {
	return len(At(addon, lifecycle)) > 0
}

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
