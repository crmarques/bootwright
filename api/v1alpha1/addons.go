package v1alpha1

func init() {
	register(ClusterAddon, addonSchema)
	register(ClusterAddonProfile, addonProfileSchema)
	register(ClusterAddonBinding, addonBindingSchema)
}

func addonSchema() *Shape {
	inputs := named(choice(
		required("name", nonempty()),
		field("resourceKind", addonResourceKind()),
		field("secretType", enumeration("opaque", "token", "usernamePassword", "dockerConfigJson", "caBundle", "tlsCertificate", "sshKeyPair")),
		defaulted("required", boolean(), BoolValue(true)),
		field("effects", array(choice(
			field("storageExportAttachment", record()),
			field("globalPullSecretMerge", record(required("registry", nonempty()), required("username", nonempty()))),
		))),
	))
	inputs.Element.Arms = []string{"resourceKind", "secretType"}
	s := record(
		field("provides", set(lexical("token"))),
		field("requires", set(lexical("token"))),
		field("inputs", inputs),
		field("olm", addonOLMSchema()),
		field("manifestSet", record(required("manifests", nonemptyArray(array(record(required("path", lexical("relative-path")))))))),
		field("readiness", record(defaulted("timeout", duration(), StringValue("30m")), field("checks", array(addonReadinessSchema())))),
		field("steps", named(addonStepSchema())),
	)
	s.Arms = []string{"olm", "manifestSet"}
	return s
}

func addonResourceKind() *Shape {
	values := []string{}
	for _, kind := range Kinds() {
		if kind != Secret {
			values = append(values, string(kind))
		}
	}
	return enumeration(values...)
}

func addonOLMSchema() *Shape {
	return record(
		required("namespace", record(required("name", name()), defaulted("management", enumeration("managed", "external"), StringValue("managed")), field("labels", stringsMap()))),
		field("operatorGroup", record(field("name", dns()), field("targetNamespaces", array(name())))),
		field("catalogSource", record(
			required("name", dns()), required("image", image()), field("displayName", text()), field("publisher", text()), field("pollInterval", duration()),
			field("grpcPodConfig", record(required("securityContextConfig", enumeration("legacy", "restricted")))),
		)),
		required("subscription", record(
			field("name", dns()), required("package", nonempty()), required("channel", nonempty()), field("startingCSV", nonempty()),
			required("source", dns()), defaulted("sourceNamespace", name(), StringValue("openshift-marketplace")),
			defaulted("installPlanApproval", enumeration("Automatic", "Manual"), StringValue("Automatic")),
		)),
		field("customResources", array(native())),
	)
}

func addonReadinessSchema() *Shape {
	return choice(
		field("csvSucceeded", record(required("namespace", name()), required("subscription", dns()))),
		field("condition", record(required("apiVersion", nonempty()), required("kind", nonempty()), required("name", nonempty()), field("namespace", name()), required("condition", record(required("type", nonempty()), required("status", nonempty()))))),
		field("resourceExists", record(required("apiVersion", nonempty()), required("kind", nonempty()), required("name", nonempty()), field("namespace", name()))),
	)
}

func addonStepSchema() *Shape {
	target := choice(
		field("boundCluster", record()),
		field("fromInput", record(required("input", nonempty()))),
		field("static", record(field("clusters", array(ref(ContainerCluster, StorageCluster))), field("machines", array(ref(Machine))))),
		field("limit", enumeration("firstReachable", "all")),
	)
	target.Arms = []string{"boundCluster", "fromInput", "static"}
	s := record(
		required("name", lexical("token")), field("gates", enumeration("apply")), field("follows", enumeration("operatorReady", "ready")),
		field("requires", array(addonReadinessSchema())), field("source", playbookSourceSchema()),
		field("playbook", lexical("relative-path")), field("rolesPath", lexical("relative-path")), field("collectionsPath", lexical("relative-path")),
		field("target", target), field("extraVars", native()), field("secretRefs", set(ref(Secret))), field("timeout", duration()),
		field("outputs", named(record(required("name", lexical("token")), required("file", lexical("relative-path")), field("secret", boolean()), field("format", enumeration("text", "json", "sha256"))))),
		field("manifests", array(record(required("path", lexical("relative-path")), field("reclaimRendered", boolean())))),
	)
	s.Arms = []string{"gates", "follows"}
	return s
}

func addonProfileSchema() *Shape {
	return record(field("profileRefs", array(ref(ClusterAddonProfile))), field("addonRefs", array(ref(ClusterAddon))))
}

func addonBindingSchema() *Shape {
	configs := array(record(required("addonRef", ref(ClusterAddon)), field("inputs", named(record(required("name", nonempty()), required("value", nonempty()))))))
	configs.NameKey = "addonRef"
	return record(required("clusterRef", ref(ContainerCluster)), field("profileRefs", array(ref(ClusterAddonProfile))), field("addonRefs", array(ref(ClusterAddon))), field("addonConfigs", configs))
}
