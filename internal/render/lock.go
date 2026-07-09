package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func Lock(state v1alpha1.State) map[string]any {
	return map[string]any{
		"apiVersion": v1alpha1.APIVersion,
		"kind":       "BootwrightLock",
		"components": ComponentPins(state),
	}
}
