package main

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/cli"
)

const updateServiceURL = "https://api.openshift.com/api/upgrades_info/v1/graph"

var defaultCatalogRepositories = map[string]string{
	"redhat-operators":    "registry.redhat.io/redhat/redhat-operator-index",
	"certified-operators": "registry.redhat.io/redhat/certified-operator-index",
	"community-operators": "registry.redhat.io/redhat/community-operator-index",
	"redhat-marketplace":  "registry.redhat.io/redhat/redhat-marketplace-index",
}

func operatorAcquisitions(catalog api.Catalog) []cli.Acquisition {
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
			rows = append(rows, cli.Acquisition{Kind: "default catalog image", Artifact: repository, Version: releaseTag(distribution.Get("release", "version").Text()), Origin: repository,
				Class: originClass("https://" + repository), Machines: nodes, Consumer: cluster.Identity()})
		}
		rows = append(rows, cli.Acquisition{Kind: "update service", Artifact: "the cluster's update graph", Version: distribution.Get("release", "channel").Text(), Origin: updateServiceURL,
			Class: originClass(updateServiceURL), Machines: nodes, Consumer: cluster.Identity()})
	}
	return rows
}

func addonCatalogAcquisitions(catalog api.Catalog) []cli.Acquisition {
	selected, _ := addons.Instances(catalog, addonpackages.Builtin())
	var rows []cli.Acquisition
	for _, member := range selected {
		image := member.Instance.Config.Get("subscription", "catalogSource", "image").Text()
		cluster, found := catalog.Find(api.OpenShiftCluster, member.Instance.Cluster)
		if image == "" || !found {
			continue
		}
		row := imageAcquisition("add-on catalog image", image, clusterNodes(cluster), "ClusterAddonBinding/"+member.Instance.Binding)
		if !slices.ContainsFunc(rows, func(seen cli.Acquisition) bool {
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
