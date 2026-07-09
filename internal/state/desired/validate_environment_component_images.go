package desiredstate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var imageVersionTag = regexp.MustCompile(`^v?[0-9][\w.+-]*$`)
var imageSHA256Digest = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

var componentImageCatalog = map[string]map[string]bool{
	v1alpha1.ComponentSlotLoadBalancer:   {v1alpha1.InfraComponentTypeHAProxy: true},
	v1alpha1.ComponentSlotRegistry:       {v1alpha1.InfraComponentTypeMirrorRegistry: true},
	v1alpha1.ComponentSlotProxy:          {v1alpha1.InfraComponentTypeSquid: true},
	v1alpha1.ComponentSlotNameResolution: {v1alpha1.InfraComponentTypeDnsmasq: true},
	v1alpha1.ComponentSlotArtifactServer: {v1alpha1.ComponentImageTypeArtifactsHTTP: true},
}

func validateComponentImages(env v1alpha1.Environment) []string {
	var errs []string
	for category, types := range env.Spec.ComponentImages {
		allowedTypes, ok := componentImageCatalog[category]
		if !ok {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages key %q is not a known component type (accepted: loadBalancer, registry, proxy, nameResolution, artifactServer)", env.Metadata.Name, category))
			continue
		}
		for typ, image := range types {
			if !allowedTypes[typ] {
				errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s] key %q is not a known type for this category", env.Metadata.Name, category, typ))
				continue
			}
			if image.Local == "" && image.Public == "" {
				errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s][%s] requires at least one of local or public", env.Metadata.Name, category, typ))
			}
			for _, field := range []struct{ name, ref string }{
				{"local", image.Local},
				{"public", image.Public},
			} {
				fieldName, ref := field.name, field.ref
				if err := validatePinnedImageReference(ref); err != "" {
					errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s][%s].%s %q %s", env.Metadata.Name, category, typ, fieldName, ref, err))
				}
			}
		}
	}
	return errs
}

func validatePinnedImageReference(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.Contains(ref, "@") {
		parts := strings.Split(ref, "@")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || !imageSHA256Digest.MatchString(parts[1]) {
			return "must pin a version tag or digest"
		}
		return ""
	}
	if slash := strings.LastIndex(ref, "/"); slash >= 0 {
		ref = ref[slash+1:]
	}
	tagIndex := strings.LastIndex(ref, ":")
	if tagIndex < 0 || tagIndex == len(ref)-1 {
		return "must pin a version tag or digest"
	}
	tag := ref[tagIndex+1:]
	if strings.EqualFold(tag, "latest") {
		return "must not use mutable :latest tag; pin a version tag or digest"
	}
	if !imageVersionTag.MatchString(tag) {
		return "must pin a version tag or digest"
	}
	return ""
}
