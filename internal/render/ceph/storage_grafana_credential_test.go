package ceph_test

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	ceph "github.com/crmarques/bootwright/internal/render/ceph"
)

func grafanaCluster(passwordRef string) v1alpha1.StorageCluster {
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}}
	grafana := &v1alpha1.StorageCephMonitoringService{Port: 3000}
	if passwordRef != "" {
		grafana.InitialAdminPasswordRef = v1alpha1.LocalObjectReference{Name: passwordRef}
	}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Monitoring: &v1alpha1.StorageCephMonitoring{Grafana: grafana},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "node-01"}, Roles: []string{v1alpha1.StorageCephRoleGrafana, v1alpha1.StorageCephRolePrometheus}},
		}},
	}
	return cluster
}

func TestCephadmGrafanaDocIsRenderedWithoutACredential(t *testing.T) {
	docs := docsFromSpecs(t, ceph.CephadmLateServicesSpec(v1alpha1.State{}, grafanaCluster("")))
	doc := serviceDoc(t, docs, "grafana", "")
	if doc == nil {
		t.Fatalf("a credential-less grafana keeps its Go-rendered document, got %v", docs)
	}
	if _, ok := doc["spec"].(map[string]any)["initial_admin_password"]; ok {
		t.Fatalf("the Go render must never carry secret material — it writes files that are not secret-scoped, got %v", doc)
	}
}

func TestCephadmGrafanaDocIsLeftToTheSecretBearingPhase(t *testing.T) {
	docs := docsFromSpecs(t, ceph.CephadmLateServicesSpec(v1alpha1.State{}, grafanaCluster("grafana-admin")))
	for _, doc := range docs {
		if doc["service_type"] == "grafana" {
			t.Fatalf("a grafana carrying initialAdminPasswordRef must be assembled by the management phase from the secret file, not rendered here — two documents for one service would race, and the last apply would win by dropping the credential: %v", doc)
		}
	}
}
