package agentinstall

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
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("domains", api.MapValue(
			text("base", "lab.example.test"), text("machines", "lab.example.test"),
			text("clusters", "lab.example.test"), text("containerClusters", "lab.example.test"),
		)),
		field("controller", api.MapValue(text("machineRef", "controller"))),
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

func artifactServer() api.Object {
	return api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "192.0.2.1"),
		field("tls", api.MapValue(text("secretRef", "artifact-server-tls"))),
		field("listeners", api.ListValue(
			api.MapValue(text("name", "https"), text("protocol", "https"), number("port", "8443")),
		)),
		field("endpoints", api.ListValue(
			api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip")),
		)),
	))
}

func nameService() api.Object {
	return api.NewObject(api.DNSServer, "lab-dns", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "192.0.2.1"),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	))
}

func timeService() api.Object {
	return api.NewObject(api.NTPServer, "lab-ntp", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "192.0.2.1"),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	))
}

func networkConfiguration() api.Object {
	return api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("dns", api.ListValue(api.MapValue(text("serverRef", "lab-dns"), text("endpointRef", "ip")))),
		field("nmstate", api.MapValue(
			field("interfaces", api.ListValue(api.MapValue(
				text("name", "enp1s0"), text("type", "ethernet"), text("state", "up"),
				field("ipv4", api.MapValue(field("enabled", api.BoolValue(true)), field("dhcp", api.BoolValue(false)))),
				field("ipv6", api.MapValue(field("enabled", api.BoolValue(false)))),
			))),
			field("routes", api.MapValue(field("config", api.ListValue(api.MapValue(
				text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"),
				text("next-hop-interface", "enp1s0"), number("table-id", "254"),
			))))),
		)),
	))
}

func libvirtProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(
			text("machineRef", "controller"), text("uri", "qemu:///system"),
			field("bmcEmulationDefaults", api.MapValue(
				text("bindAddress", "192.0.2.1"), number("port", "8000"),
				field("auth", api.MapValue(text("credentialsRef", "lab-bmc-credentials"))),
			)),
			field("machineProfiles", api.ListValue(api.MapValue(
				text("name", "sno"), number("cpu", "8"), number("memoryMiB", "16384"), number("diskGiB", "120"),
			))),
		)),
		field("networkAttachments", api.ListValue(api.MapValue(
			text("name", "lab-guests"),
			field("libvirt", api.MapValue(text("bridge", "virbr-lab"), text("management", "managed"), text("address", "198.51.100.1/24"))),
		))),
	))
}

func metalProvider() api.Object {
	return api.NewObject(api.InfraProvider, "floor", api.Value{}, api.MapValue(
		field("baremetal", api.MapValue(field("defaults", api.MapValue(
			field("bmc", api.MapValue(text("credentialsRef", "metal-bmc"))),
		)))),
	))
}

// guest is one installer-provisioned Machine on the libvirt provider: the
// substrate realizes its hardware and the cluster installer supplies its OS.
func guest(name, address string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		field("capabilities", api.StringList("openshift-node")),
		field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), text("profileRef", "sno"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)),
			field("install", api.MapValue(field("rootDeviceHints", api.MapValue(text("deviceName", "/dev/vda"))))))),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("attachmentRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", name+".lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", address), text("interface", "enp1s0")),
			)),
		)),
	))
}

// server is one operator-owned Machine on the bare-metal provider, which
// declares the hardware every later effect is proved against.
func server(name, address, mac string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		field("capabilities", api.StringList("openshift-node")),
		field("substrate", api.MapValue(text("providerRef", "floor"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)),
			field("install", api.MapValue(field("rootDeviceHints", api.MapValue(text("deviceName", "/dev/sda"))))))),
		field("hardware", api.MapValue(
			field("nics", api.ListValue(api.MapValue(text("name", "enp1s0"), text("macAddress", mac)))),
			field("boot", api.MapValue(text("nicRef", "enp1s0"))),
			field("management", api.MapValue(field("bmc", api.MapValue(
				text("address", "https://"+name+"-bmc.lab.example.test/redfish/v1/Systems/1"),
				text("credentialsRef", "metal-bmc"),
			)))),
		)),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", name+".lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", address), text("interface", "enp1s0")),
			)),
		)),
	))
}

