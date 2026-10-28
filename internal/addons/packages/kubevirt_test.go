package packages

import (
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
	if len(rendered.Checks) != 1 || rendered.Checks[0].Condition.Ref.Kind != "HyperConverged" || rendered.Checks[0].Condition.Type != "Available" {
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
