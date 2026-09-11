package v1alpha1

func environmentSchema() *Shape {
	return record(
		required("domains", record(required("base", dns()), field("machines", dns()), field("clusters", dns()), field("containerClusters", dns()), field("storageClusters", dns()))),
		field("sites", named(record(required("name", name()), field("description", text())))),
		field("resources", nonemptyArray(set(nonempty()))), field("containerClusters", nonemptyArray(set(nonempty()))), field("storageClusters", nonemptyArray(set(nonempty()))),
		field("remoteMachinesAccessKey", record(required("keyRef", secret("sshKeyPair")))),
		field("defaults", &Shape{Type: Mapping, KindDefaults: true}),
		field("downloads", record(field("openshiftClientsMirror", url()), field("virtctlMirror", url()), field("helmMirror", url()))),
		field("dependencyVersions", dependencyVersions()),
		required("controller", record(required("machineRef", ref(Machine)))),
		field("lifecycle", record(field("rescue", record(required("imageRef", ref(MachineImage)), required("os", record(required("family", enumeration("rhel")), required("version", nonempty()), required("architecture", enumeration("x86_64", "aarch64")))), required("artifactServerEndpoint", artifactEndpoint()))))),
	)
}

func dependencyVersions() *Shape {
	return record(
		field("python", lexical("cli-version")), field("ansible", lexical("cli-version")),
		field("podman", lexical("package-version")), field("openssh", lexical("package-version")),
		field("nmstate", lexical("package-version")), field("libvirt", lexical("package-version")),
		field("helm", lexical("cli-version")), field("govc", lexical("cli-version")), field("virtctl", lexical("cli-version")),
	)
}

func artifactEndpoint() *Shape {
	return record(required("serverRef", ref(ArtifactServer)), required("endpointRef", nonempty()))
}
