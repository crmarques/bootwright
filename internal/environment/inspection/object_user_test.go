package inspection

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestObjectUserCredentialsRequireExplicitClusterSecrets(t *testing.T) {
	f := newFixture(t)
	objects := append(f.catalog.Objects(), api.NewObject(api.StorageObjectUser, "documents", api.Value{}, api.MapValue(field("clusterRef", text("storage")), field("objectGatewayRef", text("objects")))), api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage")))))
	f.catalog = api.NewCatalog(objects)
	f.objectUsers = map[string]Custodied{"documents": {Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})}}
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
	if err != nil || len(f.objectReads) != 0 || len(result.Storage[0].Ceph.ObjectUsers) != 0 {
		t.Fatal("ordinary info read private object material", err)
	}
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil || len(f.objectReads) != 1 || len(result.Storage[0].Ceph.ObjectUsers) != 1 {
		t.Fatal("explicit private handoff did not return owned object credentials", err)
	}
	ClearSecrets(result)
}

func TestObjectUserHandoffRefusesUnprovedCustodyAndClearsOtherSecrets(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), api.NewObject(api.StorageObjectUser, "documents", api.Value{}, api.MapValue(field("clusterRef", text("storage")), field("objectGatewayRef", text("objects")))), api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage"))))))
	f.objectUsers = map[string]Custodied{"documents": {Unproved: true, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})}}
	if result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); err == nil || result != nil {
		t.Fatal("unproved credentials were revealed")
	}
	for _, material := range []secrets.Material{f.passwords["storage"].Material, f.objectUsers["documents"].Material} {
		for _, part := range material.Parts() {
			value, _ := material.Part(part)
			for _, b := range value {
				if b != 0 {
					t.Fatal("refusal retained private material")
				}
			}
		}
	}
}

func handoffUser(name, cluster, gateway string) api.Object {
	return api.NewObject(api.StorageObjectUser, name, api.Value{}, api.MapValue(field("clusterRef", text(cluster)), field("objectGatewayRef", text(gateway))))
}

func handoffMaterial() secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})
}

func assertHandoffCleared(t *testing.T, materials ...secrets.Material) {
	t.Helper()
	for _, material := range materials {
		for _, part := range material.Parts() {
			value, _ := material.Part(part)
			for _, b := range value {
				if b != 0 {
					t.Fatal("private material survived refusal")
				}
			}
		}
	}
}

func TestObjectUserHandoffDoesNotReadForeignClusterCustody(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), handoffUser("foreign", "archive", "absent-gateway")))
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil || result == nil || len(f.objectReads) != 0 {
		t.Fatal("foreign user reached custody", err, f.objectReads)
	}
	ClearSecrets(result)
}

func TestObjectUserHandoffRefusesChangedScopeBeforeCustody(t *testing.T) {
	for _, scenario := range []string{"gateway-other-cluster", "missing-gateway", "unproved-original-scope"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			gatewayCluster := "storage"
			if scenario == "gateway-other-cluster" {
				gatewayCluster = "archive"
			}
			objects := append(f.catalog.Objects(), handoffUser("documents", "storage", "objects"))
			if scenario != "missing-gateway" {
				objects = append(objects, api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text(gatewayCluster)))))
			}
			f.catalog = api.NewCatalog(objects)
			f.objectUnproved = scenario == "unproved-original-scope"
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
			if err == nil || result != nil || len(f.objectReads) != 0 {
				t.Fatal("changed scope read custody", err, f.objectReads)
			}
			assertHandoffCleared(t, f.passwords["storage"].Material)
		})
	}
}

func TestObjectUserHandoffLateFailureClearsEarlierUserAndDashboard(t *testing.T) {
	for _, scenario := range []string{"missing", "unproved", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.catalog = api.NewCatalog(append(f.catalog.Objects(), handoffUser("z-last", "storage", "objects"), handoffUser("a-first", "storage", "objects"), api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage"))))))
			first, last := handoffMaterial(), handoffMaterial()
			f.objectUsers = map[string]Custodied{"a-first": {Material: first}}
			if scenario == "unproved" {
				f.objectUsers["z-last"] = Custodied{Material: last, Unproved: true}
			}
			if scenario == "malformed" {
				last = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("PRIVATE")})
				f.objectUsers["z-last"] = Custodied{Material: last}
			}
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
			if err == nil || result != nil || len(f.objectReads) != 2 || f.objectReads[0] != "lab/a-first" || f.objectReads[1] != "lab/z-last" {
				t.Fatal("late refusal did not follow sorted users", err, f.objectReads)
			}
			assertHandoffCleared(t, first, f.passwords["storage"].Material)
			if scenario != "missing" {
				assertHandoffCleared(t, last)
			}
			last.Clear()
		})
	}
}
