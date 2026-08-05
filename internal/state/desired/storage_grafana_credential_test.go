package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func grafanaCredentialCluster(field, ref string) v1alpha1.StorageCluster {
	service := &v1alpha1.StorageCephMonitoringService{
		InitialAdminPasswordRef: v1alpha1.LocalObjectReference{Name: ref},
	}
	monitoring := &v1alpha1.StorageCephMonitoring{}
	switch field {
	case "grafana":
		monitoring.Grafana = service
	case "prometheus":
		monitoring.Prometheus = service
	}
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Monitoring: monitoring,
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "node-01"}, Roles: []string{v1alpha1.StorageCephRoleGrafana, v1alpha1.StorageCephRolePrometheus}},
			}},
		}},
	}
}

func TestGrafanaInitialAdminPasswordRefRules(t *testing.T) {
	state := v1alpha1.State{Secrets: []v1alpha1.Secret{
		{Metadata: v1alpha1.Metadata{Name: "grafana-admin"}},
	}}
	for _, tc := range []struct {
		name  string
		field string
		ref   string
		want  string
	}{
		{name: "grafana-with-declared-secret", field: "grafana", ref: "grafana-admin"},
		{name: "grafana-with-unknown-secret", field: "grafana", ref: "absent", want: `initialAdminPasswordRef "absent" is not a declared Secret`},
		{name: "prometheus-is-refused", field: "prometheus", ref: "grafana-admin", want: "initialAdminPasswordRef applies to grafana only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := grafanaCredentialCluster(tc.field, tc.ref)
			got := strings.Join(validateStorageCephMonitoring("spec.ceph.monitoring", cluster, state), "; ")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("unexpected errors: %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}
