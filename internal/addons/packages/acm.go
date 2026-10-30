package packages

import (
	"encoding/json"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func acm() addons.Package {
	return addons.Package{Name: "acm", Summary: "Red Hat Advanced Cluster Management hub with its MultiClusterHub", Budget: 45 * time.Minute, Normalize: normalizeACM, OLM: &addons.OLM{Namespace: "open-cluster-management", OperatorGroup: addons.OwnNamespace, GroupName: "open-cluster-management", Subscription: "acm-operator-subscription", Package: "advanced-cluster-management", DefaultSource: "redhat-operators", Approval: addons.Exact}, Admit: admitACM, Render: renderACM}
}

func normalizeACM(v api.Value) api.Value {
	mce := v.Get("mce")
	if mce.Present() {
		v = v.With("mce", mce.Default("source", v.Get("subscription", "source")).Default("sourceNamespace", v.Get("subscription", "sourceNamespace")))
	}
	return v.With("hub", v.Get("hub").Default("disableHubSelfManagement", api.BoolValue(false)))
}

func admitACM(i addons.Instance, f addons.ClusterFacts) []api.Issue {
	if len(f.Nodes) == 1 && i.Config.Get("hub", "availabilityConfig").Text() != "Basic" {
		return []api.Issue{{Code: "api.invariant", Field: ".hub.availabilityConfig", Message: "package acm on single-node OpenShiftCluster/" + i.Cluster + " needs hub availabilityConfig Basic", Remediation: "set hub availabilityConfig to Basic"}}
	}
	return nil
}

func renderACM(i addons.Instance) (addons.Rendered, error) {
	c, err := addons.Decode[api.ACMConfig](i.Config)
	if err != nil {
		return addons.Rendered{}, err
	}
	annotation, err := json.Marshal(map[string]string{"channel": c.MCE.Channel, "installPlanApproval": "Manual", "source": c.MCE.Source, "sourceNamespace": c.MCE.SourceNamespace, "startingCSV": c.MCE.StartingCSV})
	if err != nil {
		return addons.Rendered{}, err
	}
	hub := object("operator.open-cluster-management.io/v1", "MultiClusterHub", "open-cluster-management", "multiclusterhub", i.Config.Get("hub"))
	hub.Body = hub.Body.WithPath(api.MapValue(api.FieldValue{Name: "installer.open-cluster-management.io/mce-subscription-spec", Value: api.StringValue(string(annotation))}), "metadata", "annotations")
	return addons.Rendered{After: []co.Phase{{Gate: []co.Check{crd("multiclusterhubs.operator.open-cluster-management.io")}, Objects: []co.Object{hub}, Approvals: []co.DependentApproval{{Namespace: "multicluster-engine", Subscription: "multicluster-engine", Package: "multicluster-engine", Channel: c.MCE.Channel, Source: c.MCE.Source, SourceNamespace: c.MCE.SourceNamespace, CSV: c.MCE.StartingCSV, Creator: hub.Ref}}}}, Checks: []co.Check{phaseCheck(hub.Ref, "status.phase", "Running")}}, nil
}

func phaseCheck(ref co.Ref, path, value string) co.Check {
	return co.Check{StatusPhase: &co.PhaseCheck{Ref: ref, Path: path, Values: []string{value}}}
}
