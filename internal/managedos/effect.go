package managedos

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.Machine {
		return "", false
	}
	if provided := object.Spec().Get("os", "provided"); provided.Type() != api.Boolean || provided.Bool() {
		return "", false
	}
	return "no capability of this executable realizes a Machine whose operating system is not provided", true
}
