package customplaybooks

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.CustomPlaybook {
		return "", false
	}
	if enabled := object.Spec().Get("enabled"); enabled.Type() != api.Boolean || !enabled.Bool() {
		return "", false
	}
	return "no capability of this executable runs an enabled CustomPlaybook", true
}
