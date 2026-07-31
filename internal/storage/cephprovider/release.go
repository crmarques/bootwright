package cephprovider

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const MgmtGatewayMinimumCephMajor = 20

var (
	ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	ossReleaseNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9]+$`)
	vendorReleasePattern      = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

	ossReleaseMajors = map[string]int{
		"octopus":  15,
		"pacific":  16,
		"quincy":   17,
		"reef":     18,
		"squid":    19,
		"tentacle": 20,
	}
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

func UpstreamCephMajor(distribution, release string) (int, bool) {
	resolved, ok := ResolveRelease(distribution, release)
	if !ok {
		return 0, false
	}
	if v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return 0, false
	}
	if major, ok := ossReleaseMajors[resolved.Value]; ok {
		return major, true
	}
	if !ossUpstreamVersionPattern.MatchString(resolved.Value) {
		return 0, false
	}
	major, err := strconv.Atoi(leadingComponent(resolved.Value))
	if err != nil {
		return 0, false
	}
	return major, true
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

func DerivedOSSImageVersion(distribution, release string) string {
	if distribution != v1alpha1.StorageCephDistributionOSS {
		return ""
	}
	if ossUpstreamVersionPattern.MatchString(release) {
		return "v" + release
	}
	return ""
}

func JoinImageReference(base, version string) string {
	if base == "" || version == "" {
		return ""
	}
	if strings.HasPrefix(version, "sha256:") {
		return base + "@" + version
	}
	return base + ":" + version
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
