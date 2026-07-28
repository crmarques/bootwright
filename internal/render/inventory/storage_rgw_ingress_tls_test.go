package inventory

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageRGWIngressTLSVars(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "node-1", MachineRef: v1alpha1.LocalObjectReference{Name: "node-1"}, Roles: []string{"ingress"}},
					},
				},
			},
		},
	}
	gw := v1alpha1.StorageObjectGateway{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			Public:            v1alpha1.StorageObjectGatewayPublic{DNSLabel: "rgw", Scheme: "https", Port: 8443},
			Ceph: v1alpha1.StorageObjectGatewayCephSpec{
				ServiceID: "odf",
				Ingresses: []v1alpha1.StorageObjectGatewayIngress{
					{
						Name: "no-tls", Address: "10.0.0.8", PrefixLength: 24,
					},
					{
						Name: "ha", Address: "10.0.0.9", PrefixLength: 24,
						VirtualInterfaceNetworks: []string{"10.0.0.0/24"},
						FirstVirtualRouterID:     51,
						TLS: &v1alpha1.StorageObjectGatewayIngressTLS{
							CertificateRef: v1alpha1.LocalObjectReference{Name: "rgw-tls"},
							KeyRef:         v1alpha1.LocalObjectReference{Name: "rgw-tls"},
						},
					},
				},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}, StorageObjectGateways: []v1alpha1.StorageObjectGateway{gw}}
	paths := PathOptions{SecretsDir: "/ctx/secrets"}

	got := storageRGWIngressTLSVars(state, cluster, paths)
	if len(got) != 1 {
		t.Fatalf("storageRGWIngressTLSVars entries = %d, want 1 (only the TLS-configured ingress)", len(got))
	}
	item := got[0].(map[string]any)

	if want := "rgw.odf.ha"; item["serviceID"] != want {
		t.Fatalf("serviceID = %v, want %v", item["serviceID"], want)
	}
	if want := "rgw.odf"; item["backendService"] != want {
		t.Fatalf("backendService = %v, want %v", item["backendService"], want)
	}
	if want := []string{"node-1"}; len(item["hosts"].([]string)) != 1 || item["hosts"].([]string)[0] != want[0] {
		t.Fatalf("hosts = %v, want %v", item["hosts"], want)
	}
	if want := "10.0.0.9/24"; item["virtualIP"] != want {
		t.Fatalf("virtualIP = %v, want %v", item["virtualIP"], want)
	}
	if want := 8443; item["frontendPort"] != want {
		t.Fatalf("frontendPort = %v, want %v (from the gateway's public endpoint)", item["frontendPort"], want)
	}
	if want := 1967; item["monitorPort"] != want {
		t.Fatalf("monitorPort = %v, want %v", item["monitorPort"], want)
	}
	if want := 51; item["firstVirtualRouterID"] != want {
		t.Fatalf("firstVirtualRouterID = %v, want %v", item["firstVirtualRouterID"], want)
	}
	if want := []string{"10.0.0.0/24"}; len(item["virtualInterfaceNetworks"].([]string)) != 1 || item["virtualInterfaceNetworks"].([]string)[0] != want[0] {
		t.Fatalf("virtualInterfaceNetworks = %v, want %v", item["virtualInterfaceNetworks"], want)
	}
	if want := filepath.Join("/ctx/secrets", "rgw-tls"); item["certificatePath"] != want {
		t.Fatalf("certificatePath = %v, want %v", item["certificatePath"], want)
	}
}

func TestStorageRGWIngressTLSVarsEmptyWhenNoneConfigured(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	if got := storageRGWIngressTLSVars(state, cluster, PathOptions{}); len(got) != 0 {
		t.Fatalf("storageRGWIngressTLSVars with no gateways = %v, want empty", got)
	}
}
