package v1alpha1

func init() {
	register(Machine, machineSchema)
	register(NetworkConfig, machineNetworkSchema)
	register(InfraProvider, machineProviderSchema)
	register(MachineImage, machineImageSchema)
	register(MachineInstallProfile, machineInstallSchema)
}

func machineVirtualMediaSchema() *Shape {
	tls := record(field("trust", enumeration("disable-verification", "import-certificate", "established")), field("restoreVerificationAfterBoot", boolean()), field("removeCertificateAfterBoot", boolean()))
	tls.Suppress = []Suppression{
		{Field: "trust", Value: StringValue("disable-verification"), Fields: []string{"removeCertificateAfterBoot"}},
		{Field: "trust", Value: StringValue("import-certificate"), Fields: []string{"restoreVerificationAfterBoot"}},
		{Field: "trust", Value: StringValue("established"), Fields: []string{"restoreVerificationAfterBoot", "removeCertificateAfterBoot"}},
	}
	return record(field("tls", tls))
}

func machineBMCSchema() *Shape {
	return record(required("address", nonempty()), field("protocol", enumeration("redfish")), field("credentialsRef", secret("usernamePassword")), field("tls", record(field("verify", boolean()))), field("virtualMedia", machineVirtualMediaSchema()))
}

func machineSchema() *Shape {
	ssh := record(field("addressRef", nonempty()), field("port", port()), field("user", lexical("posix-user")), required("auth", atomic(choice(field("operatorIdentity", record()), field("privateKeyRef", secret("sshKeyPair")), field("passwordRef", secret("usernamePassword"))))), field("sudoPasswordRef", secret("usernamePassword")), field("knownHostsRef", secret("opaque")))
	access := choice(field("local", boolean()), field("ssh", ssh))
	access.AllowEmpty = true
	access.Fields = append(access.Fields, field("rootLogin", enumeration("keep", "revoke")))
	network := choice(field("configRef", ref(NetworkConfig)), field("inline", machineNetworkSchema()))
	network.AllowEmpty = true
	network.Fields = append(network.Fields,
		field("attachmentRef", nonempty()), field("interfaceAttachments", &Shape{Type: Sequence, Atomic: true, NameKey: "interface", Element: record(required("interface", nonempty()), required("attachmentRef", nonempty()))}),
		field("installAddressRef", nonempty()), field("addresses", named(record(required("name", nonempty()), required("address", lexical("address")), field("interface", nonempty())))),
		field("interfaceBinding", &Shape{Type: Sequence, Atomic: true, NameKey: "nicRef", Element: record(required("nicRef", nonempty()), required("interfaceName", nonempty()))}), field("overrides", native()))
	return record(
		defaulted("capabilities", set(enumeration("openshift-node", "ceph-node", "ceph-arbiter", "container-runtime", "libvirt")), ListValue()),
		field("placement", record(field("site", name()))),
		field("substrate", record(field("providerRef", ref(InfraProvider)), field("profileRef", nonempty()))),
		field("hardware", record(field("nics", named(record(required("name", nonempty()), field("macAddress", lexical("mac"))))), field("boot", record(field("nicRef", nonempty()))), field("management", record(field("bmc", machineBMCSchema()))))),
		required("os", record(required("provided", boolean()), field("installProfileRef", ref(MachineInstallProfile)), field("install", record(field("ntp", serverSelections(NTPServer)), field("rootDeviceHints", record(field("deviceName", lexical("device-path")), field("hctl", nonempty()), field("model", nonempty()), field("vendor", nonempty()), field("serialNumber", nonempty()), field("minSizeGigabytes", integer("0", "")), field("wwn", nonempty()), field("rotational", boolean()))))))),
		field("proxy", proxySelection()),
		field("network", network), field("access", access),
	)
}

func machineNetworkSchema() *Shape {
	return record(required("machineNetwork", nonemptyArray(set(record(required("cidr", cidr()))))), field("dns", serverSelections(DNSServer)), required("nmstate", native()))
}

