package storage

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

type BindingSlot struct {
	ServiceType string
	Backend     string
	Path        string
	SecretTypes []string
	Part        secrets.Part
	Pair        string
	Requires    string
}

var bindingSlots = []BindingSlot{
	{ServiceType: "rgw", Path: "/spec/ssl_cert", SecretTypes: []string{"tlsCertificate"}, Part: secrets.CertificatePart, Pair: "/spec/ssl_key", Requires: "ssl"},
	{ServiceType: "rgw", Path: "/spec/ssl_key", SecretTypes: []string{"tlsCertificate"}, Part: secrets.PrivateKeyPart, Pair: "/spec/ssl_cert", Requires: "ssl"},
}

func NativeBindingSlot(cluster api.Object, serviceRef, path string) (BindingSlot, bool) {
	service, ok := NativeService(cluster, serviceRef)
	if !ok {
		return BindingSlot{}, false
	}
	for _, slot := range bindingSlots {
		if slot.ServiceType != textOf(service, "service_type") || slot.Path != path {
			continue
		}
		if slot.Backend != "" {
			backend, found := NativeService(cluster, textOf(service, "spec", "backend_service"))
			if !found || textOf(backend, "service_type") != slot.Backend {
				continue
			}
		}
		return slot, true
	}
	return BindingSlot{}, false
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
		}
		leaves = append(leaves, boundLeaf{field: leaf, ref: ref, slot: slot})
	}
	for _, leaf := range leaves {
		if leaf.slot.Pair == "" {
			continue
		}
		at := slices.IndexFunc(leaves, func(other boundLeaf) bool { return other.slot.Path == leaf.slot.Pair })
		switch {
		case at < 0:
			a.issue("api.invariant", leaf.field+".path", "ssl_cert and ssl_key are bound together", "bind both /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret")
		case leaf.slot.Part == secrets.PrivateKeyPart && leaves[at].ref != leaf.ref:
			a.issue("api.invariant", leaf.field+".secretRef", "ssl_cert and ssl_key must bind the certificate and private key of one tlsCertificate Secret", "bind /spec/ssl_cert and /spec/ssl_key to the same tlsCertificate Secret")
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
