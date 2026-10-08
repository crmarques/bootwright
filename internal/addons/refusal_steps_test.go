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
	tokenInput := m("name", s("credential"), "secretType", s("token"))
	secretAddon := manifestAddon("pulls").WithSpec(manifestAddon("pulls").Spec().With("inputs", l(tokenInput)))
	storageAddon := manifestAddon("storage").WithSpec(manifestAddon("storage").Spec().With("provides", api.StringList("dataFoundation")).With("readiness", m("checks", l(m()))).With("inputs", l(storageInput("export"))))
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
			owner: obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "profileRefs", api.StringList("broken"), "addonRefs", api.StringList("consumer"),
				"addonConfigs", l(m("addonRef", s("expanded"))))),
		},
		{
			name: "a profile a profile expands", undecodable: "ClusterAddonProfile/broken",
			objects: []api.Object{consumer},
			owner:   obj(api.ClusterAddonProfile, "outer", m("profileRefs", api.StringList("broken"), "addonRefs", api.StringList("consumer"))),
		},
		{
			name: "a selected add-on", undecodable: "ClusterAddon/broken",
			objects: []api.Object{consumer},
			owner: obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("consumer", "broken"),
				"addonConfigs", l(m("addonRef", s("broken"))))),
		},
		{
			name: "a StorageExport input target", undecodable: "StorageExport/export",
			objects: []api.Object{storageAddon},
			owner: obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("storage"),
				"addonConfigs", l(m("addonRef", s("storage"), "inputs", l(m("name", s("export"), "value", s("export"))))))),
		},
		{
			name: "a Secret input target", undecodable: "Secret/credential",
			objects: []api.Object{secretAddon},
			owner: obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("pulls"),
				"addonConfigs", l(m("addonRef", s("pulls"), "inputs", l(m("name", s("credential"), "value", s("credential"))))))),
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
			if !hasCode(issues, "api.reference") {
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
	tokenAddon := manifestAddon("pulls").WithSpec(manifestAddon("pulls").Spec().With("inputs", l(m("name", s("credential"), "secretType", s("token")))))
	configured := func(inputs api.Value) api.Object {
		return binding(m("addonRefs", api.StringList("pulls"), "addonConfigs", l(m("addonRef", s("pulls"), "inputs", inputs))))
	}
	provider := manifestAddon("provider").WithSpec(m("provides", api.StringList("capability"), "requires", api.StringList("reverse")))
	consumer := manifestAddon("consumer").WithSpec(m("provides", api.StringList("reverse"), "requires", api.StringList("capability")))
	for _, test := range []struct {
		name, field, remediation string
		owner                    api.Object
		others                   []api.Object
	}{
		{"no selection", "$.spec", "add a profile to spec.profileRefs or an add-on to spec.addonRefs", binding(m()), nil},
		{"a profile cycle", "$.spec.profileRefs", "remove the profile reference that leads back to a profile already being expanded",
			binding(m("profileRefs", api.StringList("first"))), []api.Object{
				obj(api.ClusterAddonProfile, "first", m("profileRefs", api.StringList("second"))),
				obj(api.ClusterAddonProfile, "second", m("profileRefs", api.StringList("first"))),
			}},
		{"a missing profile", "$.spec.profileRefs", "declare each profile named in spec.profileRefs, and in the profiles it expands, exactly once as a ClusterAddonProfile, or remove the name",
			binding(m("profileRefs", api.StringList("missing"))), nil},
		{"a missing add-on", "$.spec.addonRefs", "declare each add-on named in spec.addonRefs, and in the profiles this selection expands, exactly once as a ClusterAddon, or remove the name",
			binding(m("addonRefs", api.StringList("missing"))), nil},
		{"a configuration that selects", "$.spec.addonConfigs[0].addonRef", "select the add-on through spec.addonRefs or a profile, or remove this spec.addonConfigs entry",
			binding(m("addonRefs", api.StringList("a"), "addonConfigs", l(m("addonRef", s("b"))))), []api.Object{manifestAddon("a"), manifestAddon("b")}},
		{"a required input missing", "$.spec.addonConfigs", "supply each required input of the selected add-on once in its spec.addonConfigs entry",
			binding(m("addonRefs", api.StringList("pulls"))), []api.Object{tokenAddon}},
		{"an input of the wrong Secret type", "$.spec.addonConfigs", "set the input's value to the name of a declared object of the input's resourceKind, or of a Secret of its secretType",
			configured(l(m("name", s("credential"), "value", s("credential")))), []api.Object{tokenAddon, obj(api.Secret, "credential", m("type", s("opaque")))}},
		{"an undeclared input", "$.spec.addonConfigs", "remove the input, or use an input name the add-on declares in its spec.inputs",
			configured(l(m("name", s("credential"), "value", s("credential")), m("name", s("undeclared"), "value", s("credential")))),
			[]api.Object{tokenAddon, obj(api.Secret, "credential", m("type", s("token")))}},
		{"an add-on bound twice", "$.spec.clusterRef", "bind each add-on to the cluster through one ClusterAddonBinding only",
			binding(m("addonRefs", api.StringList("a"))), []api.Object{manifestAddon("a"), obj(api.ClusterAddonBinding, "other", m("clusterRef", s("cluster"), "addonRefs", api.StringList("a")))}},
		{"a capability nothing provides", "$.spec", "select an add-on that provides the required capability, or remove the add-on that requires it",
			binding(m("addonRefs", api.StringList("consumer"))), []api.Object{requiring("consumer", "capability")}},
		{"a capability cycle", "$.spec", "remove a requires entry from one of the selected add-ons to break the cycle",
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