func machineProfileSchema(variant string) *Shape {
	fields := []Field{required("name", nonempty()), defaulted("cpu", integer("0", ""), IntegerValue("0")), defaulted("memoryMiB", integer("0", ""), IntegerValue("0")), defaulted("diskGiB", integer("0", ""), IntegerValue("0"))}
	if variant == "vsphere" {
		fields = append(fields, field("template", nonempty()), field("failureDomainRef", nonempty()))
	}
	if variant != "kubevirt" {
		fields = append(fields, field("dataDisks", named(record(required("name", nonempty()), required("sizeGiB", integer("1", ""))))))
	}
	if variant == "libvirt" {
		fields = append(fields, field("tpm", record()))
	} else if variant == "kubevirt" {
		fields = append(fields, field("tpm", record(defaulted("persistent", boolean(), BoolValue(true)))))
	}
	return named(record(fields...))
}

func machineProviderSchema() *Shape {
	baremetal := record(field("boot", record(field("method", nonempty()))), field("defaults", record(field("bmc", record(field("credentialsRef", secret("usernamePassword")), field("tls", record(field("verify", boolean()))), field("virtualMedia", machineVirtualMediaSchema()))))))
	libvirt := record(required("machineRef", ref(Machine)), required("uri", nonempty()), required("bmcEmulationDefaults", record(defaulted("enabled", boolean(), BoolValue(true)), defaulted("protocol", enumeration("redfish"), StringValue("redfish")), defaulted("emulator", enumeration("sushy-tools"), StringValue("sushy-tools")), defaulted("bindAddress", ip(), StringValue("0.0.0.0")), defaulted("port", port(), IntegerValue("8000")), required("auth", record(required("credentialsRef", secret("usernamePassword")))), defaulted("disableCertificateVerification", boolean(), BoolValue(false)))), defaulted("machineProfiles", machineProfileSchema("libvirt"), ListValue()))
	vcenters := nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "server", Element: record(required("server", lexical("host")), field("port", integer("0", "65535")), required("datacenters", nonemptyArray(set(nonempty()))), required("credentialsRef", secret("usernamePassword")), defaulted("disableCertificateVerification", boolean(), BoolValue(false)))})
	topology := record(required("datacenter", nonempty()), required("computeCluster", nonempty()), required("datastore", nonempty()), required("networks", nonemptyArray(set(nonempty()))), field("folder", nonempty()), field("resourcePool", nonempty()))
	networking := record(field("external", record(field("networkSubnetCidr", set(cidr())))), field("internal", record(field("networkSubnetCidr", set(cidr())))))
	vsphere := record(required("vcenters", vcenters), required("failureDomains", nonemptyArray(named(record(required("name", nonempty()), required("region", nonempty()), required("zone", nonempty()), required("server", nonempty()), required("topology", topology))))), field("nodeNetworking", networking), field("isoStaging", record(field("datastore", nonempty()), field("folder", nonempty()))), defaulted("machineProfiles", machineProfileSchema("vsphere"), ListValue()))
	kubevirt := choice(field("hostClusterRef", ref(ContainerCluster)), field("kubeconfigRef", secret("opaque")))
	kubevirt.Fields = append(kubevirt.Fields, required("namespace", name()), field("storageClassRef", nonempty()), defaulted("machineProfiles", machineProfileSchema("kubevirt"), ListValue()))
	libvirtAttachment := record(required("bridge", nonempty()), defaulted("management", enumeration("managed", "external"), StringValue("external")), field("address", lexical("address")), field("forward", enumeration("nat", "none")))
	libvirtAttachment.Suppress = []Suppression{{Field: "management", Value: StringValue("external"), Fields: []string{"address", "forward"}}}
	attachment := choice(field("baremetal", record(field("vlan", integer("0", "4094")))), field("libvirt", libvirtAttachment), field("vsphere", record(required("portgroup", nonempty()), field("distributedSwitch", nonempty()))), field("kubevirt", record(required("networkRef", record(field("apiGroup", text()), field("kind", nonempty()), required("name", nonempty()), field("namespace", name()))))))
	attachment.Fields = append([]Field{required("name", nonempty())}, attachment.Fields...)
	provider := choice(field("baremetal", baremetal), field("libvirt", libvirt), field("vsphere", vsphere), field("kubevirt", kubevirt))
	provider.Fields = append(provider.Fields, field("networkAttachments", named(attachment)))
	return provider
}
