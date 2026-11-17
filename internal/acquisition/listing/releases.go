package listing

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
)

func (t Tools) MirrorReleases(catalog api.Catalog) ([]mirrorfill.Release, error) {
	if mirror, declared := infrastructureservices.ImageMirrorOf(catalog); !declared || !mirror.Managed {
		return nil, nil
	}
	tools, err := controller.SelectTools(catalog)
	if err != nil {
		return nil, err
	}
	var releases []mirrorfill.Release
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		digest, managed := agentinstall.MirroredRelease(catalog, cluster)
		if !managed {
			continue
		}
		version := cluster.Spec().Get("distribution", "release", "version").Text()
		index := slices.IndexFunc(tools, func(tool evidence.ToolRequest) bool {
			return tool.Kind == "openshift-clients" && tool.Compatibility == "openshift" && tool.Version == version
		})
		location := ""
		if index >= 0 {
			if location, err = t.SourceURL(tools[index]); err != nil {
				return nil, err
			}
		}
		releases = append(releases, mirrorfill.Release{Client: mirrorfill.ReleaseClientFor(version, location), Cluster: cluster.Name(), Digest: digest,
			PullSecretRef: cluster.Spec().Get("install", "pullSecretRef").Text(), Version: version})
	}
	return releases, nil
}

func (t Tools) mirrorReleaseAcquisitions(catalog api.Catalog) ([]Row, error) {
	releases, err := t.MirrorReleases(catalog)
	if err != nil || len(releases) == 0 {
		return nil, err
	}
	mirror, _ := infrastructureservices.ImageMirrorOf(catalog)
	host := []string{mirror.Registry.Spec().Get("machineRef").Text()}
	var rows []Row
	for _, release := range releases {
		if err := release.Refusal(); err != nil {
			return nil, err
		}
		consumer := api.NewObject(api.OpenShiftCluster, release.Cluster, api.Value{}, api.Value{}).Identity()
		for _, repository := range openShiftReleaseRepositories {
			rows = append(rows, Row{Kind: "mirror release", Artifact: repository, Version: release.Version, Origin: repository, Class: originClass("https://" + repository),
				Machines: host, Consumer: consumer})
		}
		rows = append(rows,
			Row{Kind: "mirror release client", Artifact: "openshift-clients", Version: release.Version, Origin: release.Client.URL, Class: originClass(release.Client.URL),
				Machines: host, Consumer: consumer},
			Row{Kind: "mirror release client checksums", Artifact: "openshift-clients", Version: release.Version, Origin: release.Client.Checksums,
				Class: originClass(release.Client.Checksums), Machines: host, Consumer: consumer})
		for _, store := range releaseSignatureStores {
			rows = append(rows, Row{Kind: "mirror release signatures", Artifact: infrastructureservices.OpenShiftReleaseRepository, Version: release.Version,
				Origin: store, Class: originClass(store), Machines: host, Consumer: consumer})
		}
	}
	return rows, nil
}

func clusterAcquisitions(catalog api.Catalog, controllerMachine string) []Row {
	var rows []Row
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		machines := append([]string{controllerMachine}, clusterNodes(cluster)...)
		distribution := cluster.Spec().Get("distribution")
		release := distribution.Get("release")
		if distribution.Get("type").Text() == "openshift" {
			for _, repository := range openShiftReleaseRepositories {
				if agentinstall.NeverContacted(catalog, cluster, repository) {
					continue
				}
				rows = append(rows, Row{Kind: "release", Artifact: repository, Version: release.Get("version").Text(), Origin: repository, Class: originClass("https://" + repository),
					Machines: machines, Consumer: cluster.Identity()})
			}
		} else {
			rows = append(rows, Row{Kind: "release", Artifact: "the release image its installer names", Version: release.Get("version").Text(), Origin: "quay.io", Class: acquisition.ClassPublic,
				Machines: machines, Consumer: cluster.Identity()})
		}
		for _, source := range agentinstall.ImageDigestSources(catalog, cluster) {
			entry, _ := source.(map[string]any)
			mirrors, _ := entry["mirrors"].([]any)
			for _, mirror := range mirrors {
				location, _ := mirror.(string)
				origin, _ := entry["source"].(string)
				rows = append(rows, Row{Kind: "release mirror", Artifact: origin, Origin: location, Class: originClass("https://" + location),
					Machines: machines, Consumer: cluster.Identity()})
			}
		}
	}
	return rows
}
