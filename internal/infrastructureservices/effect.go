package infrastructureservices

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if !IsService(object.Kind()) || object.Spec().Get("management").Text() != "managed" {
		return "", false
	}
	return "no capability of this executable manages the " + string(object.Kind()) + " kind", true
}

func UnclaimedRemedy(object api.Object) string {
	if object.Kind() != api.Registry && object.Kind() != api.LoadBalancer {
		return ""
	}
	return "declare spec.management: external on " + object.Identity() + " to use a " + string(object.Kind()) + " the operator runs, or remove it from the selected Environment"
}

func Placement(object api.Object) string {
	return object.Spec().Get("machineRef").Text()
}
