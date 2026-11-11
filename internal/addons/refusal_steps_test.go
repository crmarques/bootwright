package addons

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func requiring(name, capability string) api.Object {
	return manifestAddon(name).WithSpec(manifestAddon(name).Spec().With("requires", api.StringList(capability)))
}

func TestUndecodableSelectionsAreTheirOwnOnlyRefusal(t *testing.T) {
	consumer := requiring("consumer", "capability")
	for _, test := range []struct {
		name        string
		undecodable string
		objects     []api.Object
		owner       api.Object
		refused     api.Object
	}{
		{
			name: "a selected profile", undecodable: "ClusterAddonProfile/broken",
			objects: []api.Object{consumer},
			owner:   obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "profileRefs", api.StringList("broken"), "addonRefs", api.StringList("consumer"))),
		},
		{
			name: "a profile a profile expands", undecodable: "ClusterAddonProfile/broken",
			objects: []api.Object{consumer},
			owner:   obj(api.ClusterAddonProfile, "outer", m("profileRefs", api.StringList("broken"), "addonRefs", api.StringList("consumer"))),
		},
		{
			name: "a selected add-on", undecodable: "ClusterAddon/broken",
			objects: []api.Object{consumer},
			owner:   obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("consumer", "broken"))),
		},
		{
			name: "an add-on another binding of the cluster selects", undecodable: "ClusterAddon/broken",
			objects: []api.Object{consumer, obj(api.ClusterAddonBinding, "other", m("clusterRef", s("cluster"), "addonRefs", api.StringList("broken")))},
			owner:   obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("consumer"))),
			refused: obj(api.ClusterAddonBinding, "other", m("clusterRef", s("cluster"), "addonRefs", api.StringList("broken"))),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := append(slices.Clone(test.objects), test.owner)
			marked := api.NewCatalog(objects).WithUndecodable(map[string]bool{test.undecodable: true})
			if issues := Validate(test.owner, marked); len(issues) != 0 {
				t.Fatalf("a check repeated the undecodable %s: %v", test.undecodable, issues)
			}
			refused := test.owner
			if test.refused.Name() != "" {
				refused = test.refused
			}
			issues := Validate(refused, api.NewCatalog(objects))
			if len(issues) > 0 && !hasCode(issues, "api.reference") {
				t.Fatalf("a missing %s was accepted: %v", test.undecodable, issues)
			}
			for _, issue := range issues {
				if issue.Remediation == "" {
					t.Fatalf("%s at %s names no next step", issue.Code, issue.Field)
				}
			}
		})
	}
}

func TestAddonRefusalsNameANextStep(t *testing.T) {
	binding := func(spec api.Value) api.Object {
		return obj(api.ClusterAddonBinding, "binding", spec.With("clusterRef", s("cluster")))
	}
	provider := manifestAddon("provider").WithSpec(m("provides", api.StringList("capability"), "requires", api.StringList("reverse")))
	consumer := manifestAddon("consumer").WithSpec(m("provides", api.StringList("reverse"), "requires", api.StringList("capability")))
	for _, test := range []struct {
		name, field, remediation string
		owner                    api.Object
		others                   []api.Object
	}{
		{"no selection", "$.spec", "add a package to spec.packages, a profile to spec.profileRefs or an add-on to spec.addonRefs", binding(m()), nil},
		{"a profile cycle", "$.spec.profileRefs[0]", "remove the reference to first from this list or from a profile it expands",
			binding(m("profileRefs", api.StringList("first"))), []api.Object{
				obj(api.ClusterAddonProfile, "first", m("profileRefs", api.StringList("second"))),
				obj(api.ClusterAddonProfile, "second", m("profileRefs", api.StringList("first"))),
			}},
		{"an add-on bound twice", "$.spec.addonRefs[0]", "bind a to cluster through one ClusterAddonBinding only",
			binding(m("addonRefs", api.StringList("a"))), []api.Object{manifestAddon("a"), obj(api.ClusterAddonBinding, "before", m("clusterRef", s("cluster"), "addonRefs", api.StringList("a")))}},
		{"a capability nothing provides", "$.spec.addonRefs[0]", "select a package or add-on that provides capability for OpenShiftCluster/cluster, or remove ClusterAddon/consumer from the selection",
			binding(m("addonRefs", api.StringList("consumer"))), []api.Object{requiring("consumer", "capability")}},
		{"a capability cycle", "$.spec.addonRefs[1]", "remove one requires entry among ClusterAddon/consumer, ClusterAddon/provider to break the cycle",
			binding(m("addonRefs", api.StringList("provider", "consumer"))), []api.Object{provider, consumer}},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := Validate(test.owner, api.NewCatalog(append(slices.Clone(test.others), test.owner)))
			if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
				return issue.Field == test.field && issue.Remediation == test.remediation
			}) {
				t.Fatalf("issues = %+v, want %q at %s", issues, test.remediation, test.field)
			}
			for _, issue := range issues {
				if issue.Remediation == "" {
					t.Fatalf("%s at %s names no next step", issue.Code, issue.Field)
				}
			}
		})
	}
}
