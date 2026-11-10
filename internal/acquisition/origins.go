package acquisition

import "strings"

const (
	ClassVendor   = "vendor"
	ClassPublic   = "public"
	ClassDeclared = "declared"
)

var vendorHosts = []string{
	"cp.icr.io", "icr.io", "mirror.openshift.com", "public.dhe.ibm.com",
	"registry.access.redhat.com", "registry.connect.redhat.com", "registry.redhat.io",
}

var publicHosts = []string{
	"api.github.com", "cdn.dl.k8s.io", "dl.k8s.io", "docker.io", "files.pythonhosted.org", "gcr.io", "get.helm.sh",
	"ghcr.io", "github.com", "objects.githubusercontent.com", "quay.io", "registry.gitlab.com", "registry.k8s.io",
	"release-assets.githubusercontent.com",
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
