package storage

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

type BindingRule struct {
	Holds   func(spec api.Value) bool
	Field   string
	Message string
	Remedy  string
}

type BindingSlot struct {
	ServiceType string
	Backend     string
	Path        string
	SecretTypes []string
	Part        secrets.Part
	Pair        string
	Requires    string
	Required    *BindingRule
	Allowed     *BindingRule
}

var bindingSlots = []BindingSlot{
	{ServiceType: "rgw", Path: "/spec/ssl_cert", SecretTypes: []string{"tlsCertificate"}, Part: secrets.CertificatePart, Pair: "/spec/ssl_key", Requires: "ssl"},
	{ServiceType: "rgw", Path: "/spec/ssl_key", SecretTypes: []string{"tlsCertificate"}, Part: secrets.PrivateKeyPart, Pair: "/spec/ssl_cert", Requires: "ssl"},
	{ServiceType: "ingress", Backend: "rgw", Path: "/spec/ssl_cert", SecretTypes: []string{"tlsCertificate"}, Part: secrets.CertificatePart, Pair: "/spec/ssl_key", Requires: "ssl"},
	{ServiceType: "ingress", Backend: "rgw", Path: "/spec/ssl_key", SecretTypes: []string{"tlsCertificate"}, Part: secrets.PrivateKeyPart, Pair: "/spec/ssl_cert", Requires: "ssl"},
	{ServiceType: "grafana", Path: "/spec/initial_admin_password", SecretTypes: valueSecretTypes, Part: secrets.ValuePart, Required: &BindingRule{Holds: anonymousAccessOff, Field: "spec.anonymous_access", Message: "Grafana without anonymous access needs the /spec/initial_admin_password binding"}},
	{ServiceType: "snmp-gateway", Path: "/spec/credentials/snmp_community", SecretTypes: valueSecretTypes, Part: secrets.ValuePart, Required: &BindingRule{Holds: snmpVersion("V2c"), Field: "spec.snmp_version", Message: "SNMP V2c notifications need the /spec/credentials/snmp_community binding"}, Allowed: &BindingRule{Holds: snmpVersion("V2c"), Message: "snmp_community is read only for snmp_version V2c", Remedy: "remove this binding; SNMP V3 binds /spec/credentials/snmp_v3_auth_username and /spec/credentials/snmp_v3_auth_password"}},
	{ServiceType: "snmp-gateway", Path: "/spec/credentials/snmp_v3_auth_username", SecretTypes: []string{"usernamePassword"}, Part: secrets.UsernamePart, Pair: "/spec/credentials/snmp_v3_auth_password", Required: snmpV3Auth, Allowed: &BindingRule{Holds: snmpVersion("V3"), Message: "snmp_v3_auth_username is read only for snmp_version V3", Remedy: snmpV2cRemedy}},
	{ServiceType: "snmp-gateway", Path: "/spec/credentials/snmp_v3_auth_password", SecretTypes: []string{"usernamePassword"}, Part: secrets.PasswordPart, Pair: "/spec/credentials/snmp_v3_auth_username", Required: snmpV3Auth, Allowed: &BindingRule{Holds: snmpVersion("V3"), Message: "snmp_v3_auth_password is read only for snmp_version V3", Remedy: snmpV2cRemedy}},
	{ServiceType: "snmp-gateway", Path: "/spec/credentials/snmp_v3_priv_password", SecretTypes: valueSecretTypes, Part: secrets.ValuePart, Required: &BindingRule{Holds: snmpPrivacy, Field: "spec.privacy_protocol", Message: "an SNMP V3 privacy_protocol needs the /spec/credentials/snmp_v3_priv_password binding"}, Allowed: &BindingRule{Holds: snmpPrivacy, Message: "snmp_v3_priv_password is read only with snmp_version V3 and a privacy_protocol", Remedy: "remove this binding or set spec.privacy_protocol on an SNMP V3 gateway"}},
	{ServiceType: "mgmt-gateway", Path: "/spec/ssl_cert", SecretTypes: []string{"tlsCertificate"}, Part: secrets.CertificatePart, Pair: "/spec/ssl_key"},
	{ServiceType: "mgmt-gateway", Path: "/spec/ssl_key", SecretTypes: []string{"tlsCertificate"}, Part: secrets.PrivateKeyPart, Pair: "/spec/ssl_cert"},
	{ServiceType: "oauth2-proxy", Path: "/spec/client_secret", SecretTypes: valueSecretTypes, Part: secrets.ValuePart, Required: &BindingRule{Holds: always, Message: "oauth2-proxy needs the /spec/client_secret binding"}},
	{ServiceType: "oauth2-proxy", Path: "/spec/cookie_secret", SecretTypes: valueSecretTypes, Part: secrets.ValuePart, Required: &BindingRule{Holds: always, Message: "oauth2-proxy needs the /spec/cookie_secret binding, so continuation never depends on a secret cephadm generated"}},
	{ServiceType: "oauth2-proxy", Path: "/spec/ssl_cert", SecretTypes: []string{"tlsCertificate"}, Part: secrets.CertificatePart, Pair: "/spec/ssl_key"},
	{ServiceType: "oauth2-proxy", Path: "/spec/ssl_key", SecretTypes: []string{"tlsCertificate"}, Part: secrets.PrivateKeyPart, Pair: "/spec/ssl_cert"},
}

