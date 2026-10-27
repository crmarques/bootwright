package containercluster

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.OpenShiftCluster {
		return "", false
	}
	return "no capability of this executable realizes the " + string(api.OpenShiftCluster) + " kind", true
}