func node(name, role, machine, fqdn string) api.Value {
	return api.MapValue(text("name", name), text("role", role), text("machineRef", machine), text("fqdn", fqdn))
}

// cluster is one declared cluster in the shape admission leaves behind, with
// its endpoints already resolved.
func cluster(name string, install api.Value, nodes ...api.Value) api.Object {
	return api.NewObject(api.ContainerCluster, name, api.Value{}, api.MapValue(
		field("distribution", api.MapValue(text("type", "openshift"),
			field("release", api.MapValue(text("version", "4.21.15"))))),
		field("install", install),
		field("nodes", api.ListValue(nodes...)),
	))
}

func installSelection(extra ...api.FieldValue) api.Value {
	fields := []api.FieldValue{
		text("method", "agent"), text("mode", "connected"),
		text("pullSecretRef", "openshift-pull-secret"),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("nodeSSH", api.MapValue(text("keyPairRef", "sno-cluster-admin-ssh-key"))),
		field("ntp", api.ListValue(api.MapValue(text("serverRef", "lab-ntp"), text("endpointRef", "ip")))),
		field("agent", api.MapValue(field("redfishVirtualMedia", api.MapValue(
			field("artifactServerEndpoint", api.MapValue(text("serverRef", "lab-artifacts"), text("endpointRef", "ip-https"))),
		)))),
	}
	return api.MapValue(append(fields, extra...)...)
}

func endpoints(api_, apiInt, ingress string, source string) api.FieldValue {
	entry := func(address string) api.Value {
		return api.MapValue(text("address", address), field("source", api.MapValue(text("type", source))))
	}
	return field("endpoints", api.MapValue(
		field("api", entry(api_)), field("api-int", entry(apiInt)), field("ingress", entry(ingress)),
	))
}

func base() []api.Object {
	return []api.Object{
		environment(), controller(), artifactServer(), nameService(), timeService(),
		networkConfiguration(), libvirtProvider(),
	}
}

// singleNodeCatalog is the shape examples/lab-sno declares: one cluster whose
// three endpoint slots resolve to its one node.
func singleNodeCatalog() api.Catalog {
	objects := append(base(), guest("sno-01", "198.51.100.21/24"))
	objects = append(objects, cluster("sno",
		installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
		node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))
	return api.NewCatalog(objects)
}

