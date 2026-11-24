package managedos

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.Machine {
		return "", false
	}
	if object.Spec().Has("os", "provided", "updates") {
		return "no capability of this executable realizes the Machine's provided OS update", true
	}
	if object.Spec().Has("os", "provided", "subscription") {
		return "no capability of this executable realizes the Machine's provided registration", true
	}
	if !api.MachineInstalled(object) && !api.MachineClusterInstalled(object) {
		return "", false
	}
	return "no capability of this executable realizes a Machine whose operating system is not provided", true
}
