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

func TestStorageMgmtGatewayVarsFollowTheDeclaredExposure(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionIBM,
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Exposure: v1alpha1.StorageCephMgmtGatewayExposureHTTP,
			Ingress:  v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	got := storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil || got["sslDisabled"] != true {
		t.Fatalf("exposure: http is the shape that converges on vendor cephadm builds — they record mgmt-gateway daemon dependencies without the certificate entries they recompute for every certificate source, cephadm-signed included — and the management vars must carry sslDisabled so the phase pins ssl false and repairs the store, got %v", got)
	}
	cluster.Spec.Ceph.MgmtGateway.Exposure = v1alpha1.StorageCephMgmtGatewayExposureHTTPS
	got = storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil {
		t.Fatalf("management vars must be emitted for an https gateway")
	}
	if _, ok := got["sslDisabled"]; ok {
		t.Fatalf("an explicit exposure: https keeps cephadm's HTTPS — the operator's declaration for a vendor build that repairs the dependency recording — so the vars must not disable ssl, got %v", got)
	}
	cluster.Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionOSS
	cluster.Spec.Ceph.MgmtGateway.Exposure = ""
	got = storageMgmtGatewayVars(cluster, nil, PathOptions{})
	if got == nil {
		t.Fatalf("management vars must be emitted for an oss gateway")
	}
	if _, ok := got["sslDisabled"]; ok {
		t.Fatalf("exposure defaults to https, so an unset field must not disable ssl, got %v", got)
	}
}

func TestStorageMgmtGatewayVarsPortFollowsExposure(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Ingress: v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{})["port"]; got != 8443 {
		t.Fatalf("an https gateway must serve the TLS-conventional 8443, got %v", got)
	}
	cluster.Spec.Ceph.MgmtGateway.Exposure = v1alpha1.StorageCephMgmtGatewayExposureHTTP
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{})["port"]; got != 8888 {
		t.Fatalf("a cleartext gateway must not default onto a TLS-conventional port — the dashboard-port vacation and every firewall rule read the number, not the spec, got %v", got)
	}
	cluster.Spec.Ceph.MgmtGateway.Port = 8080
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{})["port"]; got != 8080 {
		t.Fatalf("an authored port must win over the exposure default, got %v", got)
	}
}

func TestStorageMgmtGatewayVarsAreNilWithoutAGateway(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
	if got := storageMgmtGatewayVars(cluster, nil, PathOptions{}); got != nil {
		t.Fatalf("management vars = %v, want nil when no gateway is declared", got)
	}
}
