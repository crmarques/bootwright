package packages

import (
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func TestKubeVirtPackageShape(t *testing.T) {
	p, ok := Builtin().Find("kubevirt")
	if !ok {
		t.Fatal("kubevirt is not registered")
	}
	olm := p.OLM
	if p.Budget != 45*time.Minute || p.Last || len(p.Requires) != 0 || len(p.Provides) != 1 || p.Provides[0] != "kubevirt" {
		t.Fatal(p)
	}
	if olm.Namespace != "openshift-cnv" || olm.NamespaceLabels["openshift.io/cluster-monitoring"] != "true" || olm.OperatorGroup != addons.OwnNamespace || olm.GroupName != "kubevirt-hyperconverged-group" || olm.Subscription != "hco-operatorhub" || olm.Package != "kubevirt-hyperconverged" || olm.DefaultSource != "redhat-operators" || olm.Approval != addons.Exact {
		t.Fatal(olm)
	}
}

func TestKubeVirtRendersTheEmptyHyperConvergedAfterItsCRD(t *testing.T) {
	rendered, err := kubevirt().Render(addons.Instance{Config: packageTestConfig("kubevirt")})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.After) != 1 || len(rendered.After[0].Objects) != 1 || len(rendered.After[0].Gate) != 1 || rendered.After[0].Gate[0].Condition.Name != "hyperconvergeds.hco.kubevirt.io" {
		t.Fatal(rendered)
	}
	object := rendered.After[0].Objects[0]
	if object.Ref.Namespace != "openshift-cnv" || object.Ref.Name != "kubevirt-hyperconverged" || len(object.Body.Get("spec").Fields()) != 0 {
		t.Fatal(object)
	}
	if len(rendered.Checks) != 2 || rendered.Checks[0].Condition.Ref.Kind != "HyperConverged" || rendered.Checks[0].Condition.Type != "Available" || rendered.Checks[0].Condition.Status != "True" ||
		rendered.Checks[1].Condition.Type != "Degraded" || rendered.Checks[1].Condition.Status != "False" {
		t.Fatal(rendered.Checks)
	}
}

func TestKubeVirtConfigurationIsClosedAndDefaultsItsTimeout(t *testing.T) {
	if got := packageTestConfig("kubevirt").Get("timeout").Text(); got != "45m0s" {
		t.Fatal("timeout default", got)
	}
	extra := packageTestConfig("kubevirt").With("featureGates", api.MapValue())
	if _, err := addons.Decode[api.KubeVirtConfig](extra); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

func hostedFacts() addons.ClusterFacts {
	return addons.ClusterFacts{Name: "host", HostedNamespaces: []addons.HostedNamespace{
		{Name: "guests-a", Labels: map[string]string{"bootwright.example.com/net": "machine", "tier": "hub"}},
		{Name: "guests-b"},
	}}
}

func TestKubeVirtOwnsEachHostedNamespaceItsAccountAndItsBindings(t *testing.T) {
	rendered, err := kubevirt().Render(addons.Instance{Cluster: "host", Config: packageTestConfig("kubevirt"), Facts: hostedFacts()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.After) != 2 || len(rendered.After[1].Gate) != 0 {
		t.Fatal(rendered.After)
	}
	var names []string
	for _, object := range rendered.After[1].Objects {
		names = append(names, object.Kind+"/"+object.Namespace+"/"+object.Name)
	}
	want := []string{"Namespace//guests-a", "ServiceAccount/guests-a/bootwright-kubevirt", "RoleBinding/guests-a/bootwright-kubevirt",
		"Namespace//guests-b", "ServiceAccount/guests-b/bootwright-kubevirt", "RoleBinding/guests-b/bootwright-kubevirt",
		"ClusterRole//bootwright-kubevirt-machines", "ClusterRole//bootwright-kubevirt-proof", "ClusterRoleBinding//bootwright-kubevirt-proof",
		"Role/openshift-config-managed/bootwright-kubevirt-proof", "RoleBinding/openshift-config-managed/bootwright-kubevirt-proof",
		"Role/openshift-cnv/bootwright-kubevirt-proof", "RoleBinding/openshift-cnv/bootwright-kubevirt-proof"}
	if !slices.Equal(names, want) {
		t.Fatal(names)
	}
	labels := rendered.After[1].Objects[0].Body.Get("metadata", "labels")
	if labels.Get("tier").Text() != "hub" || labels.Get("bootwright.example.com/net").Text() != "machine" || rendered.After[1].Objects[3].Body.Get("metadata", "labels").Present() {
		t.Fatal(labels)
	}
	if ref := rendered.After[1].Objects[10].Body.Get("roleRef", "kind").Text(); ref != "Role" {
		t.Fatal(ref)
	}
	if subjects := rendered.After[1].Objects[8].Body.Get("subjects"); subjects.Len() != 2 {
		t.Fatal(subjects)
	}
}

func TestKubeVirtRefusesANamespaceItsNetworksLabelDifferently(t *testing.T) {
	facts := hostedFacts()
	facts.HostedNamespaces[0].Conflict = "label tier is \"hub\" for one secondary network of host and \"edge\" for edge"
	_, err := kubevirt().Render(addons.Instance{Cluster: "host", Config: packageTestConfig("kubevirt"), Facts: facts})
	if err == nil || !strings.Contains(err.Error(), "guests-a") || !strings.Contains(err.Error(), "edge") {
		t.Fatal(err)
	}
}
