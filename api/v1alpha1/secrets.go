package v1alpha1

import "slices"

func secretSchema() *Shape {
	source := atomic(choice(field("contextStore", record()), field("file", record(field("path", nonempty()), field("cert", nonempty()), field("key", nonempty()), field("privateKey", nonempty()), field("publicKey", nonempty()))), field("generated", record(field("issuerRef", secret("caBundle")), field("username", nonempty()), field("commonName", nonempty()), field("dnsNames", array(dns())), field("ipAddresses", array(ip())), field("validityDays", integer("1", "36500")), field("keyType", enumeration("ed25519", "rsa", "ecdsaP256", "ecdsaP384", "ecdsaP521")), field("comment", text()), field("bytes", integer("16", "1024"))))))
	source.AllowEmpty = true
	return record(required("type", enumeration("opaque", "token", "usernamePassword", "dockerConfigJson", "caBundle", "tlsCertificate", "sshKeyPair")), field("source", source))
}

func entitlementSchema() *Shape {
	rhsm := record(defaulted("management", enumeration("managed", "external"), StringValue("managed")), field("organizationRef", secret("opaque")), field("activationKeyRef", secret("opaque", "token")), field("connectToInsights", boolean()), field("satellite", record(required("hostname", lexical("host")), field("trustBundleRef", secret("caBundle")), field("contentBaseURL", url()))))
	rhsm.Suppress = []Suppression{{Field: "management", Value: StringValue("external"), Fields: []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"}}}
	entitlement := record(required("type", enumeration("redhatRhel", "redhatCeph", "ibmStorageCeph")), field("rhsm", rhsm), field("registry", record(field("url", lexical("registry")), field("credentialsRef", secret("usernamePassword")), field("trustBundleRef", secret("caBundle")))), field("license", record(defaulted("accept", boolean(), BoolValue(false)))))
	entitlement.Permits = []Permission{{Field: "type", Value: "redhatRhel", Arms: []string{"rhsm"}}, {Field: "type", Value: "redhatCeph", Arms: []string{"rhsm", "registry"}}, {Field: "type", Value: "ibmStorageCeph", Arms: []string{"registry", "license"}}}
	entitlement.Suppress = permittedOnly(entitlement.Permits)
	return entitlement
}

func PermittedArmSet(permits []Permission) []string {
	var arms []string
	for _, permit := range permits {
		for _, arm := range permit.Arms {
			if !slices.Contains(arms, arm) {
				arms = append(arms, arm)
			}
		}
	}
	return arms
}

func permittedOnly(permits []Permission) []Suppression {
	var suppressed []Suppression
	for _, permit := range permits {
		var others []string
		for _, arm := range PermittedArmSet(permits) {
			if !slices.Contains(permit.Arms, arm) {
				others = append(others, arm)
			}
		}
		suppressed = append(suppressed, Suppression{Field: permit.Field, Value: StringValue(permit.Value), Fields: others})
	}
	return suppressed
}
