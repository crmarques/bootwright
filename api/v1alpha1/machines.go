package v1alpha1

func init() {
	register(Machine, machineSchema)
	register(NetworkConfig, machineNetworkSchema)
	register(InfraProvider, machineProviderSchema)
	register(MachineImage, machineImageSchema)
	register(MachineInstallProfile, machineInstallSchema)
}

func machineVirtualMediaSchema() *Shape {
	tls := record(field("trust", enumeration("disableVerification", "importCertificate", "established")), field("enableVerification", boolean()), field("restoreVerificationAfterBoot", boolean()), field("removeCertificateAfterBoot", boolean()))
	tls.Suppress = []Suppression{
		{Field: "trust", Value: StringValue("disableVerification"), Fields: []string{"enableVerification", "removeCertificateAfterBoot"}},
		{Field: "trust", Value: StringValue("importCertificate"), Fields: []string{"restoreVerificationAfterBoot"}},
		{Field: "trust", Value: StringValue("established"), Fields: []string{"restoreVerificationAfterBoot", "removeCertificateAfterBoot"}},
	}
	return record(field("tls", tls))
}

func machineBMCTLSSchema() *Shape {
	tls := record(field("trustBundleRef", secret("caBundle", "trustBundle")), field("verification", tlsVerification()))
	tls.Suppress = []Suppression{
		{Field: "verification", Value: StringValue("none"), Fields: []string{"trustBundleRef"}},
	}
	return tls
}

func machineBMCSchema() *Shape {
	return record(required("address", nonempty()), field("credentialsRef", secret("usernamePassword")), field("tls", machineBMCTLSSchema()), field("virtualMedia", machineVirtualMediaSchema()))
}

func machineFirmwareSchema() *Shape {
	attributes := &Shape{Type: Mapping, Open: true, Atomic: true, Element: &Shape{Alternatives: []*Shape{text(), integer("-9223372036854775808", "9223372036854775807"), boolean()}}}
	return atomic(record(defaulted("mode", enumeration("enforce", "prove"), StringValue("enforce")), field("secureBoot", boolean()), field("bootMode", enumeration("UEFI")), field("biosAttributes", attributes)))
}

func machineDiskDiscoverySchema() *Shape {
	match := record(field("model", nonempty()), field("controllerPCIAddress", nonempty()), field("minSizeBytes", integer("4096", "70368744177664")), field("maxSizeBytes", integer("4096", "70368744177664")))
	identity := record(field("wwn", nonempty()), field("serialNumber", nonempty()), required("sizeBytes", integer("4096", "70368744177664")))
	ssh := record(defaulted("port", port(), IntegerValue("22")))
	return atomic(record(defaulted("inventory", enumeration("automatic", "boot"), StringValue("automatic")), defaulted("ssh", ssh, MapValue()), field("match", match), field("exclude", &Shape{Type: Sequence, Atomic: true, Element: identity})))
}

func diskSize() *Shape { return integer("4096", "70368744177664") }

func removalAction() *Shape { return enumeration("retain", "metadataClear", "logicalOverwrite") }

func machineHardwareSchema() *Shape {
	segment := Scope{Noun: "segment", Owner: []string{"hardware", "providerRef"}, Kind: InfraProvider, Entries: []string{"segments"}, Joined: &ScopeJoin{Kind: Network, Field: []string{"libvirt", "providerRef"}}}
	profile := Scope{Noun: "machine profile", Owner: []string{"hardware", "providerRef"}, Kind: InfraProvider, Arms: []string{"libvirt", "kubevirt", "vsphere"}, Entries: []string{"machineProfiles"}}
	nics := named(record(required("name", nonempty()), field("macAddress", lexical(LexicalMAC)), field("segmentRef", nestedIn(segment))))
	hints := record(field("deviceName", lexical(LexicalDevicePath)), field("model", nonempty()), field("vendor", nonempty()), field("minSizeGigabytes", integer("0", "")), field("rotational", boolean()), field("hctl", nonempty()))
	rootDisk := record(field("wwn", nonempty()), field("serialNumber", nonempty()), field("sizeBytes", diskSize()),
		field("volumeRef", atomic(external(record(required("storageId", nonempty()), required("volumeId", nonempty()))))), field("bootVolume", record()), field("discover", machineDiskDiscoverySchema()),
		field("hints", hints), field("onRemoval", removalAction()))
	dataDisks := &Shape{Type: Sequence, Atomic: true, NameKey: "serialNumber", Element: record(required("serialNumber", nonempty()), required("sizeBytes", diskSize()), field("wwn", nonempty()), field("onInstall", enumeration("retain", "metadataClear")), field("onRemoval", removalAction()))}
	return record(field("providerRef", ref(InfraProvider)), field("profileRef", nestedIn(profile)), field("bmc", machineBMCSchema()), field("firmware", machineFirmwareSchema()), field("nics", nics), field("rootDisk", rootDisk), field("dataDisks", dataDisks))
}

