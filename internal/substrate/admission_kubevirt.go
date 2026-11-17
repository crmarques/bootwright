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
