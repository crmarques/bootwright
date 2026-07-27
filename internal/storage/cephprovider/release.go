package cephprovider

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	ossReleaseNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9]+$`)
	vendorReleasePattern      = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
	ossCatalogedReleaseNames  = map[string]bool{"squid": true, "tentacle": true}
	ossCatalogedSeries        = []string{"19.2.", "20.2."}
)

type RuntimeOS struct {
	Family         string
	MajorVersions  []string
	ExactVersions  []string
	ManagedMessage string
}

type ResolvedRelease struct {
	Value        string
	Stream       string
	ImageOSMajor string
	RuntimeOS    RuntimeOS
	Cataloged    bool
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
		return ResolvedRelease{Value: value, RuntimeOS: def.runtimeOS, Cataloged: catalogedOSSRelease(value)}, true
	}
	if !vendorReleasePattern.MatchString(value) {
		return ResolvedRelease{}, false
	}
	if release, ok := def.releases[value]; ok {
		return ResolvedRelease{
			Value:        release.value,
			Stream:       leadingComponent(release.value),
			ImageOSMajor: firstNonEmpty(release.imageOSMajor, def.defaultImageOSMajor),
			RuntimeOS:    release.runtimeOS,
			Cataloged:    true,
		}, true
	}
	return ResolvedRelease{
		Value:        value,
		Stream:       leadingComponent(value),
		ImageOSMajor: def.defaultImageOSMajor,
		RuntimeOS:    def.runtimeOS,
	}, true
}

func CatalogedReleases(distribution string) []string {
	def, ok := distributions[distribution]
	if !ok {
		return nil
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return []string{"19.2.x", "20.2.x", "squid", "tentacle"}
	}
	out := make([]string, 0, len(def.releases))
	for value := range def.releases {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func RuntimeOSMajorAllowed(runtimeOS RuntimeOS, version string) bool {
	if len(runtimeOS.MajorVersions) == 0 {
		return true
	}
	return containsString(runtimeOS.MajorVersions, leadingComponent(version))
}

func RuntimeOSVersionCataloged(runtimeOS RuntimeOS, version string) bool {
	if len(runtimeOS.ExactVersions) == 0 {
		return true
	}
	return containsString(runtimeOS.ExactVersions, version)
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

func ExpectedImageRepository(distribution, release, registryURL string) (string, bool) {
	prefix, resolved, ok := imageRepositoryPrefix(distribution, release, registryURL)
	if !ok {
		return "", false
	}
	return prefix + resolved.ImageOSMajor, true
}

func ExpectedImageRepositoryPrefix(distribution, release, registryURL string) (string, bool) {
	prefix, _, ok := imageRepositoryPrefix(distribution, release, registryURL)
	return prefix, ok
}

func imageRepositoryPrefix(distribution, release, registryURL string) (string, ResolvedRelease, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return "", ResolvedRelease{}, false
	}
	resolved, ok := ResolveRelease(distribution, release)
	if !ok {
		return "", ResolvedRelease{}, false
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return ossImageRepository, ResolvedRelease{}, true
	}
	if registryURL == "" {
		registryURL = def.registryURL
	}
	if def.imagePathTemplate == "" || registryURL == "" || resolved.Stream == "" {
		return "", ResolvedRelease{}, false
	}
	return strings.TrimSuffix(registryURL, "/") + "/" + fmt.Sprintf(def.imagePathTemplate, resolved.Stream, ""), resolved, true
}

func catalogedOSSRelease(value string) bool {
	if ossCatalogedReleaseNames[value] {
		return true
	}
	if !ossUpstreamVersionPattern.MatchString(value) {
		return false
	}
	for _, series := range ossCatalogedSeries {
		if strings.HasPrefix(value, series) {
			return true
		}
	}
	return false
}

func leadingComponent(value string) string {
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		return value[:dot]
	}
	return value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
