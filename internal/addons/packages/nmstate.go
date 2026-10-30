package packages

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func nmstate() addons.Package {
	return addons.Package{Name: "nmstate", Summary: "Kubernetes NMState operator for node network configuration", Provides: []string{"nmstate"}, Budget: 30 * time.Minute, OLM: &addons.OLM{Namespace: "openshift-nmstate", OperatorGroup: addons.OwnNamespace, GroupName: "openshift-nmstate", Subscription: "kubernetes-nmstate-operator", Package: "kubernetes-nmstate-operator", DefaultSource: "redhat-operators", Approval: addons.Exact}, Render: func(i addons.Instance) (addons.Rendered, error) {
		if _, err := addons.Decode[api.NMStateConfig](i.Config); err != nil {
			return addons.Rendered{}, err
		}
		return addons.Rendered{After: []co.Phase{{Gate: []co.Check{crd("nmstates.nmstate.io")}, Objects: []co.Object{object("nmstate.io/v1", "NMState", "", "nmstate", api.Value{})}}}, Checks: []co.Check{condition("nmstate.io/v1", "NMState", "", "nmstate", "Available"), condition("apps/v1", "Deployment", "openshift-nmstate", "nmstate-webhook", "Available")}}, nil
	}}
}
