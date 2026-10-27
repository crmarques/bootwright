package v1alpha1

func init() {
	register(Machine, machineSchema)
	register(NetworkConfig, machineNetworkSchema)
	register(InfraProvider, machineProviderSchema)
	register(MachineImage, machineImageSchema)
	register(MachineInstallProfile, machineInstallSchema)
}

func machineVirtualMediaSchema() *Shape {
	tls := record(field("trust", enumeration("disableVerification", "importCertificate", "established")), field("restoreVerificationAfterBoot", boolean()), field("removeCertificateAfterBoot", boolean()))
	tls.Suppress = []Suppression{
		{Field: "trust", Value: StringValue("disableVerification"), Fields: []string{"removeCertificateAfterBoot"}},
		{Field: "trust", Value: StringValue("importCertificate"), Fields: []string{"restoreVerificationAfterBoot"}},
		{Field: "trust", Value: StringValue("established"), Fields: []string{"restoreVerificationAfterBoot", "removeCertificateAfterBoot"}},
	}
	return record(field("tls", tls))
}

func machineBMCTLSSchema() *Shape {
	tls := record(field("verify", boolean()), field("trustBundleRef", secret("caBundle")))
	tls.Suppress = []Suppression{{Field: "verify", Value: BoolValue(false), Fields: []string{"trustBundleRef"}}}
	return tls
}

func machineBMCSchema() *Shape {
	return record(required("address", nonempty()), field("protocol", enumeration("redfish")), field("credentialsRef", secret("usernamePassword")), field("tls", machineBMCTLSSchema()), field("virtualMedia", machineVirtualMediaSchema()))
}

func machineSubstrateSchema() *Shape {
	macs := &Shape{Type: Sequence, Atomic: true, NameKey: "nicRef", Element: record(required("nicRef", nestedRef()), required("macAddress", lexical("mac")))}
	attachments := &Shape{Type: Sequence, Atomic: true, NameKey: "interface", Element: record(required("interface", lexical("ifname")), required("attachmentRef", nestedRef()))}
	baremetal := record(field("bmc", machineBMCSchema()), field("nics", macs), field("rootDevice", record(field("wwn", nonempty()), field("hctl", nonempty()), field("serialNumber", nonempty()))))
	libvirt := record(field("profileRef", nestedRef()), field("interfaceAttachments", attachments))
	kubevirt := record(field("profileRef", nestedRef()), field("interfaceAttachments", attachments))
	vsphere := record(field("profileRef", nestedRef()), field("nics", macs))
	arms := choice(field("baremetal", baremetal), field("libvirt", libvirt), field("kubevirt", kubevirt), field("vsphere", vsphere))
	arms.AllowEmpty = true
	arms.Fields = append([]Field{field("providerRef", ref(InfraProvider))}, arms.Fields...)
	return arms
}

func machineSchema() *Shape {
	ssh := record(field("addressRef", nestedRef()), field("port", port()), field("user", lexical("posix-user")), required("auth", atomic(choice(field("operatorIdentity", record()), field("privateKeyRef", secret("sshKeyPair")), field("passwordRef", secret("usernamePassword"))))), field("sudoPasswordRef", secret("usernamePassword")), field("knownHostsRef", secret("opaque")))
	access := choice(field("local", boolean()), field("ssh", ssh))
	access.AllowEmpty = true
	access.Fields = append(access.Fields, field("rootLogin", enumeration("keep", "revoke")))
	network := choice(field("configRef", ref(NetworkConfig)), field("inline", machineNetworkSchema()))
	network.AllowEmpty = true
	network.Fields = append(network.Fields,
		field("attachmentRef", nestedRef()),
		field("installAddressRef", nestedRef()), field("addresses", named(record(required("name", nonempty()), required("address", lexical("address")), field("interface", lexical("ifname"))))),
		field("interfaceBinding", &Shape{Type: Sequence, Atomic: true, NameKey: "nicRef", Element: record(required("nicRef", nestedRef()), required("interfaceName", lexical("ifname")))}), field("overrides", native()))
	return record(
		field("placement", record(field("site", name()))),
		field("substrate", machineSubstrateSchema()),
		field("hardware", record(field("eraseOnRemoval", machineEraseSchema()), field("nics", named(record(required("name", nonempty())))), field("rootDevice", record(field("deviceName", lexical("device-path")), field("model", nonempty()), field("vendor", nonempty()), field("minSizeGigabytes", integer("0", "")), field("rotational", boolean()))))),
		required("os", record(required("provided", boolean()), field("installProfileRef", ref(MachineInstallProfile)), field("install", record(field("hostKeyRef", secret("sshKeyPair")), field("ntp", serverSelections(NTPServer)))))),
		field("proxy", proxySelection()),
		field("network", network), field("access", access),
	)
}

