package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/support"
)

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
	return managedServiceImage(state, v1alpha1.ComponentSlotLoadBalancer, v1alpha1.InfraComponentTypeHAProxy)
}

func managedMirrorRegistryImage(state v1alpha1.State) string {
	return managedServiceImage(state, v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry)
}

func managedSquidImage(state v1alpha1.State) string {
	return managedServiceImage(state, v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid)
}

func managedDnsmasqImage(state v1alpha1.State) string {
	return managedServiceImage(state, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)
}

func managedArtifactsHTTPImage(state v1alpha1.State) string {
	return managedServiceImage(state, v1alpha1.ComponentSlotArtifactServer, v1alpha1.ArtifactServerProtocolHTTP)
}

func managedServiceImage(state v1alpha1.State, kind, realisation string) string {
	image, ok := support.ServiceImagePin(kind, realisation)
	if !ok {
		return ""
	}
	return managedComponentImage(
		state,
		image.Category,
		image.Type,
		image.Repository+":"+componentPinVersion(state, image.Type, image.Version),
	)
}
