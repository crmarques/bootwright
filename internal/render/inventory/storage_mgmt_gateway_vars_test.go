package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageMgmtGatewayVarsAreEmittedWithoutSecrets(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Ingress: v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	got := storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil {
		t.Fatalf("a declared mgmt-gateway without TLS or oauth2-proxy must still emit management vars: the dashboard SSL/port mitigation that frees the gateway port on the mgr hosts runs from them, and skipping it strands every gateway daemon that shares a host with a mgr")
	}
	if _, ok := got["tls"]; ok {
		t.Fatalf("management vars must not invent TLS material the spec does not carry, got %v", got)
	}
	if _, ok := got["oauth2Proxy"]; ok {
		t.Fatalf("management vars must not invent oauth2-proxy material the spec does not carry, got %v", got)
	}
	for _, key := range []string{"hosts", "port", "virtualIP", "enableAuth"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("management vars must carry %q so the management phase can free the gateway port and apply the spec, got %v", key, got)
		}
	}
}

func TestStorageMgmtGatewayVarsAreNilWithoutAGateway(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{}); got != nil {
		t.Fatalf("management vars = %v, want nil when no gateway is declared", got)
	}
}
