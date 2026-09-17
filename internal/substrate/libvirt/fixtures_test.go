package libvirt

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func number(name, value string) api.FieldValue { return field(name, api.IntegerValue(value)) }

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

// remoteHost is a provider host reached over SSH, with exactly the access a
// lifecycle placement requires.
func remoteHost() api.Object {
	return api.NewObject(api.Machine, "hypervisor", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("libvirt")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "hypervisor.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.5")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("user", "operator"), text("addressRef", "ip"), text("knownHostsRef", "host-key"),
			field("auth", api.MapValue(text("privateKeyRef", "hypervisor-key"))),
		)))),
	))
}

func provider(fields ...api.FieldValue) api.Object {
	libvirtArm := api.MapValue(
		text("machineRef", "controller"),
		text("uri", "qemu:///system"),
		field("bmcEmulationDefaults", api.MapValue(
			field("enabled", api.BoolValue(true)), text("protocol", "redfish"), text("emulator", "sushy-tools"),
			text("bindAddress", "192.0.2.1"), number("port", "8000"),
			field("auth", api.MapValue(text("credentialsRef", "lab-bmc-credentials"))),
		)),
		field("machineProfiles", api.ListValue(api.MapValue(
			text("name", "rhel"), number("cpu", "4"), number("memoryMiB", "8192"), number("diskGiB", "60"),
			field("tpm", api.MapValue()),
		))),
	)
	spec := api.MapValue(
		field("libvirt", libvirtArm),
		field("networkAttachments", api.ListValue(api.MapValue(
			text("name", "lab-guests"),
			field("libvirt", api.MapValue(
				text("bridge", "virbr-lab"), text("management", "managed"),
				text("address", "198.51.100.1/24"), text("forward", "nat"),
			)),
		))),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, spec)
}

func networkConfig() api.Object {
	return api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("nmstate", api.MapValue(
			field("interfaces", api.ListValue(api.MapValue(
				text("name", "enp1s0"), text("type", "ethernet"), text("state", "up"),
			))),
			field("routes", api.MapValue(field("config", api.ListValue(api.MapValue(
				text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"),
				text("next-hop-interface", "enp1s0"),
			))))),
		)),
	))
}

func guest(name string, fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), text("profileRef", "rhel"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", "rhel-9-8"),
			field("install", api.MapValue(field("rootDeviceHints", api.MapValue(text("deviceName", "/dev/vda"))))))),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("attachmentRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", name+".lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24"), text("interface", "enp1s0")),
			)),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.Machine, name, api.Value{}, spec)
}

func catalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func labCatalog() api.Catalog {
	return catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"))
}
