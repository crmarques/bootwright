package steps

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ContentRoot(addonSourcePath string, step v1alpha1.ClusterAddonStep) string {
	if v1alpha1.PlaybookSourceIsSet(step.Source) {
		return step.Source.Path
	}
	return filepath.Dir(addonSourcePath)
}

func At(addon v1alpha1.ClusterAddon, anchor string) []v1alpha1.ClusterAddonStep {
	var out []v1alpha1.ClusterAddonStep
	for _, step := range addon.Spec.Steps {
		if value, _ := v1alpha1.ClusterAddonStepAnchor(step); value == anchor {
			out = append(out, step)
		}
	}
	return out
}

func HasAnchor(addon v1alpha1.ClusterAddon, anchor string) bool {
	return len(At(addon, anchor)) > 0
}

func HasAlwaysAt(addon v1alpha1.ClusterAddon, anchors ...string) bool {
	want := map[string]bool{}
	for _, anchor := range anchors {
		want[anchor] = true
	}
	for _, step := range addon.Spec.Steps {
		value, _ := v1alpha1.ClusterAddonStepAnchor(step)
		if want[value] && v1alpha1.ClusterAddonStepRun(step) == v1alpha1.PlaybookRunAlways {
			return true
		}
	}
	return false
}
