package v1alpha1

func init() { register(OpenShiftCluster, openShiftClusterSchema) }

func clusterEndpoint() *Shape {
	source := record(defaulted("type", enumeration("installer", "external", "loadBalancer", "node"), StringValue("installer")), field("loadBalancerRef", ref(LoadBalancer)), field("bindAddressRef", nestedRef()))
	for _, kind := range []string{"installer", "external", "node"} {
		source.Suppress = append(source.Suppress, Suppression{Field: "type", Value: StringValue(kind), Fields: []string{"loadBalancerRef", "bindAddressRef"}})
	}
	endpoint := record(field("address", ip()), field("dnsName", dns()), field("port", port()), field("scheme", enumeration("http", "https")), field("prefixLength", integer("1", "128")), defaulted("interfaceNetworks", set(cidr()), ListValue()), defaulted("source", source, MapValue()))
	for _, kind := range []string{"node", "loadBalancer"} {
		endpoint.Suppress = append(endpoint.Suppress, Suppression{Field: "source.type", Value: StringValue(kind), Fields: []string{"address"}})
	}
	return endpoint
}

func openShiftClusterSchema() *Shape {
	networkSubnets := record(field("networkSubnetCidr", array(cidr())))
	platform := choice(
		field("baremetal", record(field("provisioningNetwork", enumeration("disabled", "managed", "unmanaged")))),
		field("vsphere", record(field("nodeNetworking", record(field("external", networkSubnets), field("internal", networkSubnets))))),
		field("none", record()),
		field("external", native()),
	)
	nodeSSH := atomic(record(field("keyPairRef", secret("sshKeyPair")), field("publicKeyRef", secret("opaque", "sshKeyPair")), field("privateKeyRef", secret("opaque", "sshKeyPair"))))
	namedCertificate := record(required("names", nonemptyArray(set(dns()))), required("secretRef", secret("tlsCertificate")))
	certificates := record(
		field("apiServer", record(required("namedCertificates", nonemptyArray(array(namedCertificate))))),
		field("ingress", record(required("defaultCertificateRef", secret("tlsCertificate")))),
	)
	install := record(
		defaulted("mode", enumeration("connected", "disconnected"), StringValue("connected")),
		field("platform", platform),
		field("proxy", proxySelection()),
		field("ntp", serverSelections(NTPServer)),
		field("registries", registrySelections()),
		required("endpoints", record(required("api", clusterEndpoint()), field("api-int", clusterEndpoint()), required("ingress", clusterEndpoint()))),
		defaulted("agent", record(field("redfishVirtualMedia", record(required("artifactServerEndpoint", artifactEndpoint()))), field("bootArtifacts", record(required("artifactServerEndpoint", artifactEndpoint())))), MapValue()),
		field("pullSecretRef", secret("dockerConfigJson")),
		field("nodeSSH", nodeSSH),
		defaulted("additionalTrustBundleRefs", set(secret("caBundle")), ListValue()),
		field("additionalTrustBundlePolicy", enumeration("Always", "Proxyonly")),
		field("servingCertificates", certificates),
	)
	install.Arms = []string{"agent"}
	install.Suppress = []Suppression{{Field: "mode", Value: StringValue("connected"), Fields: []string{"agent.bootArtifacts"}}}
	networking := record(
		field("networkType", nonempty()),
		field("ovnKubernetesConfig", ovnKubernetesSchema()),
		field("secondaryNetworks", secondaryNetworksSchema()),
		field("clusterNetwork", array(record(required("cidr", cidr()), required("hostPrefix", integer("1", "128"))))),
		field("serviceNetwork", array(cidr())),
	)
	security := record(
		field("fips", record(defaulted("enabled", boolean(), BoolValue(false)))),
		field("diskEncryption", record(required("unlock", record(required("tpm2", record()))), field("roles", set(enumeration("controlPlane", "worker", "infra"))))),
	)
	taint := record(required("key", nonempty()), field("value", text()), required("effect", enumeration("NoSchedule", "PreferNoSchedule", "NoExecute")))
	node := record(required("name", name()), field("fqdn", dns()), required("role", enumeration("controlPlane", "worker", "infra")), required("machineRef", ref(Machine)), field("labels", stringsMap()), field("taints", array(taint)))
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

func ovnKubernetesSchema() *Shape {
	return record(field("mtu", integer("576", "65536")), field("gatewayConfig", record(field("routingViaHost", boolean()), field("ipForwarding", enumeration("Restricted", "Global")))))
}
func secondaryNetworksSchema() *Shape {
	return named(record(required("name", name()), required("topology", enumeration("Localnet")), required("uplink", record(required("bridge", lexical("ifname")), field("interface", lexical("ifname")))), field("vlan", integer("1", "4094")), field("mtu", integer("576", "65536")), defaulted("ipam", record(defaulted("mode", enumeration("Disabled", "Enabled"), StringValue("Disabled")), field("subnets", array(cidr())), field("excludeSubnets", array(cidr())), field("lifecycle", enumeration("Persistent"))), MapValue()), required("namespaceSelector", record(required("matchLabels", stringsMap())))))
}
