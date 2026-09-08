package v1alpha1

func init() { register(CustomPlaybook, customPlaybookSchema) }

func playbookSourceSchema() *Shape {
	return atomic(choice(
		field("path", lexical("absolute-path")),
		field("git", record(required("url", nonempty()), required("ref", nonempty()), field("subdir", lexical("relative-path")), field("secretRef", secret("token", "usernamePassword", "sshKeyPair")))),
	))
}

func customPlaybookSchema() *Shape {
	s := record(
		field("gates", enumeration("fabric", "machines", "deps", "base", "add-ons")),
		field("follows", enumeration("fabric", "machines", "deps", "base", "add-ons")),
		field("source", playbookSourceSchema()), required("playbook", lexical("relative-path")), field("rolesPath", lexical("relative-path")), field("collectionsPath", lexical("relative-path")),
		field("tags", set(lexical("token"))), field("skipTags", set(lexical("token"))),
		required("target", record(field("clusters", array(ref(ContainerCluster, StorageCluster))), field("machines", array(ref(Machine))), field("hostGroups", array(lexical("token"))))),
		field("order", integer("", "")), field("provides", array(lexical("token"))), field("requires", array(lexical("token"))),
		field("extraVars", native()), field("secretRefs", array(ref(Secret))), field("timeout", duration()), defaulted("enabled", boolean(), BoolValue(true)),
	)
	s.Arms = []string{"gates", "follows"}
	return s
}
