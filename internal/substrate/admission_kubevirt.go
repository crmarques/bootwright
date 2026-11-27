package substrate

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeKubeVirt(spec, arm api.Value, catalog api.Catalog) api.Value {
	segments := spec.Get("segments").Items()
	host, hosted := catalog.Find(api.OpenShiftCluster, spec.Get("kubevirt", "hostClusterRef").Text())
	for index, segment := range segments {
		ref := segment.Get("kubevirt", "networkRef")
		if !ref.Present() {
			continue
		}
		segment = segment.WithPath(defaultedNetworkRef(ref, arm.Get("namespace")), "kubevirt", "networkRef")
		if !hosted {
			segments[index] = segment
			continue
		}
		if _, mtu, ok := HostNetworkMTU(host, segment); ok {
			segment = segment.With("mtu", api.IntegerValue(fmt.Sprint(mtu)))
		}
		segments[index] = segment
	}
	if spec.Has("segments") {
		spec = spec.With("segments", api.ListValue(segments...))
	}
	return spec
}

func validateKubeVirt(provider api.Object, catalog api.Catalog) []api.Issue {
	var issues []api.Issue
	arm := provider.Spec().Get("kubevirt")
	templateProfiles := map[string]struct{}{}
	for i, profile := range arm.Get("machineProfiles").Items() {
		if !profile.Has("templateSnapshotRef") {
			continue
		}
		templateProfiles[profile.Get("name").Text()] = struct{}{}
		path := fmt.Sprintf("$.spec.kubevirt.machineProfiles[%d]", i)
		if arm.Has("hostClusterRef") {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".templateSnapshotRef",
				Message:     "a template snapshot profile requires a kubeconfig for an existing virtualization host",
				Remediation: "select spec.kubevirt.kubeconfigRef on " + provider.Identity() + " or remove the template snapshot from this profile"})
		}
		if profile.Has("tpm") {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".tpm",
				Message:     "a template snapshot profile bootstraps a root disk without TPM state",
				Remediation: "remove tpm from this profile on " + provider.Identity()})
		}
	}
	if len(templateProfiles) == 0 {
		return issues
	}
	for _, machine := range HostedMachines(catalog, provider.Name()) {
		if _, usesTemplate := templateProfiles[MachineProfileRef(machine)]; !usesTemplate {
			continue
		}
		install, _ := catalog.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(machine))
		if !install.Spec().Has("installer", "templateClone") {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.kubevirt.machineProfiles",
				Message:     machine.Identity() + " selects a template snapshot profile without selecting a templateClone installer",
				Remediation: "select a templateClone install profile on " + machine.Identity() + " or use a machine profile without a template snapshot"})
		}
	}
	return issues
}

func validateKubeVirtSegment(path string, segment, _ api.Value) []api.Issue {
	var issues []api.Issue
	ref := segment.Get("kubevirt", "networkRef")
	kind := ref.Get("kind").Text()
	if kind == "ClusterUserDefinedNetwork" && ref.Has("namespace") {
		issues = api.AppendIssues(issues, issue(path+".kubevirt.networkRef.namespace", "cluster-scoped networks forbid a namespace"))
	}
	if ref.Present() && !ref.Has("apiGroup") {
		issues = api.AppendIssues(issues, issue(path+".kubevirt.networkRef.apiGroup", "unknown external network kinds require an explicit API group"))
	}
	return issues
}

func defaultedNetworkRef(ref, namespace api.Value) api.Value {
	ref = ref.Default("kind", api.StringValue("ClusterUserDefinedNetwork"))
	switch ref.Get("kind").Text() {
	case "ClusterUserDefinedNetwork", "UserDefinedNetwork":
		ref = ref.Default("apiGroup", api.StringValue("k8s.ovn.org"))
	case "NetworkAttachmentDefinition":
		ref = ref.Default("apiGroup", api.StringValue("k8s.cni.cncf.io"))
	}
	if ref.Get("kind").Text() == "UserDefinedNetwork" || ref.Get("kind").Text() == "NetworkAttachmentDefinition" {
		ref = ref.Default("namespace", namespace)
	}
	return ref
}
