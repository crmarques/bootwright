package storage

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.CephCluster && !slices.Contains(api.Children(api.CephCluster), object.Kind()) {
		return "", false
	}
	return "no capability of this executable realizes the " + string(object.Kind()) + " kind", true
}
