package cephprovider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	ossReleaseNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9]+$`)
	vendorReleasePattern      = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
)

type RuntimeOS struct {
	Family string
}

type ResolvedRelease struct {
	Value  string
	Stream string
}

func ResolveRelease(distribution, authored string) (ResolvedRelease, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return ResolvedRelease{}, false
	}
	value := authored
	if value == "" {
		value = def.defaultRelease
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		if !ossReleaseNamePattern.MatchString(value) && !ossUpstreamVersionPattern.MatchString(value) {
			return ResolvedRelease{}, false
		}
		return ResolvedRelease{Value: value}, true
	}
	if !vendorReleasePattern.MatchString(value) {
		return ResolvedRelease{}, false
	}
	return ResolvedRelease{Value: value, Stream: leadingComponent(value)}, true
}

func DefaultRelease(distribution string) string {
	if def, ok := distributions[distribution]; ok {
		return def.defaultRelease
	}
	return ""
}

func DefaultRegistryURL(distribution string) string {
	if def, ok := distributions[distribution]; ok {
		return def.registryURL
	}
	return ""
}

func DerivedOSSImage(release string) string {
	if ossUpstreamVersionPattern.MatchString(release) {
		return ossImageRepository + ":v" + release
	}
	return ""
}

func ImageRepository(image string) string {
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	segStart := strings.LastIndexByte(image, '/') + 1
	if colon := strings.IndexByte(image[segStart:], ':'); colon >= 0 {
		return image[:segStart+colon]
	}
	return image
}

func DerivedImageRepository(distribution, release, registryURL string) (string, bool) {
	prefix, ok := ImageRepositoryPrefix(distribution, release, registryURL)
	if !ok {
		return "", false
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return prefix, true
	}
	return prefix + distributions[distribution].defaultImageOSMajor, true
}

func ImageRepositoryPrefix(distribution, release, registryURL string) (string, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return "", false
	}
	resolved, ok := ResolveRelease(distribution, release)
	if !ok {
		return "", false
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return ossImageRepository, true
	}
	if registryURL == "" {
		registryURL = def.registryURL
	}
	if def.imagePathTemplate == "" || registryURL == "" || resolved.Stream == "" {
		return "", false
	}
	return strings.TrimSuffix(registryURL, "/") + "/" + fmt.Sprintf(def.imagePathTemplate, resolved.Stream, ""), true
}

func leadingComponent(value string) string {
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		return value[:dot]
	}
	return value
}
