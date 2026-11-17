package installfixtures

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const TestContext = "lab"

func Field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func Text(name, value string) api.FieldValue { return Field(name, api.StringValue(value)) }

func Number(name, value string) api.FieldValue { return Field(name, api.IntegerValue(value)) }

func Environment() api.Object {
	return api.NewObject(api.Environment, "lab-rhel", api.Value{}, api.MapValue(
		Field("domains", api.MapValue(Text("base", "lab.example.test"))),
		Field("controller", api.MapValue(Text("machineRef", "controller"))),
		Field("remoteMachinesAccessKey", api.MapValue(Text("keyRef", "bootwright-machine-key"))),
	))
}

func Controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		Field("os", api.MapValue(Field("provided", api.BoolValue(true)))),
		Field("network", api.MapValue(Field("addresses", api.ListValue(
			api.MapValue(Text("name", "fqdn"), Text("address", "controller.lab.example.test")),
			api.MapValue(Text("name", "ip"), Text("address", "192.0.2.1")),
		)))),
		Field("access", api.MapValue(Field("local", api.BoolValue(true)))),
	))
}

func Provider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		Field("libvirt", api.MapValue(
			Text("machineRef", "controller"), Text("uri", "qemu:///system"),
			Field("bmcEmulation", api.MapValue(
				Text("certificateRef", "lab-bmc-tls"),
				Text("bindAddress", "192.0.2.1"), Number("port", "8000"),
				Field("auth", api.MapValue(Text("credentialsRef", "lab-bmc-credentials"))),
			)),
			Field("machineProfiles", api.ListValue(api.MapValue(
				Text("name", "rhel"), Number("cpu", "4"), Number("memoryMiB", "8192"), Number("diskGiB", "60"),
			))),
		)),
	))
}

func LabNetwork() api.Object {
	return api.NewObject(api.Network, "lab-guests", api.Value{}, api.MapValue(
		Field("provided", api.BoolValue(false)), Number("mtu", "1500"),
		Field("ipv4", api.MapValue(Text("cidr", "198.51.100.0/24"))),
		Field("libvirt", api.MapValue(Text("providerRef", "lab-libvirt"), Text("bridge", "virbr-lab"), Text("forward", "nat"), Text("hostAddress", "198.51.100.1"))),
	))
}

func LabLAN() api.Object {
	return api.NewObject(api.Network, "lab-lan", api.Value{}, api.MapValue(
		Field("provided", api.BoolValue(true)), Field("ipv4", api.MapValue(Text("cidr", "198.51.100.0/24"))),
	))
}

func ArtifactServer(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		Text("management", "managed"), Text("machineRef", "controller"), Text("bindAddress", "192.0.2.1"),
		Field("tls", api.MapValue(Text("secretRef", "lab-artifacts-tls"))),
		Field("listeners", api.ListValue(
			api.MapValue(Text("name", "https"), Text("protocol", "https"), Number("port", "8443")),
			api.MapValue(Text("name", "http"), Text("protocol", "http"), Number("port", "8080")),
		)),
		Field("endpoints", api.ListValue(
			api.MapValue(Text("name", "ip-https"), Text("listenerRef", "https"), Text("addressRef", "ip")),
			api.MapValue(Text("name", "ip-http"), Text("listenerRef", "http"), Text("addressRef", "ip")),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, spec)
}

func ServicesHost() api.Object {
	return api.NewObject(api.Machine, "services", api.Value{}, api.MapValue(
		Field("os", api.MapValue(Field("provided", api.BoolValue(true)))),
		Field("network", api.MapValue(Field("addresses", api.ListValue(
			api.MapValue(Text("name", "fqdn"), Text("address", "services.lab.example.test")),
			api.MapValue(Text("name", "ip"), Text("address", "192.0.2.2")),
		)))),
		Field("access", api.MapValue(Field("ssh", api.MapValue(
			Text("addressRef", "ip"), Number("port", "2222"), Text("user", "root"),
			Field("auth", api.MapValue(Text("privateKeyRef", "services-ssh-key"))),
			Text("knownHostsRef", "services-known-hosts"),
		)))),
	))
}

func Service(kind api.Kind, name, port string) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(
		Text("management", "managed"), Text("machineRef", "controller"), Text("bindAddress", "192.0.2.1"),
		Number("port", port),
		Field("endpoints", api.ListValue(api.MapValue(Text("name", "ip"), Text("addressRef", "ip")))),
	))
}

