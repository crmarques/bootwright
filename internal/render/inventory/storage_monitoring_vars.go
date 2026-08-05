package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func storageGrafanaVars(cluster v1alpha1.StorageCluster, paths PathOptions) map[string]any {
	if !v1alpha1.StorageCephGrafanaHasCredential(cluster) {
		return nil
	}
	grafana := cluster.Spec.Ceph.Monitoring.Grafana
	hosts := topology.ResolvePlacement(cluster, grafana.Placement, v1alpha1.StorageCephRoleGrafana)
	if len(hosts) == 0 {
		return nil
	}
	out := map[string]any{
		"hosts":                hosts,
		"initialAdminPassPath": secret.ResolvePath(grafana.InitialAdminPasswordRef.Name, paths.SecretIndex, paths.SecretsDir),
	}
	if grafana.Port > 0 {
		out["port"] = grafana.Port
	}
	if len(grafana.Networks) > 0 {
		out["networks"] = append([]string(nil), grafana.Networks...)
	}
	if grafana.Placement.CountPerHost > 0 {
		out["countPerHost"] = grafana.Placement.CountPerHost
	}
	return out
}
