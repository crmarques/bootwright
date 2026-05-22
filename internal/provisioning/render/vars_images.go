package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func managedComponentImage(state v1alpha1.State, category, typ, fallback string) string {
	env := primaryEnvironment(state)
	if env != nil {
		if byType, ok := env.Spec.ComponentImages[category]; ok {
			if image, ok := byType[typ]; ok {
				if image.Local != "" {
					return image.Local
				}
				if image.Public != "" {
					return image.Public
				}
			}
		}
	}
	return fallback
}

func componentPinVersion(state v1alpha1.State, name, fallback string) string {
	for _, pin := range ComponentPins(state) {
		if pin.Name == name && pin.Version != "" {
			return pin.Version
		}
	}
	return fallback
}

func managedHAProxyImage(state v1alpha1.State) string {
	return managedComponentImage(
		state,
		v1alpha1.ComponentImageCategoryLoadBalancer,
		v1alpha1.ComponentImageTypeHAProxy,
		"docker.io/library/haproxy:"+componentPinVersion(state, v1alpha1.ComponentImageTypeHAProxy, defaultHAProxyVersion),
	)
}

func managedMirrorRegistryImage(state v1alpha1.State) string {
	return managedComponentImage(
		state,
		v1alpha1.ComponentImageCategoryRegistry,
		v1alpha1.ComponentImageTypeMirrorRegistry,
		"docker.io/library/registry:"+componentPinVersion(state, v1alpha1.ComponentImageTypeMirrorRegistry, defaultMirrorRegistryVersion),
	)
}

func managedSquidImage(state v1alpha1.State) string {
	return managedComponentImage(
		state,
		v1alpha1.ComponentImageCategoryProxy,
		v1alpha1.ComponentImageTypeSquid,
		"docker.io/openeuler/squid:"+componentPinVersion(state, v1alpha1.ComponentImageTypeSquid, defaultSquidVersion),
	)
}

func managedDnsmasqImage(state v1alpha1.State) string {
	return managedComponentImage(
		state,
		v1alpha1.ComponentImageCategoryDNS,
		v1alpha1.ComponentImageTypeDnsmasq,
		"docker.io/dockurr/dnsmasq:"+componentPinVersion(state, v1alpha1.ComponentImageTypeDnsmasq, defaultDnsmasqVersion),
	)
}
