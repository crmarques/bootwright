package agentinstall

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func encryptedCatalog(roles []string, tpm bool) api.Catalog {
	objects := compactCatalog().Objects()
	for i, object := range objects {
		switch object.Kind() {
		case api.OpenShiftCluster:
			nodes := object.Spec().Get("nodes").Items()
			// Add a worker to exercise native masters/workers/all on one topology.
			nodes = append(nodes, node("worker-0", "worker", "worker-01", "worker-0.ocp.lab.example.test"))
			objects[i] = object.WithSpec(object.Spec().With("nodes", api.ListValue(nodes...)).WithPath(api.MapValue(field("roles", api.StringList(roles...)), field("unlock", api.MapValue(field("tpm2", api.MapValue())))), "security", "diskEncryption"))
		case api.InfraProvider:
			if tpm {
				profiles := object.Spec().Get("libvirt", "machineProfiles").Items()
				for j, profile := range profiles {
					profiles[j] = profile.With("tpm", api.MapValue())
				}
				objects[i] = object.WithSpec(object.Spec().WithPath(api.ListValue(profiles...), "libvirt", "machineProfiles"))
			}
		}
	}
	return api.NewCatalog(append(objects, guest("worker-01", "198.51.100.31/24")))
}

func TestDiskEncryptionGolden(t *testing.T) {
	for name, roles := range map[string][]string{"all": {"controlPlane", "worker"}, "masters": {"controlPlane"}, "workers": {"worker"}} {
		t.Run(name, func(t *testing.T) {
			media, install, _ := onlyRequests(t, encryptedCatalog(roles, true))
			if media.DiskEncryption == nil || media.DiskEncryption.EnableOn != name {
				t.Fatalf("encryption=%+v", media.DiskEncryption)
			}
			data, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "media-request-encryption-"+name, data)
			if _, err := DecodeMediaRequest(data); err != nil {
				t.Fatal(err)
			}
			for _, node := range install.Nodes {
				want := name == "all" || name == "workers" && node.Name == "worker-0" || name == "masters" && node.Name != "worker-0"
				if node.RequireTPM != want {
					t.Fatalf("TPM selection=%+v", node)
				}
			}
		})
	}
}

func TestDiskEncryptionRefusesLibvirtWithoutTPM(t *testing.T) {
	catalog := encryptedCatalog([]string{"controlPlane"}, false)
	if len(Refusals(catalog)) != 1 {
		t.Fatal("accepted encryption without TPM")
	}
	if _, _, _, err := Requests(catalog, "controller", testContext); err == nil {
		t.Fatal("derived encryption without TPM")
	}
}

func TestComputeEncryptionSelectsTheSharedWorkerPool(t *testing.T) {
	for _, selected := range []string{"worker", "infra"} {
		cluster := api.NewObject(api.OpenShiftCluster, "fixture", api.Value{}, api.MapValue(field("security", api.MapValue(field("diskEncryption", api.MapValue(field("roles", api.StringList(selected))))))))
		if !encryptedRole(cluster, "worker") || !encryptedRole(cluster, "infra") || encryptedRole(cluster, "controlPlane") {
			t.Fatalf("%s did not select exactly the shared worker pool", selected)
		}
	}
}
