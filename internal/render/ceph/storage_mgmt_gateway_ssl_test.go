package ceph_test

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	ceph "github.com/crmarques/bootwright/internal/render/ceph"
)

func mgmtGatewayClusterWithExposure(distribution, exposure string) v1alpha1.StorageCluster {
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Distribution: distribution,
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Exposure: exposure,
			Ingress:  v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "node-01"}, Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	return cluster
}

func TestCephadmMgmtGatewaySpecDisablesSSLForHTTPExposure(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionIBM, ""} {
		docs := ceph.CephadmLateServicesSpec(v1alpha1.State{}, mgmtGatewayClusterWithExposure(distribution, v1alpha1.StorageCephMgmtGatewayExposureHTTP))
		gw := serviceDoc(t, docsFromSpecs(t, docs), "mgmt-gateway", "")
		spec := gw["spec"].(map[string]any)
		if got, ok := spec["ssl"]; !ok || got != false {
			t.Fatalf("exposure: http must pin ssl: false on distribution %q — on vendor cephadm builds it is the only shape that converges, because they recompute certificate dependencies for every certificate source but record the daemon's dependencies without them, got %v", distribution, spec)
		}
	}
}

func TestCephadmMgmtGatewaySpecPortFollowsExposure(t *testing.T) {
	for _, tc := range []struct {
		exposure string
		port     int
		want     int
	}{
		{v1alpha1.StorageCephMgmtGatewayExposureHTTP, 0, 8888},
		{v1alpha1.StorageCephMgmtGatewayExposureHTTPS, 0, 8443},
		{"", 0, 8443},
		{v1alpha1.StorageCephMgmtGatewayExposureHTTP, 8080, 8080},
	} {
		cluster := mgmtGatewayClusterWithExposure("", tc.exposure)
		cluster.Spec.Ceph.MgmtGateway.Port = tc.port
		docs := ceph.CephadmLateServicesSpec(v1alpha1.State{}, cluster)
		gw := serviceDoc(t, docsFromSpecs(t, docs), "mgmt-gateway", "")
		spec := gw["spec"].(map[string]any)
		if got := spec["port"]; got != tc.want {
			t.Fatalf("exposure %q with authored port %d must serve port %d — a cleartext listener on a TLS-conventional port misreads to every operator, scanner and firewall rule, got %v", tc.exposure, tc.port, tc.want, got)
		}
	}
}

func TestCephadmMgmtGatewaySpecKeepsSSLForHTTPSExposure(t *testing.T) {
	for _, tc := range []struct{ distribution, exposure string }{
		{v1alpha1.StorageCephDistributionIBM, v1alpha1.StorageCephMgmtGatewayExposureHTTPS},
		{"", ""},
	} {
		docs := ceph.CephadmLateServicesSpec(v1alpha1.State{}, mgmtGatewayClusterWithExposure(tc.distribution, tc.exposure))
		gw := serviceDoc(t, docsFromSpecs(t, docs), "mgmt-gateway", "")
		spec := gw["spec"].(map[string]any)
		if _, ok := spec["ssl"]; ok {
			t.Fatalf("exposure defaults to https and an explicit https is the operator's declaration for a build whose dependency recording is repaired, so the doc must keep cephadm's ssl default (distribution %q exposure %q), got %v", tc.distribution, tc.exposure, spec)
		}
	}
}
