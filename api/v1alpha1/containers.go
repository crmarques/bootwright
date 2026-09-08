package v1alpha1

func init() { register(ContainerCluster, containerClusterSchema) }

func containerEndpoint() *Shape {
	source := record(defaulted("type", enumeration("openshift", "external", "infraComponent", "node"), StringValue("openshift")), field("componentRef", ref(InfraComponent)), field("bindAddressRef", nonempty()))
	for _, kind := range []string{"openshift", "external", "node"} {
		source.Suppress = append(source.Suppress, Suppression{Field: "type", Value: StringValue(kind), Fields: []string{"componentRef", "bindAddressRef"}})
	}
	endpoint := record(field("address", ip()), field("dnsName", dns()), field("port", port()), field("scheme", enumeration("http", "https")), field("prefixLength", integer("1", "128")), defaulted("interfaceNetworks", set(cidr()), ListValue()), defaulted("source", source, MapValue()))
	for _, kind := range []string{"node", "infraComponent"} {
		endpoint.Suppress = append(endpoint.Suppress, Suppression{Field: "source.type", Value: StringValue(kind), Fields: []string{"address"}})
	}
	return endpoint
}

func containerClusterSchema() *Shape {
	networkSubnets := record(field("networkSubnetCidr", array(cidr())))
	platform := record(
		required("type", enumeration("baremetal", "vsphere", "none", "external")),
		field("baremetal", record(field("provisioningNetwork", enumeration("disabled", "managed", "unmanaged")))),
		field("vsphere", record(field("nodeNetworking", record(field("external", networkSubnets), field("internal", networkSubnets))))),
		field("external", native()),
	)
	platform.Discriminator = "type"
	platform.Arms = []string{"baremetal", "vsphere", "external"}
	platform.ArmValues = map[string][]string{"baremetal": {"baremetal"}, "vsphere": {"vsphere"}, "none": {}, "external": {"external"}}
	platform.Suppress = []Suppression{{Field: "type", Value: StringValue("none"), Fields: []string{"baremetal", "vsphere", "external"}}}
	nodeSSH := atomic(record(field("keyPairRef", secret("sshKeyPair")), field("publicKeyRef", secret("opaque", "sshKeyPair")), field("privateKeyRef", secret("opaque", "sshKeyPair"))))
	namedCertificate := record(required("names", nonemptyArray(set(dns()))), required("secretRef", secret("tlsCertificate")))
	certificates := record(
		field("apiServer", record(required("namedCertificates", nonemptyArray(array(namedCertificate))))),
		field("ingress", record(required("defaultCertificateRef", secret("tlsCertificate")))),
	)
	install := record(
		defaulted("method", enumeration("agent"), StringValue("agent")),
		defaulted("mode", enumeration("connected", "disconnected"), StringValue("connected")),
		field("platform", platform),
		required("endpoints", record(required("api", containerEndpoint()), field("api-int", containerEndpoint()), required("ingress", containerEndpoint()))),
		field("agent", record(field("redfishVirtualMedia", record(required("artifactServerEndpoint", artifactEndpoint()))), field("bootArtifacts", record(required("artifactServerEndpoint", artifactEndpoint()))))),
		field("pullSecretRef", secret("dockerConfigJson")),
		field("nodeSSH", nodeSSH),
		defaulted("additionalTrustBundleRefs", set(secret("caBundle")), ListValue()),
		field("servingCertificates", certificates),
	)
	install.Suppress = []Suppression{{Field: "mode", Value: StringValue("connected"), Fields: []string{"agent.bootArtifacts"}}}
	networking := record(
		field("networkType", nonempty()),
		field("clusterNetwork", array(record(required("cidr", cidr()), required("hostPrefix", integer("1", "128"))))),
		field("serviceNetwork", array(cidr())),
	)
	security := record(
		field("fips", record(defaulted("enabled", boolean(), BoolValue(false)))),
		field("diskEncryption", record(required("unlock", record(required("tpm2", record()))), field("roles", set(enumeration("master", "worker", "infra"))))),
	)
	taint := record(required("key", nonempty()), field("value", text()), required("effect", enumeration("NoSchedule", "PreferNoSchedule", "NoExecute")))
	node := record(required("name", name()), field("fqdn", dns()), required("role", enumeration("master", "worker", "infra")), required("machineRef", ref(Machine)), field("labels", stringsMap()), field("taints", array(taint)))
	distribution := record(defaulted("type", enumeration("openshift", "okd"), StringValue("openshift")), required("release", record(field("version", nonempty()), field("channel", nonempty()), field("image", image()))))
	distribution.Suppress = []Suppression{{Field: "type", Value: StringValue("okd"), Fields: []string{"release.channel"}}}
	return record(
		defaulted("distribution", distribution, MapValue()),
		required("install", install),
		field("networking", networking),
		field("security", security),
		required("nodes", nonemptyArray(named(node))),
	)
}
