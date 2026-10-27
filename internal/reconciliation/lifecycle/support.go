package lifecycle

import (
	"cmp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Refusal is one selected object this executable cannot realize, why, and what
// an operator changes so that it can. It names the object by kind and name, so
// the diagnostic that reports it carries that identity; the remediation names
// whatever object the operator changes, which need not be this one.
type Refusal struct {
	Kind, Name  string
	Field       string
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
		return cmp.Or(cmp.Compare(x.Kind, y.Kind), cmp.Compare(x.Name, y.Name), cmp.Compare(x.Field, y.Field),
			cmp.Compare(x.Reason, y.Reason), cmp.Compare(x.Remediation, y.Remediation))
	})
	return slices.Compact(refusals)
}

// unclaimedRemediation is what the operator does about an object no capability
// of this executable realizes: leave it out, or start from a supported shape.
func unclaimedRemediation(identity string) string {
	return "remove " + identity + " from the selected Environment, or use an example within the supported shape such as " + supportedExample
}

// Bearing is the unclaimed refusal's reason and a listed service's placement.
type Bearing struct {
	Reason      string
	Remedy      string
	Shared      bool
	Machine     string
	InstallOnly bool
}

// EffectBearing is every domain root's predicate, composed, so the engine
// names no kind or field of a domain itself.
type EffectBearing func(api.Object) (Bearing, bool)

// Unrealizable refuses each selected effect-bearing object of an unclaimed kind.
func Unrealizable(catalog api.Catalog, claimed []string, bearing EffectBearing) []Refusal {
	if bearing == nil {
		return nil
	}
	var found []Refusal
	for _, object := range catalog.Objects() {
		if slices.Contains(claimed, string(object.Kind())) {
			continue
		}
		if effect, bears := bearing(object); bears {
			remedy := effect.Remedy
			if remedy == "" {
				remedy = unclaimedRemediation(object.Identity())
			}
			found = append(found, RefusalOf(object, effect.Reason, remedy))
		}
	}
	return SortRefusals(found)
}