func NetworkConfig() api.Object {
	return api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		Field("dns", api.ListValue(api.MapValue(Text("serverRef", "lab-dns"), Text("endpointRef", "ip")))),
		Field("nmstate", api.MapValue(
			Field("interfaces", api.ListValue(api.MapValue(Text("name", "enp1s0"), Text("type", "ethernet")))),
			Field("routes", api.MapValue(Field("config", api.ListValue(api.MapValue(
				Text("destination", "0.0.0.0/0"), Text("next-hop-address", "198.51.100.1"),
			))))),
		)),
	))
}

func BootImage() api.Object {
	return api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		Text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
	))
}

func InstallProfile(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		Field("os", api.MapValue(Text("family", "rhel"), Text("version", "9.8"), Text("architecture", "x86_64"))),
		Field("installer", api.MapValue(Field("anaconda", api.MapValue(
			Text("imageRef", "rhel-9-8-boot"),
			Field("redfishVirtualMedia", api.MapValue(Field("artifactServerEndpoint", api.MapValue(
				Text("serverRef", "lab-artifacts"), Text("endpointRef", "ip-https"),
			)))),
			Field("packageSource", api.MapValue(Field("hostedTree", api.MapValue(
				Text("fromMedia", "local-media:rhel-9.8-x86_64-dvd.iso"),
				Field("artifactServerEndpoint", api.MapValue(
					Text("serverRef", "lab-artifacts"), Text("endpointRef", "ip-http"),
				)),
			)))),
		)))),
		Field("ntp", api.ListValue(api.MapValue(Text("serverRef", "lab-ntp"), Text("endpointRef", "ip")))),
		Field("updates", api.MapValue(Text("policy", "none"))),
		Field("customizations", api.MapValue(
			Field("localization", api.MapValue(
				Text("language", "en_US.UTF-8"), Text("keyboard", "us"), Text("timezone", "Etc/UTC"),
			)),
			Field("packages", api.MapValue(
				Text("environment", "minimal"), Field("excludeDocs", api.BoolValue(true)),
				Field("installWeakDeps", api.BoolValue(false)),
				Field("install", api.StringList("chrony")),
			)),
			Field("services", api.MapValue(Field("enabled", api.StringList("chronyd")))),
			Field("security", api.MapValue(Field("selinux", api.MapValue(Text("mode", "enforcing"))))),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.MachineInstallProfile, "rhel-9-8", api.Value{}, spec)
}

func Guest(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		Field("substrate", api.MapValue(Text("providerRef", "lab-libvirt"), Field("libvirt", api.MapValue(Text("profileRef", "rhel"))))),
		Field("hardware", api.MapValue(Field("nics", api.ListValue(api.MapValue(Text("name", "enp1s0")))), Field("rootDevice", api.MapValue(Text("deviceName", "/dev/vda"))))),
		Field("os", api.MapValue(
			Field("provided", api.BoolValue(false)), Text("installProfileRef", "rhel-9-8"),
		)),
		Field("network", api.MapValue(
			Text("configRef", "lab-guests"), Text("installAddressRef", "ip"),
			Field("addresses", api.ListValue(
				api.MapValue(Text("name", "fqdn"), Text("address", "rhel-01.lab.example.test")),
				api.MapValue(Text("name", "ip"), Text("address", "198.51.100.11"), Text("interface", "enp1s0")),
			)),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.Machine, "rhel-01", api.Value{}, spec)
}

func MetalProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-metal", api.Value{}, api.MapValue(
		Field("baremetal", api.MapValue(Field("defaults", api.MapValue(
			Field("bmc", api.MapValue(Text("credentialsRef", "lab-bmc-credentials"))),
		)))),
	))
}