const snmpV2cRemedy = "remove this binding; SNMP V2c binds /spec/credentials/snmp_community"

var (
	valueSecretTypes = []string{"opaque", "token"}
	snmpV3Auth       = &BindingRule{Holds: snmpVersion("V3"), Field: "spec.snmp_version", Message: "SNMP V3 notifications need the /spec/credentials/snmp_v3_auth_username and /spec/credentials/snmp_v3_auth_password bindings of one usernamePassword Secret"}
)

func always(api.Value) bool { return true }

func anonymousAccessOff(spec api.Value) bool {
	access := spec.Get("anonymous_access")
	return access.Type() == api.Boolean && !access.Bool()
}

func snmpVersion(version string) func(api.Value) bool {
	return func(spec api.Value) bool { return textOf(spec, "snmp_version") == version }
}

func snmpPrivacy(spec api.Value) bool { return snmpVersion("V3")(spec) && spec.Has("privacy_protocol") }

func NativeBindingSlot(cluster api.Object, serviceRef, path string) (BindingSlot, bool) {
	service, ok := NativeService(cluster, serviceRef)
	if !ok {
		return BindingSlot{}, false
	}
	for _, slot := range bindingSlots {
		if slot.ServiceType != textOf(service, "service_type") || slot.Path != path {
			continue
		}
		if slot.Backend != "" && ingressBackendType(cluster, service) != slot.Backend {
			continue
		}
		return slot, true
	}
	return BindingSlot{}, false
}

func noBindingRefusal(cluster api.Object, service api.Value) string {
	switch {
	case textOf(service, "service_type") == "nfs":
		return "NFS services take no secret bindings: Bootwright does not support NFS over TLS or kmip yet"
	case textOf(service, "service_type") == "ingress" && ingressBackendType(cluster, service) == "nfs":
		return "the NFS service and its ingress take no secret bindings: Bootwright does not support NFS over TLS or kmip yet"
	}
	return ""
}

type boundLeaf struct {
	field, ref string
	slot       BindingSlot
}

