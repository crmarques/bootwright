package addons

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.ClusterAddonBinding {
		return "", false
	}
	return "no capability of this executable realizes the " + string(api.ClusterAddonBinding) + " kind", true
}
