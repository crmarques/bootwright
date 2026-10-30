package packages

import (
	"fmt"
	"maps"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

const (
	hostedAccount      = "bootwright-kubevirt"
	machinesRole       = "bootwright-kubevirt-machines"
	proofRole          = "bootwright-kubevirt-proof"
	rbacVersion        = "rbac.authorization.k8s.io/v1"
	hyperConverged     = "kubevirt-hyperconverged"
	hyperConvergedKind = "HyperConverged"
	hcoVersion         = "hco.kubevirt.io/v1beta1"
)

var machineRules = []rule{
	{"kubevirt.io", "virtualmachines", []string{"create", "delete", "get", "patch"}},
	{"kubevirt.io", "virtualmachineinstances", []string{"get"}},
	{"subresources.kubevirt.io", "virtualmachines/start", []string{"update"}},
	{"subresources.kubevirt.io", "virtualmachines/stop", []string{"update"}},
	{"cdi.kubevirt.io", "datavolumes", []string{"create", "delete", "get"}},
	{"upload.cdi.kubevirt.io", "uploadtokenrequests", []string{"create"}},
	{"", "persistentvolumeclaims", []string{"get", "list"}},
	{"k8s.cni.cncf.io", "network-attachment-definitions", []string{"get"}},
}

var proofRules = []rule{
	{"", "namespaces", []string{"get"}},
	{"cdi.kubevirt.io", "cdiconfigs", []string{"get"}},
	{"cdi.kubevirt.io", "storageprofiles", []string{"get"}},
	{"storage.k8s.io", "storageclasses", []string{"get", "list"}},
	{"k8s.ovn.org", "clusteruserdefinednetworks", []string{"get"}},
}

type rule struct {
	group, resource string
	verbs           []string
}

func MachineAccess() [][3]string {
	var access [][3]string
	for _, granted := range machineRules {
		for _, verb := range granted.verbs {
			access = append(access, [3]string{granted.group, granted.resource, verb})
		}
	}
	return access
}

func kubevirt() addons.Package {
	return addons.Package{Name: "kubevirt", Summary: "OpenShift Virtualization (KubeVirt) with its HyperConverged instance", Provides: []string{"kubevirt"}, Budget: 45 * time.Minute, OLM: &addons.OLM{Namespace: "openshift-cnv", NamespaceLabels: map[string]string{"openshift.io/cluster-monitoring": "true"}, OperatorGroup: addons.OwnNamespace, GroupName: "kubevirt-hyperconverged-group", Subscription: "hco-operatorhub", Package: "kubevirt-hyperconverged", DefaultSource: "redhat-operators", Approval: addons.Exact}, Render: renderKubeVirt}
}

func renderKubeVirt(i addons.Instance) (addons.Rendered, error) {
	if _, err := addons.Decode[api.KubeVirtConfig](i.Config); err != nil {
		return addons.Rendered{}, err
	}
	degraded := condition(hcoVersion, hyperConvergedKind, "openshift-cnv", hyperConverged, "Degraded")
	degraded.Condition.Status = "False"
	rendered := addons.Rendered{
		After:  []co.Phase{{Gate: []co.Check{crd("hyperconvergeds.hco.kubevirt.io")}, Objects: []co.Object{object(hcoVersion, hyperConvergedKind, "openshift-cnv", hyperConverged, api.MapValue())}}},
		Checks: []co.Check{condition(hcoVersion, hyperConvergedKind, "openshift-cnv", hyperConverged, "Available"), degraded},
	}
	hosted := i.Facts.HostedNamespaces
	if len(hosted) == 0 {
		return rendered, nil
	}
	var objects []co.Object
	names := []string{"kube-system"}
	var subjects []api.Value
	for _, namespace := range hosted {
		if namespace.Conflict != "" {
			return addons.Rendered{}, fmt.Errorf("namespace %s cannot carry the selector of every secondary network its providers attach to: %s", namespace.Name, namespace.Conflict)
		}
		names = append(names, namespace.Name)
		objects = append(objects, labelledNamespace(namespace), object("v1", "ServiceAccount", namespace.Name, hostedAccount, api.Value{}),
			binding("RoleBinding", namespace.Name, hostedAccount, machinesRole, subject(namespace.Name)))
		subjects = append(subjects, subject(namespace.Name))
	}
	slices.Sort(names)
	objects = append(objects, clusterRole(machinesRole, machineRules, nil),
		clusterRole(proofRole, proofRules, map[string][]string{"namespaces": names, "cdiconfigs": {"config"}}),
		binding("ClusterRoleBinding", "", proofRole, proofRole, subjects...))
	for _, read := range [][2]string{{"openshift-config-managed", "default-ingress-cert"}, {"openshift-cnv", "kubemacpool-mac-range-config"}} {
		objects = append(objects, configMapRole(read[0], read[1]), roleBinding(read[0], subjects))
	}
	rendered.After = append(rendered.After, co.Phase{Objects: objects})
	return rendered, nil
}

func labelledNamespace(namespace addons.HostedNamespace) co.Object {
	created := object("v1", "Namespace", "", namespace.Name, api.Value{})
	if len(namespace.Labels) == 0 {
		return created
	}
	labels := api.MapValue()
	for _, key := range slices.Sorted(maps.Keys(namespace.Labels)) {
		labels = labels.With(key, api.StringValue(namespace.Labels[key]))
	}
	created.Body = created.Body.WithPath(labels, "metadata", "labels")
	return created
}

func subject(namespace string) api.Value {
	return api.MapValue(api.FieldValue{Name: "kind", Value: api.StringValue("ServiceAccount")}, api.FieldValue{Name: "name", Value: api.StringValue(hostedAccount)}, api.FieldValue{Name: "namespace", Value: api.StringValue(namespace)})
}

func binding(kind, namespace, name, role string, subjects ...api.Value) co.Object {
	bound := object(rbacVersion, kind, namespace, name, api.Value{})
	bound.Body = bound.Body.With("roleRef", api.MapValue(api.FieldValue{Name: "apiGroup", Value: api.StringValue("rbac.authorization.k8s.io")}, api.FieldValue{Name: "kind", Value: api.StringValue("ClusterRole")}, api.FieldValue{Name: "name", Value: api.StringValue(role)})).With("subjects", api.ListValue(subjects...))
	return bound
}

func clusterRole(name string, rules []rule, resourceNames map[string][]string) co.Object {
	role := object(rbacVersion, "ClusterRole", "", name, api.Value{})
	entries := make([]api.Value, 0, len(rules))
	for _, granted := range rules {
		entry := api.MapValue(api.FieldValue{Name: "apiGroups", Value: textList([]string{granted.group})}, api.FieldValue{Name: "resources", Value: textList([]string{granted.resource})}, api.FieldValue{Name: "verbs", Value: textList(granted.verbs)})
		if named := resourceNames[granted.resource]; len(named) != 0 {
			entry = entry.With("resourceNames", textList(named))
		}
		entries = append(entries, entry)
	}
	role.Body = role.Body.With("rules", api.ListValue(entries...))
	return role
}

func configMapRole(namespace, name string) co.Object {
	role := object(rbacVersion, "Role", namespace, proofRole, api.Value{})
	role.Body = role.Body.With("rules", api.ListValue(api.MapValue(api.FieldValue{Name: "apiGroups", Value: textList([]string{""})},
		api.FieldValue{Name: "resources", Value: textList([]string{"configmaps"})}, api.FieldValue{Name: "resourceNames", Value: textList([]string{name})},
		api.FieldValue{Name: "verbs", Value: textList([]string{"get"})})))
	return role
}

func roleBinding(namespace string, subjects []api.Value) co.Object {
	bound := binding("RoleBinding", namespace, proofRole, proofRole, subjects...)
	bound.Body = bound.Body.WithPath(api.StringValue("Role"), "roleRef", "kind")
	return bound
}

func textList(values []string) api.Value {
	items := make([]api.Value, 0, len(values))
	for _, value := range values {
		items = append(items, api.StringValue(value))
	}
	return api.ListValue(items...)
}
