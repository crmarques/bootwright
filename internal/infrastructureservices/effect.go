package infrastructureservices

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if !IsService(object.Kind()) || object.Spec().Get("management").Text() != "managed" {
		return "", false
	}
	return "no capability of this executable manages the " + string(object.Kind()) + " kind", true
}

func Placement(object api.Object) (machine string, installOnly bool) {
	return object.Spec().Get("machineRef").Text(), object.Spec().Get("retention").Text() == "install-only"
}
