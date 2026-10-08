package v1alpha1

func machineImageSchema() *Shape {
	return record(required("bootMedia", nonempty()), field("checksum", lexical("checksum")))
}

func machineInstallSchema() *Shape {
	repositories := &Shape{Type: Sequence, Atomic: true, NameKey: "id", Element: record(required("id", nonempty()), required("baseURL", lexical("repository-url")))}
	packageSource := choice(field("mirror", record(required("baseURL", lexical("repository-url")), field("repositories", repositories))), field("fromSubscription", record(required("entitlementRef", ref(Entitlement)))), field("hostedTree", record(required("fromMedia", nonempty()), required("artifactServerEndpoint", artifactEndpoint()))))
	anaconda := record(required("imageRef", ref(MachineImage)), field("redfishVirtualMedia", record(field("artifactServerEndpoint", artifactEndpoint()))), field("packageSource", packageSource))
	clone := record(required("seed", choice(field("cloudInit", record(defaulted("growRootFilesystem", boolean(), BoolValue(true)))))))
	configure := &Shape{Type: Sequence, Atomic: true, NameKey: "id", Element: record(required("id", nonempty()), required("baseURL", lexical("repository-url")), field("displayName", nonempty()), defaulted("enabled", boolean(), BoolValue(true)), defaulted("gpgCheck", boolean(), BoolValue(true)), field("gpgKeyURL", nonempty()))}
	customizations := record(
		field("localization", record(defaulted("language", nonempty(), StringValue("en_US.UTF-8")), field("formats", nonempty()), defaulted("keyboard", nonempty(), StringValue("us")), defaulted("timezone", nonempty(), StringValue("UTC")), field("additionalLocales", set(nonempty())))),
		field("ssh", record(defaulted("passwordAuthentication", boolean(), BoolValue(false)), field("initialPassword", record(required("secretRef", secret("usernamePassword")))))),
		field("packages", record(field("install", set(nonempty())), defaulted("excludeDocs", boolean(), BoolValue(false)), field("installWeakDeps", boolean()))),
		field("repositories", record(field("configure", configure), field("subscription", record(field("enable", set(nonempty())), field("disable", set(nonempty())))))),
		field("services", record(field("enabled", set(nonempty())), field("disabled", set(nonempty())))),
		field("security", record(field("selinux", record(field("mode", enumeration("enforcing", "permissive", "disabled")))), field("firewall", record(field("enabled", boolean()))), field("fips", record(defaulted("enabled", boolean(), BoolValue(false)))), field("diskEncryption", record(required("unlock", choice(field("tpm2", record(field("pcrIds", set(integer("0", "23"))), field("pcrBank", enumeration("sha1", "sha256", "sha384", "sha512")))))), required("recoveryPassphraseRef", secret("opaque", "token")))))),
	)
	return record(required("os", record(required("family", nonempty()), required("version", nonempty()), required("architecture", enumeration("x86_64")))), required("installer", choice(field("anaconda", anaconda), field("templateClone", clone))), field("subscription", record(required("entitlementRef", ref(Entitlement)))), field("customizations", customizations), field("proxy", proxySelection()), field("ntp", serverSelections(NTPServer)))
}
