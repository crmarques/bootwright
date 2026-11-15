package openshift

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func accessFixture(ssh api.Value) (api.Object, api.Object) {
	cluster := api.NewObject(api.OpenShiftCluster, "example", api.MapValue(), api.MapValue().WithPath(ssh, "install", "nodeSSH").With("nodes", api.ListValue(api.MapValue().With("name", api.StringValue("node")).With("machineRef", api.StringValue("host")))))
	host := api.NewObject(api.Machine, "host", api.MapValue(), api.MapValue().WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("ip"), "network", "installAddressRef").WithPath(api.ListValue(api.MapValue().With("name", api.StringValue("ip")).With("address", api.StringValue("192.0.2.10"))), "network", "addresses"))
	return cluster, host
}

func TestAccessCatalogDerivesOnlyTheUniqueClusterPrivateIdentity(t *testing.T) {
	for _, field := range []string{"keyPairRef", "privateKeyRef"} {
		cluster, host := accessFixture(api.MapValue().With(field, api.StringValue("admin")))
		for _, reverse := range []bool{false, true} {
			objects := []api.Object{cluster, host}
			if reverse {
				objects = []api.Object{host, cluster}
			}
			catalog := api.NewCatalog(objects)
			result, _ := AccessCatalog(catalog).Find(api.Machine, "host")
			target, ok := machine.SSH(result)
			if !ok || target.User != "core" || target.Port != 22 || target.Address != "192.0.2.10" || target.PrivateKeyRef != "admin" || machine.Installed(result) {
				t.Fatalf("target %+v", target)
			}
			original, _ := catalog.Find(api.Machine, "host")
			if original.Spec().Has("access") {
				t.Fatal("access projection mutated desired state")
			}
		}
	}
}

func TestAccessCatalogPreservesAuthoredAccessAndRefusesAmbiguousOrPublicOnlyIdentities(t *testing.T) {
	cluster, host := accessFixture(api.MapValue().With("keyPairRef", api.StringValue("admin")))
	for name, objects := range map[string][]api.Object{
		"public only":        {cluster.WithSpec(cluster.Spec().WithPath(api.MapValue().With("publicKeyRef", api.StringValue("public")), "install", "nodeSSH")), host},
		"two memberships":    {cluster, api.NewObject(api.OpenShiftCluster, "other", api.MapValue(), cluster.Spec()), host},
		"provided":           {cluster, host.WithSpec(host.Spec().WithPath(api.BoolValue(true), "os", "provided"))},
		"machine installer":  {cluster, host.WithSpec(host.Spec().WithPath(api.StringValue("installer"), "os", "installProfileRef"))},
		"authored local":     {cluster, host.WithSpec(host.Spec().WithPath(api.BoolValue(true), "access", "local"))},
		"authored ssh":       {cluster, host.WithSpec(host.Spec().WithPath(api.StringValue("operator"), "access", "ssh", "user"))},
		"missing membership": {host},
		"no endpoint":        {cluster, host.WithSpec(host.Spec().Without("network"))},
	} {
		catalog := api.NewCatalog(objects)
		before, _ := catalog.Find(api.Machine, "host")
		after, _ := AccessCatalog(catalog).Find(api.Machine, "host")
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed Machine access", name)
		}
	}
}