func Server(hints api.Value) api.Object {
	declared, physical := SplitRootDevice(hints)
	baremetal := api.MapValue(
		Field("bmc", api.MapValue(
			Text("address", "https://bmc-01.lab.example.test/redfish/v1/Systems/1"),
			Text("credentialsRef", "lab-bmc-credentials"),
		)),
		Field("nics", api.ListValue(api.MapValue(Text("nicRef", "enp1s0"), Text("macAddress", "52:54:00:9a:1b:01")))),
	)
	if len(physical.Fields()) != 0 {
		baremetal = baremetal.With("rootDevice", physical)
	}
	return api.NewObject(api.Machine, "metal-01", api.Value{}, api.MapValue(
		Field("substrate", api.MapValue(Text("providerRef", "lab-metal"), Field("baremetal", baremetal))),
		Field("os", api.MapValue(
			Field("provided", api.BoolValue(false)), Text("installProfileRef", "rhel-9-8"),
			Field("install", api.MapValue(Text("hostKeyRef", "metal-01-host-key"))),
		)),
		Field("hardware", api.MapValue(
			Field("nics", api.ListValue(api.MapValue(Text("name", "enp1s0")))),
			Field("boot", api.MapValue(Text("nicRef", "enp1s0"))),
			Field("rootDevice", declared),
		)),
		Field("network", api.MapValue(
			Text("configRef", "lab-guests"), Text("installAddressRef", "ip"),
			Field("addresses", api.ListValue(
				api.MapValue(Text("name", "fqdn"), Text("address", "metal-01.lab.example.test")),
				api.MapValue(Text("name", "ip"), Text("address", "198.51.100.41"), Text("interface", "enp1s0")),
			)),
		)),
	))
}

func SplitRootDevice(hints api.Value) (declared, physical api.Value) {
	declared, physical = api.MapValue(), api.MapValue()
	for _, hint := range hints.Fields() {
		switch hint.Name {
		case "wwn", "hctl", "serialNumber", "sizeBytes":
			physical = physical.With(hint.Name, hint.Value)
		default:
			declared = declared.With(hint.Name, hint.Value)
		}
	}
	return declared, physical
}

func CatalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func LabCatalog(overrides ...api.Object) api.Catalog {
	objects := []api.Object{
		Environment(), Controller(), Provider(), LabNetwork(), NetworkConfig(), ArtifactServer(),
		Service(api.DNSServer, "lab-dns", "53"), Service(api.NTPServer, "lab-ntp", "123"),
		BootImage(), InstallProfile(), Guest(),
	}
	for _, override := range overrides {
		replaced := false
		for index, object := range objects {
			if object.Kind() == override.Kind() && object.Name() == override.Name() {
				objects[index], replaced = override, true
			}
		}
		if !replaced {
			objects = append(objects, override)
		}
	}
	for _, object := range objects {
		for _, attachment := range object.Spec().Get("segments").Items() {
			if attachment.Get("name").Text() == "lab-guests" {
				objects = slices.DeleteFunc(objects, func(o api.Object) bool { return o.Identity() == "Network/lab-guests" })
				if !slices.ContainsFunc(objects, func(o api.Object) bool { return o.Identity() == "Network/lab-lan" }) {
					objects = append(objects, LabLAN())
				}
			}
		}
	}
	return api.NewCatalog(WithGuestNICs(objects))
}

func WithGuestNICs(objects []api.Object) []api.Object {
	ethernet := map[string][]api.Value{}
	for _, object := range objects {
		if object.Kind() == api.NetworkConfig {
			for _, iface := range object.Spec().Get("nmstate", "interfaces").Items() {
				if iface.Get("type").Text() == "ethernet" {
					ethernet[object.Name()] = append(ethernet[object.Name()], api.MapValue(Text("name", iface.Get("name").Text())))
				}
			}
		}
	}
	for index, object := range objects {
		names := slices.Clone(ethernet[object.Spec().Get("network", "configRef").Text()])
		for _, iface := range object.Spec().Get("network", "overrides", "interfaces").Items() {
			if named := api.MapValue(Text("name", iface.Get("name").Text())); iface.Get("type").Text() == "ethernet" && !slices.ContainsFunc(names, named.Equal) {
				names = append(names, named)
			}
		}
		if object.Kind() == api.Machine && object.Spec().Has("substrate", "libvirt") && len(names) > 0 {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.ListValue(names...), "hardware", "nics"))
		}
	}
	return objects
}

