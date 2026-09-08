package v1alpha1

func environmentSchema() *Shape {
	proxyChoice := choice(field("proxyRef", nonempty()), field("direct", record()))
	catalog := func(extra ...Field) *Shape {
		return named(record(append([]Field{required("name", name()), required("management", enumeration("external", "managed")), field("componentRef", ref(InfraComponent))}, extra...)...))
	}
	imagePins := record(field("local", image()), field("public", image()))
	return record(
		required("domains", record(required("base", dns()), field("machines", dns()), field("clusters", dns()), field("containerClusters", dns()), field("storageClusters", dns()))),
		field("sites", named(record(required("name", name()), field("description", text())))),
		field("resources", nonemptyArray(set(nonempty()))), field("containerClusters", nonemptyArray(set(nonempty()))), field("storageClusters", nonemptyArray(set(nonempty()))),
		field("remoteMachinesAccessKey", record(required("keyRef", secret("sshKeyPair")))),
		field("defaults", &Shape{Type: Mapping, KindDefaults: true}),
		field("downloads", record(field("openshiftClientsMirror", url()), field("virtctlMirror", url()), field("helmMirror", url()))),
		field("proxy", record(field("defaultRef", nonempty()), field("bootwright", proxyChoice), field("containerClusterInstall", proxyChoice), field("machineOSInstall", proxyChoice))),
		field("infraComponents", record(
			field("proxies", catalog(field("endpointRef", nonempty()), field("connection", record(field("httpProxy", url()), field("httpsProxy", url()), field("noProxy", array(nonempty())), field("auth", record(field("proxyAuthRef", secret("usernamePassword")))), field("trustBundleRef", secret("caBundle")))))),
			field("nameResolution", catalog(field("endpointRef", nonempty()), field("address", ip()), field("additionalIngressHosts", array(dns())))),
			field("artifactServers", catalog(defaulted("default", boolean(), BoolValue(false)), field("endpoints", nonemptyArray(named(record(required("name", name()), required("url", url()))))))),
			field("registries", catalog(defaulted("default", boolean(), BoolValue(false)), field("endpointRef", nonempty()), field("url", lexical("registry")))),
			field("ntp", catalog(field("endpointRef", nonempty()), field("address", lexical("host")))),
		)),
		field("registries", record(field("mirror", record(field("url", lexical("registry")), field("credentialsRef", secret("usernamePassword", "dockerConfigJson")), field("trustBundleRef", secret("caBundle")))), field("imageDigestSources", array(record(required("source", lexical("registry")), required("mirrors", nonemptyArray(array(lexical("registry")))), field("sourcePolicy", enumeration("NeverContactSource", "AllowContactingSource"))))))),
		field("trustedCAs", record(field("caBundleRefs", set(secret("caBundle"))))),
		field("lifecycle", record(field("rescue", record(required("imageRef", ref(MachineImage)), required("os", record(required("family", enumeration("rhel")), required("version", nonempty()), required("architecture", enumeration("x86_64", "aarch64")))), required("artifactServerEndpoint", artifactEndpoint()))))),
		field("componentImages", record(field("loadBalancer", record(field("haproxy", imagePins))), field("registry", record(field("mirror-registry", imagePins))), field("proxy", record(field("squid", imagePins))), field("nameResolution", record(field("dnsmasq", imagePins))), field("artifactServer", record(field("http", imagePins))))),
	)
}

func artifactEndpoint() *Shape {
	return record(field("serverRef", nonempty()), required("endpointRef", nonempty()))
}
