package ceph

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func TestPoolNativeNameProjectsIntoConsumers(t *testing.T) {
	pool := replicatedPool("primary-volumes", "nativeName", "volumes")
	image := imageOf("disk", "primary-volumes")
	catalog := planCatalog(6, 6, nil, pool, image)
	r, err := planned(t, catalog, api.CephRBDImage, "disk")
	if err != nil || r.RBD == nil || r.RBD.Pool != "volumes" {
		t.Fatalf("native pool name was not projected: %+v %v", r.RBD, err)
	}
}

func TestPoolNativeNameProjectsClientFilesystemAndGateway(t *testing.T) {
	pool := replicatedPool("primary-volumes", "nativeName", "volumes")
	client := childOf(api.CephClient, "reader", "poolRef", pool.Name(), "access", "readOnly")
	r, err := planned(t, planCatalog(3, 3, nil, pool, client), api.CephClient, client.Name())
	if err != nil || r.Client.Pool != "volumes" {
		t.Fatalf("client alias: %+v %v", r.Client, err)
	}
	metadata := cephfsPool("fs-meta", "cephfsMetadata", "nativeName", "native-meta")
	data := cephfsPool("fs-data", "cephfsData", "nativeName", "native-data")
	fs := filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "apps", "poolLayoutRef", "fs-data")))
	catalog := planCatalog(3, 3, withMDS, metadata, data, fs)
	r, err = planned(t, catalog, api.CephFilesystem, fs.Name())
	if err != nil || r.Filesystem.MetadataPool != "native-meta" || r.Filesystem.DefaultDataPool != "native-data" || r.Filesystem.Groups[0].Pool != "native-data" {
		t.Fatalf("filesystem aliases: %+v %v", r.Filesystem, err)
	}
	objects := rgwPool("object-data")
	objects = objects.WithSpec(objects.Spec().With("nativeName", api.StringValue("native-objects")))
	gateway := objectGateway("objects", "pools", requestMap("dataPoolRef", objects.Name()))
	catalog = planCatalog(3, 3, withPairAndRGW, objects, gateway)
	requests, err := gatewayRequests(catalog, gateway, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if got := requests[len(requests)-1].Gateway.DataPool; got != "native-objects" {
		t.Fatalf("gateway alias %q", got)
	}
}

func TestPoolNativeNameCollisionRefusesBeforePlanning(t *testing.T) {
	first := replicatedPool("first", "nativeName", "volumes")
	second := replicatedPool("second", "nativeName", "volumes")
	if issues := cephcluster.Validate(first, planCatalog(3, 3, nil, first, second)); len(issues) == 0 {
		t.Fatal("duplicate native identity admitted")
	}
}
