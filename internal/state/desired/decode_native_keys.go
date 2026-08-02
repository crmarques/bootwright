package desiredstate

import (
	"reflect"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type nativeKeyLocation struct {
	kind  string
	field string
}

var nativeKeyRedirects = map[nativeKeyLocation]string{
	{v1alpha1.KindContainerCluster, "baseDomain"}:            "Environment spec.domains.containerClusters owns the fleet DNS zone every container cluster installs under",
	{v1alpha1.KindContainerCluster, "controlPlane"}:          "control-plane and compute replica counts are derived from spec.nodes[].role",
	{v1alpha1.KindContainerCluster, "compute"}:               "control-plane and compute replica counts are derived from spec.nodes[].role",
	{v1alpha1.KindContainerCluster, "sshKey"}:                "spec.install.nodeSSH.keyPairRef names the sshKeyPair Secret; desired state carries no key material",
	{v1alpha1.KindContainerCluster, "pullSecret"}:            "spec.install.pullSecretRef names the pull-secret Secret; desired state carries no secret bytes",
	{v1alpha1.KindContainerCluster, "additionalTrustBundle"}: "spec.install.additionalTrustBundleRefs[] names the trust-bundle Secrets; desired state carries no certificate bytes",
	{v1alpha1.KindContainerCluster, "apiVIPs"}:               "spec.install.endpoints.api owns the API VIP: set address, source.type: infraComponent, or source.type: node on a single-node cluster. The slot carries one address because single-stack is the current scope, so there is no per-family list to author",
	{v1alpha1.KindContainerCluster, "ingressVIPs"}:           "spec.install.endpoints.ingress owns the ingress VIP: set address, source.type: infraComponent, or source.type: node on a single-node cluster. The slot carries one address because single-stack is the current scope, so there is no per-family list to author",
	{v1alpha1.KindContainerCluster, "machineNetwork"}:        "NetworkConfig spec.machineNetwork[] owns the machine networks, and each node reaches it through spec.nodes[].machineRef",
}

func nativeKeyRedirect(value any, field string) (string, bool) {
	kind := authoredKindName(value)
	if kind == "" {
		return "", false
	}
	where, ok := nativeKeyRedirects[nativeKeyLocation{kind: kind, field: field}]
	return where, ok
}

func authoredKindName(value any) string {
	t := reflect.TypeOf(value)
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	if !knownResourceKind(t.Name()) {
		return ""
	}
	return t.Name()
}
