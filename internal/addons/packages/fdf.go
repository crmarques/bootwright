package packages

import (
	"fmt"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func fdf() addons.Package {
	return addons.Package{Name: "fdf", Provides: []string{"dataFoundation"}, Budget: time.Hour, Normalize: func(v api.Value) api.Value { return v.Default("defaultStorageClass", api.BoolValue(false)) }, OLM: &addons.OLM{Namespace: "openshift-storage", NamespaceLabels: map[string]string{"openshift.io/cluster-monitoring": "true"}, OperatorGroup: addons.OwnNamespace, GroupName: "openshift-storage", Subscription: "odf-operator", Package: "odf-operator", Approval: addons.OperatorManaged}, Admit: admitFDF, Render: renderFDF}
}

func admitFDF(i addons.Instance, facts addons.ClusterFacts) []api.Issue {
	issues := []api.Issue{}
	sub := i.Config.Get("subscription")
	source := sub.Get("source").Text()
	if !sub.Has("catalogSource") && !slices.Contains([]string{"redhat-operators", "certified-operators", "community-operators", "redhat-marketplace"}, source) {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: ".subscription.source", Message: "package fdf subscribes from catalog " + source + ", which no cluster Bootwright installs serves", Remediation: "declare subscription.catalogSource named " + source + " with the IBM catalog image and an explicit priority, or name redhat-operators"})
	}
	name := i.Config.Get("cephConsumerRef").Text()
	for _, export := range facts.Exports {
		if export.Name == name && export.TLS && (facts.Trust.Policy != "Always" || facts.Trust.Bundles == 0) {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: ".cephConsumerRef", Message: "CephConsumer/" + name + " attaches an object gateway over TLS, and OpenShiftCluster/" + facts.Name + " adds no trust bundle to its cluster-wide trusted CA", Remediation: "add the gateway certificate's issuing CA to spec.install.additionalTrustBundleRefs of " + facts.Name + " under additionalTrustBundlePolicy Always"})
		}
	}
	return issues
}

func renderFDF(i addons.Instance) (addons.Rendered, error) {
	c, err := addons.Decode[api.FDFConfig](i.Config)
	if err != nil {
		return addons.Rendered{}, err
	}
	if c.CephConsumerRef == "" {
		return addons.Rendered{}, fmt.Errorf("fdf requires a CephConsumer reference")
	}
	namespace, name := "openshift-storage", "ocs-external-storagecluster"
	spec := api.MapValue(api.FieldValue{Name: "externalStorage", Value: api.MapValue(api.FieldValue{Name: "enable", Value: api.BoolValue(true)})}, api.FieldValue{Name: "managedResources", Value: api.MapValue(api.FieldValue{Name: "cephBlockPools", Value: api.MapValue(api.FieldValue{Name: "defaultStorageClass", Value: api.BoolValue(c.DefaultStorageClass)})})})
	storage := object("ocs.openshift.io/v1", "StorageCluster", namespace, name, spec)
	r := addons.Rendered{Requires: []reconciliation.ObjectRef{{Kind: "CephConsumer", Object: c.CephConsumerRef}}, After: []co.Phase{{Gate: []co.Check{crd("storageclusters.ocs.openshift.io")}, Secrets: []co.SecretPlacement{{Produced: &co.ProducedRef{Kind: "CephConsumer", Object: c.CephConsumerRef, Output: "external-details"}, ProducedKey: "external_cluster_details", Namespace: namespace, Name: "rook-ceph-external-cluster-details", Type: "Opaque"}}, Objects: []co.Object{storage}}}, Checks: []co.Check{{StatusPhase: &co.PhaseCheck{Ref: storage.Ref, Values: []string{"Ready"}}}, condition("ocs.openshift.io/v1", "StorageCluster", namespace, name, "ReconcileComplete"), {StatusPhase: &co.PhaseCheck{Ref: co.Ref{APIVersion: "noobaa.io/v1alpha1", Kind: "NooBaa", Namespace: namespace, Name: "noobaa", Namespaced: true}, Values: []string{"Ready"}}}}}
	classes := []string{name + "-ceph-rbd", name + "-cephfs", "openshift-storage.noobaa.io"}
	for _, export := range i.Facts.Exports {
		if export.Name == c.CephConsumerRef && export.Gateway {
			classes = append(classes, name+"-ceph-rgw")
		}
	}
	for _, class := range classes {
		r.Checks = append(r.Checks, co.Check{ResourceExists: &co.Ref{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: class}})
	}
	return r, nil
}
