package ceph_test

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	ceph "github.com/crmarques/bootwright/internal/render/ceph"
)

func mgmtGatewayClusterWithDistribution(distribution string) v1alpha1.StorageCluster {
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Distribution: distribution,
		MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
			Ingress: v1alpha1.StorageCephMgmtGatewayIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "node-01"}, Roles: []string{v1alpha1.StorageCephRoleIngress}},
		}},
	}
	return cluster
}

func TestCephadmMgmtGatewaySpecDisablesSSLOnVendorBuilds(t *testing.T) {
	docs := ceph.CephadmLateServicesSpec(v1alpha1.State{}, mgmtGatewayClusterWithDistribution(v1alpha1.StorageCephDistributionIBM))
	gw := serviceDoc(t, docsFromSpecs(t, docs), "mgmt-gateway", "")
	spec := gw["spec"].(map[string]any)
	if got, ok := spec["ssl"]; !ok || got != false {
		t.Fatalf("a vendor cephadm build recomputes mgmt-gateway certificate dependencies for every certificate source, its own cephadm-signed one included, but records the daemon's dependencies without them, so any ssl-enabled gateway reconfigures forever; the vendor doc must pin ssl: false, got %v", spec)
	}
}

func TestCephadmMgmtGatewaySpecKeepsSSLOnCommunityBuilds(t *testing.T) {
	docs := ceph.CephadmLateServicesSpec(v1alpha1.State{}, mgmtGatewayClusterWithDistribution(""))
	gw := serviceDoc(t, docsFromSpecs(t, docs), "mgmt-gateway", "")
	spec := gw["spec"].(map[string]any)
	if _, ok := spec["ssl"]; ok {
		t.Fatalf("upstream tentacle computes no certificate dependencies and converges with cephadm-signed HTTPS, so a community gateway must keep cephadm's ssl default, got %v", spec)
	}
}