// compactCatalog is three control-plane nodes on the same libvirt provider,
// which is the smallest cluster that installs on a platform and answers at
// addresses no single node owns.
func compactCatalog() api.Catalog {
	objects := append(base(),
		guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"))
	objects = append(objects, cluster("ocp",
		installSelection(
			endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "openshift"),
			field("platform", api.MapValue(text("type", "baremetal"),
				field("baremetal", api.MapValue(text("provisioningNetwork", "disabled"))))),
		),
		node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
		node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
		node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test")))
	return api.NewCatalog(objects)
}

// withHints replaces the root-device hints a Machine declares.
func withHints(machine api.Object, hints ...api.FieldValue) api.Object {
	return machine.WithSpec(machine.Spec().WithPath(api.MapValue(hints...), "os", "install", "rootDeviceHints"))
}

// virtualHints is every root-device hint admission accepts on a Machine its
// libvirt provider creates, each string one a YAML 1.1 reader would take for a
// number, with a size of zero and a disk that is not rotational, so the frozen
// input must keep each type and value.
func virtualHints() []api.FieldValue {
	return []api.FieldValue{
		text("deviceName", "/dev/disk/by-path/pci-0000:00:04.0"), text("model", "1e3"), text("vendor", "0o17"),
		number("minSizeGigabytes", "0"), field("rotational", api.BoolValue(false)),
	}
}

// everyHint is every root-device hint admission accepts on a bare-metal
// Machine: the virtual hints, and the WWN, SCSI address and serial number a
// created disk carries none of.
func everyHint() []api.FieldValue {
	return append(virtualHints(),
		text("hctl", "1:0:0:0"), text("serialNumber", "0987654321"), text("wwn", "0x5000c500a1b2c3d4"))
}

// hintsCatalog is the compact topology whose first node declares every hint a
// virtual node is admitted with, whose second selects its disk by size alone
// and whose third names its device.
func hintsCatalog() api.Catalog {
	objects := append(base(),
		withHints(guest("ocp-01", "198.51.100.31/24"), virtualHints()...),
		withHints(guest("ocp-02", "198.51.100.32/24"), number("minSizeGigabytes", "100")),
		guest("ocp-03", "198.51.100.33/24"))
	compact, _ := compactCatalog().Find(api.ContainerCluster, "ocp")
	return api.NewCatalog(append(objects, compact))
}

// singleNodeWith is the lab-sno shape with its node declaring the given hints.
func singleNodeWith(hints ...api.FieldValue) api.Catalog {
	objects := singleNodeCatalog().Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "sno-01" {
			objects[index] = withHints(object, hints...)
		}
	}
	return api.NewCatalog(objects)
}

// externalCatalog is the compact topology with endpoints something outside the
// cluster answers, so its load balancer is user-managed. It also trusts two
// additional CA bundles, declared out of name order so the order the media
// request freezes them in is visible. It declares its networking too: the
// optional network type, and cluster and service networks other than the
// defaults, so its media request freezes the declared networking.
func externalCatalog() api.Catalog {
	objects := append(base(),
		guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"))
	declared := cluster("ocp",
		installSelection(
			endpoints("198.51.100.20", "198.51.100.20", "198.51.100.21", "external"),
			field("platform", api.MapValue(text("type", "baremetal"))),
			field("additionalTrustBundleRefs", api.StringList("lab-root-ca", "corp-proxy-ca")),
		),
		node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
		node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
		node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test"))
	declared = declared.WithSpec(declared.Spec().With("networking", api.MapValue(
		text("networkType", "OVNKubernetes"),
		field("clusterNetwork", api.ListValue(api.MapValue(text("cidr", "10.132.0.0/14"), number("hostPrefix", "23")))),
		field("serviceNetwork", api.StringList("172.31.0.0/16")),
	)))
	return api.NewCatalog(append(objects, declared))
}

// physicalCatalog is the same topology on operator-owned hardware, reached at
// the controllers those machines declare.
func physicalCatalog() api.Catalog {
	objects := append(base(), metalProvider(),
		server("metal-01", "198.51.100.41/24", "aa:bb:cc:dd:ee:01"),
		server("metal-02", "198.51.100.42/24", "aa:bb:cc:dd:ee:02"),
		server("metal-03", "198.51.100.43/24", "aa:bb:cc:dd:ee:03"))
	objects = append(objects, cluster("metal",
		installSelection(
			endpoints("198.51.100.20", "198.51.100.20", "198.51.100.21", "external"),
			field("platform", api.MapValue(text("type", "baremetal"))),
		),
		node("master-0", "master", "metal-01", "master-0.metal.lab.example.test"),
		node("master-1", "master", "metal-02", "master-1.metal.lab.example.test"),
		node("master-2", "master", "metal-03", "master-2.metal.lab.example.test")))
	return api.NewCatalog(objects)
}

// physicalHintsCatalog is the physical topology whose first node declares
// every hint, whose second selects its disk by wwn alone and whose third names
// its device.
func physicalHintsCatalog() api.Catalog {
	objects := physicalCatalog().Objects()
	for index, object := range objects {
		switch {
		case object.Kind() == api.Machine && object.Name() == "metal-01":
			objects[index] = withHints(object, everyHint()...)
		case object.Kind() == api.Machine && object.Name() == "metal-02":
			objects[index] = withHints(object, text("wwn", "0x5000c500a1b2c3d5"))
		}
	}
	return api.NewCatalog(objects)
}