func FleetMaterial() map[string]secrets.Material {
	return map[string]secrets.Material{
		"lab-bmc-credentials": secrets.NewMaterial(nil),
		"bootwright-machine-key": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.PublicKeyPart:  []byte("ssh-ed25519 AAAAPUBLIC bootwright\n"),
			secrets.PrivateKeyPart: []byte("PRIVATE"),
		}),
	}
}

func PlanInput(verb reconciliation.Verb) lifecycle.PlanInput {
	catalog := LabCatalog()
	return lifecycle.PlanInput{
		Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: TestContext},
	}
}

func Realizing(references []reconciliation.ObjectRef) []reconciliation.BlockDefinition {
	definitions := make([]reconciliation.BlockDefinition, 0, len(references))
	for index, reference := range references {
		definitions = append(definitions, reconciliation.BlockDefinition{
			ID:          "realizes-" + strings.ToLower(reference.Kind) + "-" + reference.Object,
			Description: "realize " + reference.Object, Stage: reconciliation.StageInfrastructureServices,
			Kind: reference.Kind, Object: reference.Object, Implementation: "placeholder", Family: reference.Family,
			ContentDigest: strings.Repeat("a", 64),
			Request:       json.RawMessage(`{"index":` + string(rune('0'+index)) + `}`),
		})
	}
	return definitions
}

func SSHKeySecret(source api.Value) api.Object {
	declared := api.NewObject(api.Secret, "metal-01-host-key", api.Value{}, api.MapValue(
		Text("type", "sshKeyPair"), Field("source", source),
	))
	normalized, _ := secrets.Normalize(declared, api.Catalog{})
	return normalized
}

func ExpectRefusal(t *testing.T, err error, code string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code {
		t.Fatalf("refusal = %#v, want %s", reported, code)
	}
}

func EraseMachine() api.Object {
	machine := Server(api.MapValue(Text("wwn", "0x5000c500a1b2c3d4")))
	hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(Text("method", "logicalOverwrite"), Field("targets", api.ListValue(api.MapValue(Text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"), Text("wwn", "0x5000c500a1b2c3d4"), Text("serialNumber", "serial-example"), Number("sizeBytes", "8192"))))))
	return api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
}

func DeclaredInterface(name, kind string, fields ...api.FieldValue) api.Value {
	return api.MapValue(append([]api.FieldValue{Text("name", name), Text("type", kind)}, fields...)...)
}

func HardwareNIC(name, mac string) api.Value {
	return api.MapValue(Text("name", name), Text("macAddress", mac))
}

func MetalOn(iface string, nics ...api.Value) api.Object {
	metal := Server(api.MapValue(Text("wwn", "0x5000c500a1b2c3d4")))
	var names, addresses []api.Value
	for _, nic := range nics {
		names = append(names, api.MapValue(Text("name", nic.Get("name").Text())))
		addresses = append(addresses, api.MapValue(Text("nicRef", nic.Get("name").Text()), Text("macAddress", nic.Get("macAddress").Text())))
	}
	return metal.WithSpec(metal.Spec().
		WithPath(api.ListValue(names...), "hardware", "nics").
		WithPath(api.ListValue(addresses...), "substrate", "baremetal", "nics").
		WithPath(api.StringValue("metal-net"), "network", "configRef").
		WithPath(api.ListValue(
			api.MapValue(Text("name", "fqdn"), Text("address", "metal-01.lab.example.test")),
			api.MapValue(Text("name", "ip"), Text("address", "198.51.100.41"), Text("interface", iface)),
		), "network", "addresses"))
}

func MetalNetwork(interfaces ...api.Value) api.Object {
	return api.NewObject(api.NetworkConfig, "metal-net", api.Value{}, NetworkWith(interfaces...).Spec())
}

func MetalCatalog(metal, network api.Object) api.Catalog {
	return LabCatalog(MetalProvider(), metal, network, SSHKeySecret(api.MapValue(Field("generated", api.MapValue()))))
}

func NetworkWith(interfaces ...api.Value) api.Object {
	config := NetworkConfig()
	return config.WithSpec(config.Spec().WithPath(api.ListValue(interfaces...), "nmstate", "interfaces"))
}
