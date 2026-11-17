package listing

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	cephnative "github.com/crmarques/bootwright/internal/storage/ceph"
)

func storageAcquisitions(catalog api.Catalog, controllerMachine string) []Row {
	var rows []Row
	for _, view := range catalog.CephClusters() {
		cluster, spec := view.Object, view.Object.Spec()
		if spec.Get("management").Text() != "managed" {
			continue
		}
		hosts := []string{}
		for _, node := range view.Nodes {
			if machine := node.MachineRef; machine != "" {
				hosts = append(hosts, machine)
			}
		}
		images := []api.Value{spec.Get("image")}
		for _, field := range spec.Get("images").Fields() {
			images = append(images, field.Value)
		}
		for _, image := range images {
			base := image.Get("base").Text()
			if base == "" {
				continue
			}
			version, digest := image.Get("version").Text(), ""
			if strings.HasPrefix(strings.ToLower(version), "sha256:") {
				digest = strings.ToLower(strings.TrimPrefix(strings.ToLower(version), "sha256:"))
			}
			rows = append(rows, Row{Kind: "storage image", Artifact: base, Version: version, Origin: base, Class: originClass("https://" + base), SHA256: digest,
				Machines: append([]string{controllerMachine}, hosts...), Consumer: cluster.Identity()})
		}
		repository := spec.Get("packageRepository").Text()
		if repository == "" {
			repository = cephnative.DefaultPackageRepository
		}
		rows = append(rows, Row{Kind: "storage packages", Artifact: "cephadm and its packages", Version: spec.Get("packageVersion").Text(), Origin: repository, Class: originClass(repository),
			Machines: hosts, Consumer: cluster.Identity()})
	}
	return rows
}
