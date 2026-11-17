package listing

import (
	"net/url"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
)

func clusterNodes(cluster api.Object) []string {
	var machines []string
	for _, node := range cluster.Spec().Get("nodes").Items() {
		if machine := node.Get("machineRef").Text(); machine != "" {
			machines = append(machines, machine)
		}
	}
	return machines
}

func originClass(location string) string {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Host == "" {
		return acquisition.ClassDeclared
	}
	return acquisition.OriginClass(parsed.Host)
}
