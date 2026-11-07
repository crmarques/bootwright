package substrate

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	switch {
	case object.Kind() == api.InfraProvider:
		return "no capability of this executable realizes the " + string(api.InfraProvider) + " kind", true
	case object.Kind() == api.Network && object.Spec().Has("provided") && !object.Spec().Get("provided").Bool():
		return "no libvirt provider host of this executable realizes " + object.Identity() + ", because its spec.libvirt.providerRef names no libvirt InfraProvider", true
	}
	return "", false
}
