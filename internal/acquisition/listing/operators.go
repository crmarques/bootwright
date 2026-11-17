package listing

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
)

const UpdateServiceURL = "https://api.openshift.com/api/upgrades_info/v1/graph"

var defaultCatalogRepositories = map[string]string{
	"redhat-operators":    "registry.redhat.io/redhat/redhat-operator-index",
	"certified-operators": "registry.redhat.io/redhat/certified-operator-index",
	"community-operators": "registry.redhat.io/redhat/community-operator-index",
	"redhat-marketplace":  "registry.redhat.io/redhat/redhat-marketplace-index",
}

func DefaultCatalogRepository(name string) string { return defaultCatalogRepositories[name] }

func operatorAcquisitions(catalog api.Catalog) []Row {
	rows := addonCatalogAcquisitions(catalog)
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		distribution := cluster.Spec().Get("distribution")
		if distribution.Get("type").Text() != "openshift" {
			continue
		}
		nodes := clusterNodes(cluster)
		for _, name := range addons.DefaultCatalogs() {
			repository := defaultCatalogRepositories[name]
			if repository == "" {
				continue
			}
			rows = append(rows, Row{Kind: "default catalog image", Artifact: repository, Version: releaseTag(distribution.Get("release", "version").Text()), Origin: repository,
				Class: originClass("https://" + repository), Machines: nodes, Consumer: cluster.Identity()})
		}
		rows = append(rows, Row{Kind: "update service", Artifact: "the cluster's update graph", Version: distribution.Get("release", "channel").Text(), Origin: UpdateServiceURL,
			Class: originClass(UpdateServiceURL), Machines: nodes, Consumer: cluster.Identity()})
	}
	return rows
}

func addonCatalogAcquisitions(catalog api.Catalog) []Row {
	selected, _ := addons.Instances(catalog, addonpackages.Builtin())
	var rows []Row
	for _, member := range selected {
		image := member.Instance.Config.Get("subscription", "catalogSource", "image").Text()
		cluster, found := catalog.Find(api.OpenShiftCluster, member.Instance.Cluster)
		if image == "" || !found {
			continue
		}
		row := imageAcquisition("add-on catalog image", image, clusterNodes(cluster), "ClusterAddonBinding/"+member.Instance.Binding)
		if !slices.ContainsFunc(rows, func(seen Row) bool {
			return seen.Artifact == row.Artifact && seen.Version == row.Version && seen.Consumer == row.Consumer
		}) {
			rows = append(rows, row)
		}
	}
	return rows
}

func releaseTag(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return "v" + parts[0] + "." + parts[1]
}
