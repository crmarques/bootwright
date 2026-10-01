package lifecycle

import (
	"cmp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

// effectKinds are the API kinds whose selected objects need a lifecycle
// capability before an operation can realize them. A kind no capability claims
// refuses the whole operation rather than silently leaving its objects out.
func effectKinds() []api.Kind {
	return []api.Kind{
		api.InfraProvider,
		api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer,
		api.ContainerCluster, api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool,
		api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport,
		api.ClusterAddon, api.ClusterAddonProfile, api.ClusterAddonBinding,
	}
}

// Refusal is one selected object this executable cannot realize, why, and what
// an operator changes so that it can. It names the object by kind and name, so
// the diagnostic that reports it carries that identity; the remediation names
// whatever object the operator changes, which need not be this one.
type Refusal struct {
	Kind, Name  string
	Reason      string
	Remediation string
}

// RefusalOf states one object's refusal.
func RefusalOf(object api.Object, reason, remediation string) Refusal {
	return Refusal{Kind: string(object.Kind()), Name: object.Name(), Reason: reason, Remediation: remediation}
}

// identity is the refused object's API identity, as Kind/name.
func (r Refusal) identity() string { return r.Kind + "/" + r.Name }

// Identities lists the objects refusals name, in canonical order and once
// each, because one object may be refused for more than one reason.
func Identities(refusals []Refusal) []string {
	found := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		found = append(found, refusal.identity())
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// SortRefusals orders refusals by object, then reason and remediation, and
// keeps one of each, which is the order their diagnostics are reported in.
func SortRefusals(refusals []Refusal) []Refusal {
	slices.SortFunc(refusals, func(x, y Refusal) int {
		return cmp.Or(cmp.Compare(x.Kind, y.Kind), cmp.Compare(x.Name, y.Name),
			cmp.Compare(x.Reason, y.Reason), cmp.Compare(x.Remediation, y.Remediation))
	})
	return slices.Compact(refusals)
}

// unclaimedRemediation is what the operator does about an object no capability
// of this executable realizes: leave it out, or start from a supported shape.
func unclaimedRemediation(identity string) string {
	return "remove " + identity + " from the selected Environment, or use an example within the supported shape such as " + supportedExample
}

// Unrealizable refuses every selected object of an unclaimed effect-bearing
// kind, in canonical order. External services and declaration-only objects are
// inputs and never appear here.
func Unrealizable(catalog api.Catalog, claimed []string) []Refusal {
	var found []Refusal
	refuse := func(object api.Object, reason string) {
		found = append(found, RefusalOf(object, reason, unclaimedRemediation(object.Identity())))
	}
	for _, kind := range effectKinds() {
		if slices.Contains(claimed, string(kind)) {
			continue
		}
		for _, object := range catalog.OfKind(kind) {
			if infrastructureservices.IsService(kind) && object.Spec().Get("management").Text() != "managed" {
				continue
			}
			reason := "no capability of this executable realizes the " + string(kind) + " kind"
			if infrastructureservices.IsService(kind) {
				reason = "no capability of this executable manages the " + string(kind) + " kind"
			}
			refuse(object, reason)
		}
	}
	if !slices.Contains(claimed, string(api.Machine)) {
		for _, machine := range catalog.OfKind(api.Machine) {
			if provided := machine.Spec().Get("os", "provided"); provided.Type() == api.Boolean && !provided.Bool() {
				refuse(machine, "no capability of this executable realizes a Machine whose operating system is not provided")
			}
		}
	}
	if !slices.Contains(claimed, string(api.CustomPlaybook)) {
		for _, playbook := range catalog.OfKind(api.CustomPlaybook) {
			if enabled := playbook.Spec().Get("enabled"); enabled.Type() == api.Boolean && enabled.Bool() {
				refuse(playbook, "no capability of this executable runs an enabled CustomPlaybook")
			}
		}
	}
	return SortRefusals(found)
}
