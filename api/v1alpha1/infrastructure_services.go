package v1alpha1

func init() {
	for _, kind := range []Kind{Proxy, DNSServer, NTPServer, ArtifactServer, Registry, LoadBalancer} {
		register(kind, func() *Shape { return infrastructureServiceSchema(kind) })
	}
}

func infrastructureServiceSchema(kind Kind) *Shape {
	fields := []Field{required("management", enumeration("managed", "external")), field("machineRef", ref(Machine))}
	managed := []string{"machineRef"}
	external := []string{}
	if kind != NTPServer {
		fields = append(fields, field("image", record(field("local", image()), field("public", image()))))
		managed = append(managed, "image")
	}
	implementation := map[Kind]string{Proxy: "squid", DNSServer: "dnsmasq", NTPServer: "chrony", Registry: "mirror-registry", LoadBalancer: "haproxy"}[kind]
	if implementation != "" {
		fields = append(fields, field("implementation", enumeration(implementation)))
		managed = append(managed, "implementation")
	}
	if kind != LoadBalancer {
		fields = append(fields, field("bindAddress", ip()))
		managed = append(managed, "bindAddress")
	}
	if kind != ArtifactServer && kind != LoadBalancer {
		fields = append(fields, field("port", port()), field("endpoints", named(record(required("name", nonempty()), required("addressRef", nonempty())))))
		managed = append(managed, "port", "endpoints")
	}
	switch kind {
	case Proxy:
		fields = append(fields, field("connection", record(field("httpProxy", url()), field("httpsProxy", url()), field("auth", record(required("proxyAuthRef", secret("usernamePassword")))), field("trustBundleRef", secret("caBundle")))))
		external = append(external, "connection")
	case DNSServer:
		fields = append(fields, field("address", ip()), field("additionalIngressHosts", set(dns())), field("forwarders", set(ip())))
		external = append(external, "address")
		managed = append(managed, "forwarders")
	case NTPServer:
		fields = append(fields, field("address", lexical("host")), field("upstreamSources", set(lexical("host"))))
		external = append(external, "address")
		managed = append(managed, "upstreamSources")
	case ArtifactServer:
		listener := named(record(required("name", nonempty()), required("protocol", enumeration("http", "https")), required("port", port())))
		endpoint := named(record(required("name", nonempty()), field("url", url()), field("listenerRef", nonempty()), field("addressRef", nonempty())))
		fields = append(fields, field("retention", enumeration("persistent", "install-only")), field("tls", record(required("secretRef", secret("tlsCertificate")), field("minVersion", enumeration("TLSv1.2", "TLSv1.3")))), field("listeners", listener), field("endpoints", endpoint))
		managed = append(managed, "retention", "tls", "listeners")
	case Registry:
		fields = append(fields, field("url", lexical("registry")), field("credentialsRef", secret("usernamePassword", "dockerConfigJson")), field("trustBundleRef", secret("caBundle")))
		external = append(external, "url")
	case LoadBalancer:
		fields = append(fields, required("bindAddresses", nonemptyArray(array(record(required("address", ip()), field("name", nonempty()))))))
	}
	schema := record(fields...)
	schema.Suppress = []Suppression{
		{Field: "management", Value: StringValue("external"), Fields: managed},
		{Field: "management", Value: StringValue("managed"), Fields: external},
	}
	if kind == ArtifactServer {
		for _, modes := range [][2]string{{"external", "managed"}, {"managed", "external"}} {
			schema.Suppress = append(schema.Suppress, Suppression{Field: "management", Value: StringValue(modes[0]), FallbackValue: StringValue(modes[1]), Fields: []string{"endpoints"}})
		}
	}
	return schema
}

func proxySelection() *Shape {
	selection := choice(field("proxyRef", ref(Proxy)), field("direct", record()))
	selection.Fields = append(selection.Fields, field("endpointRef", nonempty()), field("noProxy", set(nonempty())))
	return atomic(selection)
}

func serverSelections(kind Kind) *Shape {
	return set(record(required("serverRef", ref(kind)), field("endpointRef", nonempty())))
}

func registrySelections() *Shape {
	mirror := atomic(record(required("registryRef", ref(Registry)), field("endpointRef", nonempty())))
	return record(field("mirror", mirror), field("imageDigestSources", array(record(required("source", lexical("registry")), required("mirrors", nonemptyArray(array(lexical("registry")))), field("sourcePolicy", enumeration("NeverContactSource", "AllowContactingSource"))))))
}
