package controller_test

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

type pinnedShape struct {
	object, field, reason, remediation string
}

func mirrorGraph(mirror api.Value, bridge string, services ...api.Object) []api.Object {
	addresses := api.ListValue(api.MapValue().With("name", api.StringValue("ip")).With("address", api.StringValue("192.0.2.10")))
	objects := []api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef").With("mirror", mirror)),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").
			WithPath(api.MapValue(), "proxy", "direct").WithPath(addresses, "network", "addresses")),
		api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef")),
		api.NewObject(api.Network, "lab-net", api.Value{}, api.MapValue().With("provided", api.BoolValue(false)).
			WithPath(api.StringValue("lab-libvirt"), "libvirt", "providerRef").WithPath(api.StringValue(bridge), "libvirt", "hostAddress").
			WithPath(api.StringValue("192.0.2.0/24"), "ipv4", "cidr")),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, api.MapValue().With("type", api.StringValue("caBundle"))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, api.MapValue().With("type", api.StringValue("tlsCertificate")).WithPath(api.StringValue("mirror-ca"), "source", "generated", "issuerRef")),
	}
	return append(objects, services...)
}

func pinnedArtifactServer(bind string) api.Object {
	spec := api.MapValue().With("management", api.StringValue("managed")).With("machineRef", api.StringValue("controller")).
		WithPath(api.StringValue("mirror-tls"), "tls", "secretRef").
		With("listeners", api.ListValue(api.MapValue().With("name", api.StringValue("https")).With("protocol", api.StringValue("https")).With("port", api.IntegerValue("443")))).
		With("endpoints", api.ListValue(api.MapValue().With("name", api.StringValue("ip")).With("listenerRef", api.StringValue("https")).With("addressRef", api.StringValue("ip"))))
	if bind != "" {
		spec = spec.With("bindAddress", api.StringValue(bind))
	}
	return api.NewObject(api.ArtifactServer, "mirror", api.Value{}, spec)
}

func pinnedRegistry(bind string) api.Object {
	spec := api.MapValue().With("management", api.StringValue("managed")).With("machineRef", api.StringValue("controller")).With("port", api.IntegerValue("5000")).
		WithPath(api.StringValue("mirror-tls"), "tls", "secretRef").
		With("endpoints", api.ListValue(api.MapValue().With("name", api.StringValue("ip")).With("addressRef", api.StringValue("ip"))))
	if bind != "" {
		spec = spec.With("bindAddress", api.StringValue(bind))
	}
	return api.NewObject(api.Registry, "mirror-registry", api.Value{}, spec)
}

// TestAManagedMirrorABridgeCarriesRefusesInItsOwnWords pins the provider
// refusal of both managed mirrors on the controller Machine, where the
// provider it hosts is judged only by the addresses its bridges carry: the
// service's bind address and each endpoint's address.
func TestAManagedMirrorABridgeCarriesRefusesInItsOwnWords(t *testing.T) {
	artifacts := api.MapValue().With("artifacts", api.MapValue().With("serverRef", api.StringValue("mirror")).With("endpointRef", api.StringValue("ip")))
	registry := api.MapValue().With("registry", api.MapValue().With("registryRef", api.StringValue("mirror-registry")).With("endpointRef", api.StringValue("ip")))
	artifactShape := pinnedShape{"Environment/lab", "$.spec.mirror.artifacts.serverRef",
		"the artifact mirror is filled before the controller stage, and ArtifactServer/mirror on Machine/controller waits for InfraProvider/lab-libvirt, which that Machine hosts or whose bridge carries its address",
		"place ArtifactServer/mirror on a Machine that hosts no InfraProvider and whose address no provider bridges, or select an artifact mirror the operator fills in spec.mirror.artifacts.url of Environment/lab"}
	registryShape := pinnedShape{"Environment/lab", "$.spec.mirror.registry.registryRef",
		"the image mirror is filled before the emulated BMCs that pull from it, and Registry/mirror-registry on Machine/controller waits for InfraProvider/lab-libvirt, which that Machine hosts or whose bridge carries its address",
		"place Registry/mirror-registry on a Machine no InfraProvider hosts or bridges, or select an external Registry in spec.mirror.registry.registryRef of Environment/lab"}
	for _, test := range []struct {
		name    string
		objects []api.Object
		want    []pinnedShape
	}{
		{"an artifact mirror on an unbridged address", mirrorGraph(artifacts, "192.0.2.20", pinnedArtifactServer("")), nil},
		{"an artifact mirror bound to a bridged address", mirrorGraph(artifacts, "192.0.2.20", pinnedArtifactServer("192.0.2.20")), []pinnedShape{artifactShape}},
		{"an image mirror on an unbridged address", mirrorGraph(registry, "192.0.2.20", pinnedRegistry("")), nil},
		{"an image mirror bound to a bridged address", mirrorGraph(registry, "192.0.2.20", pinnedRegistry("192.0.2.20")), []pinnedShape{registryShape}},
		{"an artifact mirror whose endpoint address a bridge carries", mirrorGraph(artifacts, "192.0.2.10", pinnedArtifactServer("")), []pinnedShape{artifactShape}},
		{"an image mirror whose endpoint address a bridge carries", mirrorGraph(registry, "192.0.2.10", pinnedRegistry("")), []pinnedShape{registryShape}},
		{"both mirrors bound to a bridged address", mirrorGraph(registry.With("artifacts", artifacts.Get("artifacts")), "192.0.2.20", pinnedRegistry("192.0.2.20"), pinnedArtifactServer("192.0.2.20")),
			[]pinnedShape{registryShape, artifactShape}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got []pinnedShape
			for _, shape := range controller.Unsupported(api.NewCatalog(test.objects)) {
				got = append(got, pinnedShape{shape.Object.Identity(), shape.Field, shape.Reason, shape.Remediation})
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("shapes:\n%+v\nwant:\n%+v", got, test.want)
			}
		})
	}
}

// TestInstallerMediaToolingNeedsAnAnacondaInstallation pins what selects the
// image-building tooling beyond the server's placement: a Machine whose OS is
// installed, by an Anaconda profile, through an artifact server that resolves.
func TestInstallerMediaToolingNeedsAnAnacondaInstallation(t *testing.T) {
	anaconda := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
		WithPath(api.StringValue("lab-artifacts"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef"))
	other := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().WithPath(api.StringValue("lab-artifacts"), "installer", "other", "serverRef"))
	unresolved := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
		WithPath(api.StringValue("absent"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef"))
	installed := api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	undeclared := api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	unprofiled := api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("absent"), "os", "installProfileRef"))
	server := api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue("controller")))
	for _, test := range []struct {
		name    string
		objects []api.Object
		want    bool
	}{
		{"an Anaconda installation", []api.Object{anaconda, installed, server}, true},
		{"another installer", []api.Object{other, installed, server}, false},
		{"an unresolved artifact server", []api.Object{unresolved, installed, server}, false},
		{"no os.provided", []api.Object{anaconda, undeclared, server}, false},
		{"an unresolved profile", []api.Object{anaconda, unprofiled, server}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection, err := controller.Select(api.NewCatalog(append(selectionObjects(), test.objects...)))
			if err != nil || selection.InstallerMedia() != test.want {
				t.Fatalf("the tooling selected %v (%v)", selection.InstallerMedia(), err)
			}
		})
	}
}