func machineNetworkSchema() *Shape {
	return record(required("machineNetwork", nonemptyArray(set(record(required("cidr", cidr()))))), field("dns", serverSelections(DNSServer)), required("nmstate", native()))
}

func machineProfileSchema(variant string) *Shape {
	fields := []Field{required("name", nonempty()), defaulted("cpu", integer("0", "1024"), IntegerValue("0")), defaulted("memoryMiB", integer("0", "16777216"), IntegerValue("0")), defaulted("diskGiB", integer("0", "65536"), IntegerValue("0"))}
	if variant == "vsphere" {
		fields = append(fields, field("template", nonempty()), field("failureDomainRef", nestedRef()))
	}
	if variant != "kubevirt" {
		disk := record(required("name", name()), required("sizeGiB", integer("1", "65536")))
		if variant == "libvirt" {
			disk.Fields = append(disk.Fields, field("media", enumeration("ssd", "hdd")))
		}
		fields = append(fields, field("dataDisks", named(disk)))
	}
	if variant == "libvirt" {
		fields = append(fields, field("tpm", record()))
	} else if variant == "kubevirt" {
		fields = append(fields, field("tpm", record(defaulted("persistent", boolean(), BoolValue(true)))))
	}
	return named(record(fields...))
}

func machineProviderSchema() *Shape {
	baremetal := record(field("defaults", record(field("bmc", record(field("credentialsRef", secret("usernamePassword")), field("tls", machineBMCTLSSchema()), field("virtualMedia", machineVirtualMediaSchema()))))))
	libvirt := record(required("machineRef", ref(Machine)), defaulted("uri", enumeration("qemu:///system"), StringValue("qemu:///system")), field("capacityReserve", record(required("cpu", integer("0", "9223372036854775807")), required("memoryMiB", integer("0", "8796093022207")), required("diskGiB", integer("0", "8589934591")))), required("bmcEmulation", record(required("bindAddress", ip()), required("certificateRef", secret("tlsCertificate")), defaulted("port", port(), IntegerValue("8000")), required("auth", record(required("credentialsRef", secret("usernamePassword")))))), defaulted("machineProfiles", machineProfileSchema("libvirt"), ListValue()))
	vcenters := nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "server", Element: record(required("server", lexical("host")), field("port", integer("0", "65535")), required("datacenters", nonemptyArray(set(nonempty()))), required("credentialsRef", secret("usernamePassword")), defaulted("disableCertificateVerification", boolean(), BoolValue(false)))})
	topology := record(required("datacenter", nonempty()), required("computeCluster", nonempty()), required("datastore", nonempty()), required("networks", nonemptyArray(set(nonempty()))), field("folder", nonempty()), field("resourcePool", nonempty()))
	vsphere := record(required("vcenters", vcenters), required("failureDomains", nonemptyArray(named(record(required("name", nonempty()), required("region", nonempty()), required("zone", nonempty()), required("server", nonempty()), required("topology", topology))))), field("isoStaging", record(field("datastore", nonempty()), field("folder", nonempty()))), defaulted("machineProfiles", machineProfileSchema("vsphere"), ListValue()))
	kubevirt := choice(field("hostClusterRef", clusters(ContainerClusterCategory)), field("kubeconfigRef", secret("opaque")))
	kubevirt.Fields = append(kubevirt.Fields, required("namespace", name()), field("storageClassRef", externalRef()), defaulted("machineProfiles", machineProfileSchema("kubevirt"), ListValue()))
	libvirtAttachment := record(required("bridge", lexical("bridge")), defaulted("management", enumeration("managed", "external"), StringValue("external")), field("address", lexical("address")), field("forward", enumeration("nat", "none")), field("mtu", integer("576", "65536")))
	libvirtAttachment.Suppress = []Suppression{{Field: "management", Value: StringValue("external"), Fields: []string{"address", "forward"}}}
	attachment := choice(field("baremetal", record()), field("libvirt", libvirtAttachment), field("vsphere", record(required("portgroup", nonempty()), field("distributedSwitch", nonempty()))), field("kubevirt", record(required("networkRef", external(record(field("apiGroup", text()), field("kind", nonempty()), required("name", nonempty()), field("namespace", name())))))))
	attachment.Fields = append([]Field{required("name", name())}, attachment.Fields...)
	provider := choice(field("baremetal", baremetal), field("libvirt", libvirt), field("vsphere", vsphere), field("kubevirt", kubevirt))
	provider.Fields = append(provider.Fields, field("networkAttachments", named(attachment)))
	return provider
}
