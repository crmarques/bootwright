package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func number(name, value string) api.FieldValue { return field(name, api.IntegerValue(value)) }

func environment() api.Object {
	return api.NewObject(api.Environment, "lab-rhel", api.Value{}, api.MapValue(
		field("domains", api.MapValue(text("base", "lab.example.test"))),
		field("controller", api.MapValue(text("machineRef", "controller"))),
		field("remoteMachinesAccessKey", api.MapValue(text("keyRef", "bootwright-machine-key"))),
	))
}

func controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("container-runtime", "libvirt")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "controller.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.1")),
		)))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
}

func provider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(
			text("machineRef", "controller"), text("uri", "qemu:///system"),
			field("bmcEmulationDefaults", api.MapValue(
				text("bindAddress", "192.0.2.1"), number("port", "8000"),
				field("auth", api.MapValue(text("credentialsRef", "lab-bmc-credentials"))),
			)),
			field("machineProfiles", api.ListValue(api.MapValue(
				text("name", "rhel"), number("cpu", "4"), number("memoryMiB", "8192"), number("diskGiB", "60"),
			))),
		)),
		field("networkAttachments", api.ListValue(api.MapValue(
			text("name", "lab-guests"),
			field("libvirt", api.MapValue(text("bridge", "virbr-lab"), text("management", "managed"), text("address", "198.51.100.1/24"))),
		))),
	))
}

func artifactServer(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "192.0.2.1"),
		field("tls", api.MapValue(text("secretRef", "lab-artifacts-tls"))),
		field("listeners", api.ListValue(
			api.MapValue(text("name", "https"), text("protocol", "https"), number("port", "8443")),
			api.MapValue(text("name", "http"), text("protocol", "http"), number("port", "8080")),
		)),
		field("endpoints", api.ListValue(
			api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip")),
			api.MapValue(text("name", "ip-http"), text("listenerRef", "http"), text("addressRef", "ip")),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, spec)
}

// servicesHost is an operator-provided Machine reached over SSH as root. It
// declares every field a lifecycle placement freezes, so a server placed on it
// populates the whole SSH arm.
func servicesHost() api.Object {
	return api.NewObject(api.Machine, "services", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("container-runtime")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "services.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.2")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("addressRef", "ip"), number("port", "2222"), text("user", "root"),
			field("auth", api.MapValue(text("privateKeyRef", "services-ssh-key"))),
			text("knownHostsRef", "services-known-hosts"),
		)))),
	))
}

func service(kind api.Kind, name, port string) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "192.0.2.1"),
		number("port", port),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	))
}

func networkConfig() api.Object {
	return api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("dns", api.ListValue(api.MapValue(text("serverRef", "lab-dns"), text("endpointRef", "ip")))),
		field("nmstate", api.MapValue(
			field("interfaces", api.ListValue(api.MapValue(text("name", "enp1s0"), text("type", "ethernet")))),
			field("routes", api.MapValue(field("config", api.ListValue(api.MapValue(
				text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"),
			))))),
		)),
	))
}

func bootImage() api.Object {
	return api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
	))
}

func installProfile(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		field("os", api.MapValue(text("family", "rhel"), text("version", "9.8"), text("architecture", "x86_64"))),
		field("installer", api.MapValue(field("anaconda", api.MapValue(
			text("imageRef", "rhel-9-8-boot"),
			field("redfishVirtualMedia", api.MapValue(field("artifactServerEndpoint", api.MapValue(
				text("serverRef", "lab-artifacts"), text("endpointRef", "ip-https"),
			)))),
			field("packageSource", api.MapValue(field("hostedTree", api.MapValue(
				text("fromMedia", "local-media:rhel-9.8-x86_64-dvd.iso"),
				field("artifactServerEndpoint", api.MapValue(
					text("serverRef", "lab-artifacts"), text("endpointRef", "ip-http"),
				)),
			)))),
		)))),
		field("ntp", api.ListValue(api.MapValue(text("serverRef", "lab-ntp"), text("endpointRef", "ip")))),
		field("customizations", api.MapValue(
			field("localization", api.MapValue(
				text("language", "en_US.UTF-8"), text("keyboard", "us"), text("timezone", "Etc/UTC"),
			)),
			field("packages", api.MapValue(
				text("environment", "minimal"), field("excludeDocs", api.BoolValue(true)),
				field("installWeakDeps", api.BoolValue(false)),
				field("install", api.StringList("chrony")),
			)),
			field("services", api.MapValue(field("enabled", api.StringList("chronyd")))),
			field("security", api.MapValue(field("selinux", api.MapValue(text("mode", "enforcing"))))),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.MachineInstallProfile, "rhel-9-8", api.Value{}, spec)
}

func guest(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), text("profileRef", "rhel"))),
		field("os", api.MapValue(
			field("provided", api.BoolValue(false)), text("installProfileRef", "rhel-9-8"),
			field("install", api.MapValue(field("rootDeviceHints", api.MapValue(text("deviceName", "/dev/vda"))))),
		)),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("attachmentRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", "rhel-01.lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24"), text("interface", "enp1s0")),
			)),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.Machine, "rhel-01", api.Value{}, spec)
}

func metalProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-metal", api.Value{}, api.MapValue(
		field("baremetal", api.MapValue(field("defaults", api.MapValue(
			field("bmc", api.MapValue(text("credentialsRef", "lab-bmc-credentials"))),
		)))),
	))
}

// server is one operator-owned Machine on the bare-metal provider, selecting
// its root device by the hints given.
func server(hints api.Value) api.Object {
	return api.NewObject(api.Machine, "metal-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-metal"))),
		field("os", api.MapValue(
			field("provided", api.BoolValue(false)), text("installProfileRef", "rhel-9-8"),
			field("install", api.MapValue(text("hostKeyRef", "metal-01-host-key"), field("rootDeviceHints", hints))),
		)),
		field("hardware", api.MapValue(
			field("nics", api.ListValue(api.MapValue(text("name", "enp1s0"), text("macAddress", "52:54:00:9a:1b:01")))),
			field("boot", api.MapValue(text("nicRef", "enp1s0"))),
			field("management", api.MapValue(field("bmc", api.MapValue(
				text("address", "https://bmc-01.lab.example.test/redfish/v1/Systems/1"),
				text("credentialsRef", "lab-bmc-credentials"),
			)))),
		)),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", "metal-01.lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", "198.51.100.41/24"), text("interface", "enp1s0")),
			)),
		)),
	))
}

func catalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func labCatalog(overrides ...api.Object) api.Catalog {
	objects := []api.Object{
		environment(), controller(), provider(), networkConfig(), artifactServer(),
		service(api.DNSServer, "lab-dns", "53"), service(api.NTPServer, "lab-ntp", "123"),
		bootImage(), installProfile(), guest(),
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
	return api.NewCatalog(objects)
}
