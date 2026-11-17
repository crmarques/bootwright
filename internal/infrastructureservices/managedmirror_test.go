package infrastructureservices

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func managedMirrorGraph(registryMachine, artifactMachine, registryEndpoint, artifactEndpoint string) api.Catalog {
	machine := func(name, address string) api.Object {
		return obj(api.Machine, name, m("network", m("addresses", list(m("name", "ip", "address", address)))))
	}
	return api.NewCatalog([]api.Object{
		obj(api.Environment, "lab", m("mirror", m(
			"registry", m("registryRef", "images", "endpointRef", registryEndpoint),
			"artifacts", m("serverRef", "files", "endpointRef", artifactEndpoint)))),
		machine("reg-host", "192.0.2.5"),
		machine("file-host", "192.0.2.6"),
		obj(api.Registry, "images", m("management", "managed", "machineRef", registryMachine, "port", api.IntegerValue("5000"), "bindAddress", "0.0.0.0",
			"endpoints", list(m("name", "ip", "addressRef", "ip")))),
		obj(api.ArtifactServer, "files", m("management", "managed", "machineRef", artifactMachine, "bindAddress", "0.0.0.0",
			"listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue("443"))),
			"endpoints", list(m("name", "ip", "listenerRef", "https", "addressRef", "ip")))),
	})
}

func TestManagedMirrorsListThePlacementOfEachManagedMirror(t *testing.T) {
	mirrors := ManagedMirrors(managedMirrorGraph("reg-host", "reg-host", "ip", "ip"))
	if len(mirrors) != 2 {
		t.Fatalf("mirrors = %+v", mirrors)
	}
	image, artifact := mirrors[0], mirrors[1]
	if image.Service.Identity() != "Registry/images" || image.Field != "$.spec.mirror.registry.registryRef" || image.Host != "reg-host" {
		t.Errorf("image mirror = %+v", image)
	}
	if artifact.Service.Identity() != "ArtifactServer/files" || artifact.Field != "$.spec.mirror.artifacts.serverRef" || artifact.Host != "reg-host" {
		t.Errorf("artifact mirror = %+v", artifact)
	}
	if got := strings.Join(image.Addresses, ","); got != "0.0.0.0,192.0.2.5" {
		t.Errorf("image addresses = %q", got)
	}
	if image.Conflict != nil || image.Unlocated != nil || artifact.Conflict != nil || artifact.Unlocated != nil {
		t.Errorf("a consistent pair is refused: %+v / %+v", image, artifact)
	}
}

func TestManagedMirrorsRefuseMirrorsOnTwoHosts(t *testing.T) {
	mirrors := ManagedMirrors(managedMirrorGraph("reg-host", "file-host", "ip", "ip"))
	if len(mirrors) != 2 || mirrors[0].Conflict == nil {
		t.Fatalf("mirrors = %+v", mirrors)
	}
	conflict := mirrors[0].Conflict
	if !strings.Contains(conflict.Reason, "Machine/reg-host") || !strings.Contains(conflict.Reason, "Machine/file-host") ||
		conflict.Remediation != "place Registry/images and ArtifactServer/files on one Machine" {
		t.Errorf("conflict = %+v", conflict)
	}
}

func TestManagedMirrorsRefuseAnEndpointThatNamesNoLocation(t *testing.T) {
	mirrors := ManagedMirrors(managedMirrorGraph("reg-host", "reg-host", "missing", "missing"))
	if len(mirrors) != 2 {
		t.Fatalf("mirrors = %+v", mirrors)
	}
	if mirrors[0].Unlocated == nil || !strings.Contains(mirrors[0].Unlocated.Reason, "names no address on Machine/reg-host") ||
		!strings.Contains(mirrors[0].Unlocated.Remediation, "spec.mirror.registry.endpointRef") {
		t.Errorf("image unlocated = %+v", mirrors[0].Unlocated)
	}
	if mirrors[1].Unlocated == nil || !strings.Contains(mirrors[1].Unlocated.Reason, "names no HTTPS URL") ||
		!strings.Contains(mirrors[1].Unlocated.Remediation, "select an https endpoint of ArtifactServer/files") {
		t.Errorf("artifact unlocated = %+v", mirrors[1].Unlocated)
	}
}

func TestManagedMirrorsRefuseAnArtifactURLBeyondTheClientPathBound(t *testing.T) {
	long := strings.Repeat("x", 5000)
	catalog := api.NewCatalog([]api.Object{
		obj(api.Environment, long, m("mirror", m("artifacts", m("serverRef", "files", "endpointRef", "ip")))),
		obj(api.Machine, "file-host", m("network", m("addresses", list(m("name", "ip", "address", "192.0.2.6"))))),
		obj(api.ArtifactServer, "files", m("management", "managed", "machineRef", "file-host",
			"listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue("443"))),
			"endpoints", list(m("name", "ip", "listenerRef", "https", "addressRef", "ip")))),
	})
	mirrors := ManagedMirrors(catalog)
	if len(mirrors) != 1 || mirrors[0].Unlocated == nil || !strings.Contains(mirrors[0].Unlocated.Reason, "exceeds the bound a client path fits beneath") ||
		!strings.Contains(mirrors[0].Unlocated.Remediation, "shorten the name of Environment/") {
		t.Fatalf("mirrors = %+v", mirrors)
	}
}

func TestManagedMirrorsAreEmptyWithoutOneEnvironmentOrAManagedMirror(t *testing.T) {
	if got := ManagedMirrors(api.NewCatalog(nil)); got != nil {
		t.Errorf("no Environment: %+v", got)
	}
	external := api.NewCatalog([]api.Object{
		obj(api.Environment, "lab", m("mirror", m("artifacts", m("url", "https://mirror.example.test/tools")))),
	})
	if got := ManagedMirrors(external); got != nil {
		t.Errorf("an operator-run mirror: %+v", got)
	}
}
