package v1alpha1

const SignOnServiceType = "oauth2-proxy"

const DefaultSignOnGroupsClaim = "groups"

func storageSignOnSchema() *Shape {
	secrets := nonemptyArray(array(record(required("path", nonempty()), required("secretRef", secret("opaque", "token", "tlsCertificate")))))
	secrets.NameKey = "path"
	access := record(required("allowedGroups", nonemptyArray(set(nonempty()))), defaulted("groupsClaim", nonempty(), StringValue(DefaultSignOnGroupsClaim)))
	return record(required("clusterRef", ref(CephCluster)), required("oidcProviderRef", ref(OIDCProvider)), field("access", access), required("secrets", secrets), required("native", native()))
}

func StorageSignOnNativeSchema() *Shape {
	spec := record(field("https_address", nonempty()), required("provider_display_name", nonempty()), required("client_id", nonempty()), field("redirect_url", nonempty()), field("allowlist_domains", set(nonempty())))
	return record(required("service_type", enumeration(SignOnServiceType)), required("placement", storageNativePlacement()), field("networks", set(cidr())), required("spec", spec))
}
