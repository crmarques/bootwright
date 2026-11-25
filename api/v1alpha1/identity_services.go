package v1alpha1

func init() {
	register(OIDCProvider, oidcProviderSchema)
	register(LDAPServer, ldapServerSchema)
}

func oidcProviderSchema() *Shape {
	return record(
		required("management", enumeration("managed", "external")),
		required("issuerURL", lexical(LexicalHTTPSURL)),
		field("trustBundleRef", secret("caBundle", "trustBundle")),
	)
}

func ldapServerSchema() *Shape {
	return record(
		required("management", enumeration("managed", "external")),
		required("urls", nonemptyArray(set(lexical(LexicalLDAPURL)))),
		field("trustBundleRef", secret("caBundle", "trustBundle")),
		required("bindCredentialsRef", secret("usernamePassword")),
		required("users", record(required("baseDN", lexical(LexicalLDAPDN)), required("nameAttribute", lexical(LexicalLDAPAttribute)), field("filter", lexical(LexicalLDAPFilter)))),
		field("activeDirectory", record(required("realm", lexical(LexicalRealm)))),
	)
}