func (a *admission) bindingLeaves(binding, service api.Value, field string, partial bool) {
	leaves := []boundLeaf{}
	for j, secret := range binding.Get("secrets").Items() {
		leaf := indexed(field+".secrets", j)
		slot, ok := NativeBindingSlot(a.object, textOf(binding, "serviceRef"), textOf(secret, "path"))
		if !ok {
			a.issue("api.invariant", leaf+".path", "secret leaf is not qualified for this service schema", "use the existing cluster SSH or entitlement Secret reference; service-specific bindings require their owning slice")
			continue
		}
		ref := textOf(secret, "secretRef")
		if !partial {
			if object, found := a.catalog.Find(api.Secret, ref); found && !slices.Contains(slot.SecretTypes, textOf(object.Spec(), "type")) {
				a.issue("api.invariant", leaf+".secretRef", "this binding leaf accepts only "+strings.Join(slot.SecretTypes, " or ")+" Secrets", "bind a "+strings.Join(slot.SecretTypes, " or ")+" Secret at this path")
			}
			if slot.Requires != "" && !service.Get("spec", slot.Requires).Bool() {
				a.issue("api.invariant", leaf+".path", "this TLS binding takes effect only when its service sets "+slot.Requires+": true", "set spec."+slot.Requires+": true on the bound service or remove this binding")
			}
			if slot.Allowed != nil && !slot.Allowed.Holds(service.Get("spec")) {
				a.issue("api.invariant", leaf+".path", slot.Allowed.Message, slot.Allowed.Remedy)
			}
		}
		leaves = append(leaves, boundLeaf{field: leaf, ref: ref, slot: slot})
	}
	for _, leaf := range leaves {
		if leaf.slot.Pair == "" {
			continue
		}
		at := slices.IndexFunc(leaves, func(other boundLeaf) bool { return other.slot.Path == leaf.slot.Pair })
		lead, follower := pairOrder(leaf.slot)
		names := lastToken(lead.Path) + " and " + lastToken(follower.Path)
		paths, kind := lead.Path+" and "+follower.Path, leaf.slot.SecretTypes[0]
		switch {
		case at < 0:
			a.issue("api.invariant", leaf.field+".path", names+" are bound together", "bind both "+paths+" to one "+kind+" Secret")
		case pairFollower(leaf.slot) && leaves[at].ref != leaf.ref:
			a.issue("api.invariant", leaf.field+".secretRef", names+" must bind the "+partWords(lead.Part)+" and "+partWords(follower.Part)+" of one "+kind+" Secret", "bind "+paths+" to the same "+kind+" Secret")
		}
	}
}

func serviceBindsLeaves(cluster api.Object, service api.Value, paths ...string) bool {
	name := NativeServiceName(service)
	for _, binding := range cluster.Spec().Get("ceph", "serviceBindings").Items() {
		if textOf(binding, "serviceRef") != name {
			continue
		}
		bound := []string{}
		for _, secret := range binding.Get("secrets").Items() {
			bound = append(bound, textOf(secret, "path"))
		}
		return !slices.ContainsFunc(paths, func(path string) bool { return !slices.Contains(bound, path) })
	}
	return false
}

func pairFollower(slot BindingSlot) bool {
	return slot.Part == secrets.PrivateKeyPart || slot.Part == secrets.PasswordPart
}

func pairOrder(slot BindingSlot) (BindingSlot, BindingSlot) {
	partner := slot
	for _, other := range bindingSlots {
		if other.ServiceType == slot.ServiceType && other.Backend == slot.Backend && other.Path == slot.Pair {
			partner = other
		}
	}
	if pairFollower(slot) {
		return partner, slot
	}
	return slot, partner
}

func lastToken(pointer string) string { return pointer[strings.LastIndex(pointer, "/")+1:] }

func partWords(part secrets.Part) string { return strings.ReplaceAll(string(part), "-", " ") }

func (a *admission) requiredBindings(service api.Value, field string) {
	for _, slot := range bindingSlots {
		if slot.ServiceType != textOf(service, "service_type") || slot.Required == nil || pairFollower(slot) || !slot.Required.Holds(service.Get("spec")) {
			continue
		}
		at := field
		if slot.Required.Field != "" {
			at += "." + slot.Required.Field
		}
		target := slot.Path + " to " + article(slot.SecretTypes[0]) + strings.Join(slot.SecretTypes, " or ") + " Secret"
		paths := []string{slot.Path}
		if slot.Pair != "" {
			lead, follower := pairOrder(slot)
			target = lead.Path + " and " + follower.Path + " to one " + slot.SecretTypes[0] + " Secret"
			paths = append(paths, slot.Pair)
		}
		if !serviceBindsLeaves(a.object, service, paths...) {
			a.issue("api.invariant", at, slot.Required.Message, "add a spec.ceph.serviceBindings entry for this service that binds "+target)
		}
	}
}

func article(word string) string {
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an "
	}
	return "a "
}
