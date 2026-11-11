package acquisition

import "strings"

const (
	ClassVendor         = "vendor"
	ClassPublic         = "public"
	ClassDeclared       = "declared"
	ClassVendorDelivery = "vendor delivery"
)

var vendorHosts = []string{
	"access.redhat.com", "api.openshift.com", "cdn.redhat.com", "cp.icr.io", "dd2.icr.io", "icr.io", "mirror.openshift.com", "public.dhe.ibm.com",
	"registry.access.redhat.com", "registry.connect.redhat.com", "registry.redhat.io", "subscription.rhsm.redhat.com",
}

var publicHosts = []string{
	"api.github.com", "auth.docker.io", "cdn.dl.k8s.io", "cdn.quay.io", "cdn01.quay.io", "cdn02.quay.io", "cdn03.quay.io", "cdn04.quay.io", "cdn05.quay.io", "cdn06.quay.io",
	"dl.k8s.io", "docker.io", "files.pythonhosted.org", "gcr.io", "get.helm.sh", "ghcr.io", "github.com", "objects.githubusercontent.com",
	"production.cloudfront.docker.com", "quay.io", "registry-1.docker.io", "registry.gitlab.com", "registry.k8s.io", "release-assets.githubusercontent.com",
}

func OriginClass(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if index := strings.LastIndex(host, ":"); index >= 0 && !strings.Contains(host[index:], "]") {
		host = host[:index]
	}
	for _, known := range vendorHosts {
		if host == known {
			return ClassVendor
		}
	}
	for _, known := range publicHosts {
		if host == known {
			return ClassPublic
		}
	}
	return ClassDeclared
}
