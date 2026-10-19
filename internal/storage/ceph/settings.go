package ceph

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage"
)

type Settings struct {
	Modules []string `json:"modules"`
}

func settingsRequest(r Request, cluster api.Object) (Request, bool) {
	modules := storage.DerivedManagerModules(cluster.Spec().Get("ceph"))
	if len(r.Auxiliary) == 0 && len(modules) == 0 {
		return Request{}, false
	}
	r.Action = "settings"
	r.Settings = &Settings{Modules: modules}
	return r, true
}
