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

func TestStorageMgmtGatewayVarsDisableSSLOnVendorBuilds(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionIBM,
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Ingress: v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	got := storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil || got["sslDisabled"] != true {
		t.Fatalf("a vendor cephadm build records mgmt-gateway daemon dependencies without the certificate entries it recomputes for every certificate source, its own cephadm-signed one included, so the only convergent gateway spec disables ssl; the management vars must carry sslDisabled, got %v", got)
	}
	cluster.Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionOSS
	got = storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil {
		t.Fatalf("management vars must be emitted for an oss gateway")
	}
	if _, ok := got["sslDisabled"]; ok {
		t.Fatalf("upstream tentacle computes no certificate dependencies and converges with cephadm-signed HTTPS, so oss must keep ssl enabled, got %v", got)
	}
}

func TestStorageMgmtGatewayVarsAreNilWithoutAGateway(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{}); got != nil {
		t.Fatalf("management vars = %v, want nil when no gateway is declared", got)
	}
}
