package v1alpha1

func environmentSchema() *Shape {
	return record(
		required("domains", record(required("base", dns()), field("machines", dns()), field("clusters", dns()), field("containerClusters", dns()), field("storageClusters", dns()))),
		field("sites", named(record(required("name", name()), field("description", text())))),
		field("containerClusters", nonemptyArray(set(nonempty()))), field("storageClusters", nonemptyArray(set(nonempty()))),
		field("remoteMachinesAccessKey", record(required("keyRef", secret("sshKeyPair")))),
		field("defaults", &Shape{Type: Mapping, KindDefaults: true}),
		field("dependencyVersions", dependencyVersions()),
		field("mirror", record(field("artifacts", artifactMirror()), field("registry", atomic(record(required("registryRef", ref(Registry)), field("endpointRef", endpointOf("registryRef", Registry))))))),
		required("controller", record(required("machineRef", ref(Machine)))),
		field("lifecycle", record(field("rescue", record(required("imageRef", ref(MachineImage)), required("os", record(required("family", enumeration("rhel")), required("version", nonempty()), required("architecture", enumeration("x86_64", "aarch64")))), required("artifactServerEndpoint", artifactEndpoint()))))),
	)
}

// dependencyVersions declares only the prerequisites a context selects. The
// private interpreter, Ansible and the baseline native packages are prepared by
// context-independent setup, which reads no Environment.
func dependencyVersions() *Shape {
	return record(
		field("libvirt", lexical(LexicalPackageVersion)),
		field("helm", lexical(LexicalCLIVersion)), field("govc", lexical(LexicalCLIVersion)), field("virtctl", lexical(LexicalCLIVersion)),
	)
}

func artifactMirror() *Shape {
	mirror := atomic(choice(field("url", lexical(LexicalArtifactMirrorURL)), field("serverRef", ref(ArtifactServer))))
	mirror.Fields = append(mirror.Fields, field("endpointRef", endpointOf("serverRef", ArtifactServer)), field("trustBundleRef", secret("caBundle", "trustBundle")), field("tlsVerification", tlsVerification()),
		field("toolSources", record(required("helm", record(required("catalogArtifact", name()))))))
	return mirror
}

func artifactEndpoint() *Shape {
	return record(required("serverRef", ref(ArtifactServer)), required("endpointRef", endpointOf("serverRef", ArtifactServer)))
}
