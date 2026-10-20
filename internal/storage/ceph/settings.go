package ceph

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage"
)

type Settings struct {
	Modules []string `json:"modules"`
}

func settingsModules() []string {
	modules := append(storage.DerivableManagerModules(), SMBManagerModule)
	slices.Sort(modules)
	return slices.Compact(modules)
}

func settingsRequest(c api.Catalog, r Request, cluster api.Object) (Request, bool) {
	modules := storage.DerivedManagerModules(cluster.Spec().Get("ceph"))
	if len(clusterSMBResources(c, cluster.Name())) != 0 {
		modules = append(modules, SMBManagerModule)
	}
	slices.Sort(modules)
	modules = slices.Compact(modules)
	if len(r.Auxiliary) == 0 && len(modules) == 0 {
		return Request{}, false
	}
	r.Action = "settings"
	r.Settings = &Settings{Modules: modules}
	return r, true
}
