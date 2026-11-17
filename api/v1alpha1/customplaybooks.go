package v1alpha1

func init() { register(CustomPlaybook, customPlaybookSchema) }

func playbookSourceSchema() *Shape {
	return atomic(choice(
		field("path", lexical(LexicalAbsolutePath)),
		field("git", record(required("url", nonempty()), required("ref", nonempty()), field("subdir", lexical(LexicalRelativePath)), field("secretRef", secret("token", "usernamePassword", "sshKeyPair")))),
	))
}

func customPlaybookSchema() *Shape {
	s := record(
		field("gates", enumeration("fabric", "machines", "deps", "base", "addOns")),
		field("follows", enumeration("fabric", "machines", "deps", "base", "addOns")),
		field("source", playbookSourceSchema()), required("playbook", lexical(LexicalRelativePath)), field("rolesPath", lexical(LexicalRelativePath)), field("collectionsPath", lexical(LexicalRelativePath)),
		field("tags", set(lexical(LexicalToken))), field("skipTags", set(lexical(LexicalToken))),
		required("target", record(field("clusters", array(clusters(ContainerClusterCategory, StorageClusterCategory))), field("machines", array(ref(Machine))), field("hostGroups", array(lexical(LexicalToken))))),
		field("order", integer("", "")), field("provides", array(lexical(LexicalToken))), field("requires", array(lexical(LexicalToken))),
		field("extraVars", native()), field("secretRefs", array(ref(Secret))), field("timeout", duration()), defaulted("enabled", boolean(), BoolValue(true)),
	)
	s.Arms = []string{"gates", "follows"}
	return s
}
