package v1alpha1

func secretSchema() *Shape {
	source := atomic(choice(field("contextStore", record()), field("file", record(field("path", nonempty()), field("cert", nonempty()), field("key", nonempty()), field("privateKey", nonempty()), field("publicKey", nonempty()))), field("generated", record(field("username", nonempty()), field("commonName", nonempty()), field("dnsNames", array(dns())), field("ipAddresses", array(ip())), field("validityDays", integer("1", "36500")), field("keyType", enumeration("ed25519", "rsa", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521")), field("comment", text()), field("bytes", integer("16", "1024"))))))
	source.AllowEmpty = true
	return record(required("type", enumeration("opaque", "token", "usernamePassword", "dockerConfigJson", "caBundle", "tlsCertificate", "sshKeyPair")), field("source", source))
}

func entitlementSchema() *Shape {
	rhsm := record(defaulted("management", enumeration("managed", "external"), StringValue("managed")), field("organizationRef", secret("opaque")), field("activationKeyRef", secret("opaque", "token")), field("connectToInsights", boolean()), field("satellite", record(required("hostname", lexical("host")), field("trustBundleRef", secret("caBundle")), field("contentBaseURL", url()))))
	rhsm.Suppress = []Suppression{{Field: "management", Value: StringValue("external"), Fields: []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"}}}
	return record(required("type", enumeration("redhat-rhel", "redhat-ceph", "ibm-storage-ceph")), field("rhsm", rhsm), field("registry", record(field("url", lexical("registry")), field("credentialsRef", secret("usernamePassword")), field("trustBundleRef", secret("caBundle")))), field("license", record(defaulted("accept", boolean(), BoolValue(false)))))
}
