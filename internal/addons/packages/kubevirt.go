package packages

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func kubevirt() addons.Package {
	return addons.Package{Name: "kubevirt", Provides: []string{"kubevirt"}, Budget: 45 * time.Minute, OLM: &addons.OLM{Namespace: "openshift-cnv", NamespaceLabels: map[string]string{"openshift.io/cluster-monitoring": "true"}, OperatorGroup: addons.OwnNamespace, GroupName: "kubevirt-hyperconverged-group", Subscription: "hco-operatorhub", Package: "kubevirt-hyperconverged", DefaultSource: "redhat-operators", Approval: addons.Exact}, Render: func(i addons.Instance) (addons.Rendered, error) {
		if _, err := addons.Decode[api.KubeVirtConfig](i.Config); err != nil {
			return addons.Rendered{}, err
		}
		return addons.Rendered{After: []co.Phase{{Gate: []co.Check{crd("hyperconvergeds.hco.kubevirt.io")}, Objects: []co.Object{object("hco.kubevirt.io/v1beta1", "HyperConverged", "openshift-cnv", "kubevirt-hyperconverged", api.MapValue())}}}, Checks: []co.Check{condition("hco.kubevirt.io/v1beta1", "HyperConverged", "openshift-cnv", "kubevirt-hyperconverged", "Available")}}, nil
	}}
}