func machineSchema() *Shape {
	ssh := record(field("addressRef", nestedIn(Scope{Noun: "address", Entries: []string{"network", "addresses"}})), field("port", port()), field("user", lexical(LexicalPOSIXUser)), required("auth", atomic(choice(field("operatorIdentity", record()), field("privateKeyRef", secret("sshKeyPair")), field("passwordRef", secret("usernamePassword"))))), field("sudoPasswordRef", secret("usernamePassword")), field("knownHostsRef", secret("opaque")))
	access := choice(field("local", record()), field("ssh", ssh))
	access.AllowEmpty = true
	lifecycle := atomic(record(
		required("user", lexical(LexicalPOSIXUser)),
		required("privateKeyRef", secret("sshKeyPair")),
		required("knownHostsRef", secret("opaque")),
		required("privilege", enumeration("direct", "sudo")),
	))
	access.Fields = append(access.Fields, field("rootLogin", enumeration("keep", "revoke")), field("lifecycle", lifecycle))
	network := choice(field("configRef", ref(NetworkConfig)), field("inline", machineNetworkSchema()))
	network.AllowEmpty = true
	network.Fields = append(network.Fields,
		field("installAddressRef", nestedIn(Scope{Noun: "address", Entries: []string{"network", "addresses"}})), field("addresses", named(record(required("name", nonempty()), required("address", lexical(LexicalHost)), field("interface", lexical(LexicalIfname))))),
		field("interfaceMatches", &Shape{Type: Sequence, Atomic: true, NameKey: "interface", Element: record(required("interface", lexical(LexicalIfname)), required("nicRef", nestedIn(Scope{Noun: "hardware NIC", Entries: []string{"hardware", "nics"}})))}), field("overrides", native()))
	os := choice(field("provided", record()), field("install", record(required("profileRef", ref(MachineInstallProfile)), field("hostKeyRef", secret("sshKeyPair")), field("ntp", serverSelections(NTPServer)))), field("clusterInstalled", record()))
	source := choice(field("ip", ip()), field("cidr", cidr()), field("machine", record(
		required("machineRef", ref(Machine)),
		required("addressRef", nestedIn(Scope{Noun: "address", Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}})),
	)))
	return record(
		field("site", name()),
		field("hardware", machineHardwareSchema()),
		required("os", os),
		field("proxy", proxySelection()),
		field("firewall", record(field("management", enumeration("managed", "external")), field("ssh", record(required("allowedSources", set(source)))))),
		field("network", network), field("access", access),
	)
}

func machineNetworkSchema() *Shape {
	return record(field("dns", serverSelections(DNSServer)), required("nmstate", native()))
}

func machineProfileSchema(variant string) *Shape {
	fields := []Field{required("name", nonempty()), defaulted("cpu", integer("0", "1024"), IntegerValue("0")), defaulted("memoryMiB", integer("0", "16777216"), IntegerValue("0")), defaulted("diskGiB", integer("0", "65536"), IntegerValue("0"))}
	if variant == "vsphere" {
		fields = append(fields, field("template", nonempty()), field("failureDomainRef", nestedIn(Scope{Noun: "failure domain", Entries: []string{"vsphere", "failureDomains"}})))
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
	baremetal := record(field("defaults", record(field("bmc", record(field("credentialsRef", secret("usernamePassword")), field("tls", record(field("trustBundleRef", secret("caBundle", "trustBundle")))), field("virtualMedia", machineVirtualMediaSchema()))))))
	libvirt := record(required("machineRef", ref(Machine)), defaulted("uri", enumeration("qemu:///system"), StringValue("qemu:///system")), field("capacityReserve", record(required("cpu", integer("0", "9223372036854775807")), required("memoryMiB", integer("0", "8796093022207")), required("diskGiB", integer("0", "8589934591")))), required("bmcEmulation", record(required("bindAddress", ip()), required("certificateRef", secret("tlsCertificate")), defaulted("port", port(), IntegerValue("8000")), required("auth", record(required("credentialsRef", secret("usernamePassword")))))), defaulted("machineProfiles", machineProfileSchema("libvirt"), ListValue()))
	vcenters := nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "server", Element: record(required("server", lexical(LexicalHost)), field("port", integer("0", "65535")), required("datacenters", nonemptyArray(set(nonempty()))), required("credentialsRef", secret("usernamePassword")), defaulted("disableCertificateVerification", boolean(), BoolValue(false)))})
	topology := record(required("datacenter", nonempty()), required("computeCluster", nonempty()), required("datastore", nonempty()), required("networks", nonemptyArray(set(nonempty()))), field("folder", nonempty()), field("resourcePool", nonempty()))
	vsphere := record(required("vcenters", vcenters), required("failureDomains", nonemptyArray(named(record(required("name", nonempty()), required("region", nonempty()), required("zone", nonempty()), required("server", nonempty()), required("topology", topology))))), field("isoStaging", record(field("datastore", nonempty()), field("folder", nonempty()))), defaulted("machineProfiles", machineProfileSchema("vsphere"), ListValue()))
	kubevirt := choice(field("hostClusterRef", clusters(ContainerClusterCategory)), field("kubeconfigRef", secret("opaque")))
	kubevirt.Fields = append(kubevirt.Fields, required("namespace", name()), field("storageClassRef", externalRef()), defaulted("machineProfiles", machineProfileSchema("kubevirt"), ListValue()))
	segment := choice(field("baremetal", record()), field("libvirt", record(required("bridge", lexical(LexicalBridge)))), field("vsphere", record(required("portgroup", nonempty()), field("distributedSwitch", nonempty()))), field("kubevirt", record(required("networkRef", external(record(field("apiGroup", text()), field("kind", nonempty()), required("name", nonempty()), field("namespace", name())))))))
	segment.Fields = append([]Field{required("name", name()), field("untaggedNetworkRef", ref(Network)), field("taggedNetworkRefs", set(ref(Network))), defaulted("mtu", integer("576", "65536"), IntegerValue("1500"))}, segment.Fields...)
	provider := choice(field("baremetal", baremetal), field("libvirt", libvirt), field("vsphere", vsphere), field("kubevirt", kubevirt))
	provider.Fields = append(provider.Fields, field("segments", named(segment)))
	return provider
}
