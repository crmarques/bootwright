package libvirt

import (
	"net/netip"

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
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "hypervisor.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.5")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("user", "root"), text("addressRef", "ip"), text("knownHostsRef", "host-key"),
			field("auth", api.MapValue(text("privateKeyRef", "hypervisor-key"))),
		)))),
	))
}

func provider(fields ...api.FieldValue) api.Object {
	libvirtArm := api.MapValue(
		text("machineRef", "controller"),
		text("uri", "qemu:///system"),
		field("bmcEmulation", api.MapValue(
			text("bindAddress", "192.0.2.1"), number("port", "8000"), text("certificateRef", "lab-bmc-tls"),
			field("auth", api.MapValue(text("credentialsRef", "lab-bmc-credentials"))),
		)),
		field("machineProfiles", api.ListValue(api.MapValue(
			text("name", "rhel"), number("cpu", "4"), number("memoryMiB", "8192"), number("diskGiB", "60"),
			field("tpm", api.MapValue()),
		))),
	)
	spec := api.MapValue(field("libvirt", libvirtArm))
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
		field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), field("libvirt", api.MapValue(text("profileRef", "rhel"),
			field("nics", api.ListValue(api.MapValue(text("nicRef", "enp1s0"), text("segmentRef", "lab-guests")))))))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", "rhel-9-8"))),
		field("hardware", api.MapValue(field("nics", api.ListValue(api.MapValue(text("name", "enp1s0")))), field("rootDevice", api.MapValue(text("deviceName", "/dev/vda"))))),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("installAddressRef", "ip"),
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

// managedNetwork is a Network lab-libvirt defines, its host address and
// prefix written as one address.
func managedNetwork(name, bridge, address string, fields ...api.FieldValue) api.Object {
	prefix := netip.MustParsePrefix(address)
	family := "ipv4"
	if prefix.Addr().Is6() {
		family = "ipv6"
	}
	spec := api.MapValue(field("provided", api.BoolValue(false)), number("mtu", "1500"),
		field(family, api.MapValue(text("cidr", prefix.Masked().String()))),
		field("libvirt", api.MapValue(text("providerRef", "lab-libvirt"), text("bridge", bridge), text("forward", "nat"), text("hostAddress", prefix.Addr().String()))))
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.Network, name, api.Value{}, spec)
}

func labNetwork() api.Object { return managedNetwork("lab-guests", "virbr-lab", "198.51.100.1/24") }

// catalogOf adds the lab-guests managed Network a fixture provider's guests
// attach to, unless the objects declare a Network or attach lab-guests to an
// existing bridge themselves.
func catalogOf(objects ...api.Object) api.Catalog {
	for _, o := range objects {
		if o.Kind() == api.Network {
			return api.NewCatalog(objects)
		}
		for _, attachment := range o.Spec().Get("segments").Items() {
			if attachment.Get("name").Text() == "lab-guests" {
				return api.NewCatalog(objects)
			}
		}
	}
	return api.NewCatalog(append(objects, labNetwork()))
}

func labCatalog() api.Catalog {
	return catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"))
}
