package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// Lock is the deterministic component-pin record written under
// <rendered-dir>/bootwright.lock.yaml. Renderers and Ansible roles read it
// to know which tool / image versions to use.
func Lock(state v1alpha1.State) map[string]any {
	return map[string]any{
		"apiVersion": v1alpha1.APIVersion,
		"kind":       "BootwrightLock",
		"components": ComponentPins(state),
	}
}
