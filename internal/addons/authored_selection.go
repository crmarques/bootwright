package addons

import (
	"fmt"
	"slices"
)

func validateAuthoredIdentity(cluster string, members []selectedMember, add selectionReporter) {
	for i, m := range members {
		if m.shadows {
			add(m, "api.invariant", fmt.Sprintf("ClusterAddon/%s has the name of built-in package %s", m.name, m.name), fmt.Sprintf("rename ClusterAddon/%s; bootwright add-ons list names the built-in packages", m.name))
			continue
		}
		if !m.authored {
			continue
		}
		for j, other := range members {
			if i == j || other.pkg == nil || other.authored && (j > i || other.name == m.name) {
				continue
			}
			for _, shared := range sharedIdentities(m, other) {
				add(m, "api.invariant", fmt.Sprintf("ClusterAddon/%s uses %s %s, which %s also uses on OpenShiftCluster/%s", m.name, shared.what, shared.name, other.subject(), cluster), shared.remedy(m, other))
			}
		}
	}
}

type sharedIdentity struct{ what, name, kind string }

func (s sharedIdentity) remedy(m, other selectedMember) string {
	switch s.kind {
	case "namespace":
		return fmt.Sprintf("set olm.namespace.name of %s to a namespace that no other package or add-on selected for the cluster uses", m.subject())
	case "package":
		return fmt.Sprintf("select one of %s and %s for the cluster, because OLM package %s installs once", m.subject(), other.subject(), s.name)
	}
	if s.kind == "source" {
		return fmt.Sprintf("set olm.subscription.source of %s to a catalog that no other add-on selected for the cluster owns", m.subject())
	}
	return fmt.Sprintf("set olm.catalogSource.name of %s to a CatalogSource name that no other package or add-on selected for the cluster uses", m.subject())
}

func sharedIdentities(m, other selectedMember) []sharedIdentity {
	var shared []sharedIdentity
	namespaces := append([]string{}, other.pkg.PlacementNamespaces...)
	if other.pkg.OLM != nil {
		namespaces = append(namespaces, other.pkg.OLM.Namespace)
		if other.pkg.OLM.Package == m.pkg.OLM.Package {
			shared = append(shared, sharedIdentity{"OLM package", m.pkg.OLM.Package, "package"})
		}
	}
	if slices.Contains(namespaces, m.pkg.OLM.Namespace) {
		shared = append([]sharedIdentity{{"namespace", m.pkg.OLM.Namespace, "namespace"}}, shared...)
	}
	owned, source := m.config.Get("subscription", "catalogSource", "name").Text(), m.config.Get("subscription", "source").Text()
	otherOwned, otherSource := other.config.Get("subscription", "catalogSource", "name").Text(), other.config.Get("subscription", "source").Text()
	switch {
	case owned != "" && (owned == otherOwned || owned == otherSource):
		shared = append(shared, sharedIdentity{"CatalogSource", owned, "catalog"})
	case otherOwned != "" && source == otherOwned:
		shared = append(shared, sharedIdentity{"CatalogSource", source, "source"})
	}
	return shared
}
