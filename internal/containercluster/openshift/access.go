package openshift

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func AccessCatalog(catalog api.Catalog) api.Catalog {
	owners := map[string][]api.Object{}
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		for _, node := range cluster.Spec().Get("nodes").Items() {
			ref := node.Get("machineRef").Text()
			owners[ref] = append(owners[ref], cluster)
		}
	}
	objects := catalog.Objects()
	for i, object := range objects {
		spec := object.Spec()
		if object.Kind() != api.Machine || spec.Has("access") || !spec.Has("os", "clusterInstalled") || len(owners[object.Name()]) != 1 {
			continue
		}
		ssh := owners[object.Name()][0].Spec().Get("install", "nodeSSH")
		key := ssh.Get("keyPairRef").Text()
		if key == "" {
			key = ssh.Get("privateKeyRef").Text()
		}
		if key == "" {
			continue
		}
		reference := spec.Get("network", "installAddressRef").Text()
		if _, ok := infrastructureservices.AddressOf(object, reference); !ok || reference == "" {
			continue
		}
		access := api.MapValue().WithPath(api.StringValue("core"), "ssh", "user").
			WithPath(api.StringValue(reference), "ssh", "addressRef").
			WithPath(api.IntegerValue("22"), "ssh", "port").
			WithPath(api.StringValue(key), "ssh", "auth", "privateKeyRef")
		objects[i] = object.WithSpec(spec.With("access", access))
	}
	return api.NewCatalog(objects)
}
