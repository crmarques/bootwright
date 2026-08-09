package topology

import (
	"math"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	RootFilesystemFloorGiB = 20
	KubeVirtDefaultDiskGiB = 120

	rootFilesystemBaseGiB           = 20
	rootFilesystemMONGiB            = 15
	rootFilesystemMGRGiB            = 5
	rootFilesystemGrafanaGiB        = 2
	rootFilesystemAlertmanagerGiB   = 1
	rootFilesystemLokiGiB           = 20
	rootFilesystemPrometheusGiB     = 20
	rootFilesystemPrometheusPadGiB  = 4
	PrometheusDefaultRetentionSize  = "10GB"
	prometheusRetentionSizeFallback = rootFilesystemPrometheusGiB
)

type NodeMachineProfile struct {
	Machine          v1alpha1.Machine
	Provider         v1alpha1.InfraProvider
	Profile          v1alpha1.MachineProfile
	EffectiveDiskGiB int
}

func ResolveNodeMachineProfile(state v1alpha1.State, node v1alpha1.StorageCephNode) (NodeMachineProfile, bool) {
	machine, ok := stateview.Machine(state, node.MachineRef.Name)
	if !ok {
		return NodeMachineProfile{}, false
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok || provider.Spec.Type == v1alpha1.ProvisionerBareMetal {
		return NodeMachineProfile{}, false
	}
	profile, ok := stateview.MachineProfile(provider, v1alpha1.MachineProfileRef(machine).Name)
	if !ok {
		return NodeMachineProfile{}, false
	}
	diskGiB := profile.DiskGiB
	if provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && diskGiB == 0 {
		diskGiB = KubeVirtDefaultDiskGiB
	}
	return NodeMachineProfile{Machine: machine, Provider: provider, Profile: profile, EffectiveDiskGiB: diskGiB}, true
}

func NodeRootFilesystemGiB(cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephNode) int {
	total := rootFilesystemBaseGiB
	if NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
		total += rootFilesystemMONGiB
	}
	if NodeHasRole(node, v1alpha1.StorageCephRoleMGR) {
		total += rootFilesystemMGRGiB
	}
	if !MonitoringEnabled(cluster) {
		return total
	}
	placed := monitoringHostServices(cluster, node.Name)
	if placed["prometheus"] {
		total += prometheusBudgetGiB(cluster)
	}
	if placed["grafana"] {
		total += rootFilesystemGrafanaGiB
	}
	if placed["alertmanager"] {
		total += rootFilesystemAlertmanagerGiB
	}
	if placed["loki"] {
		total += rootFilesystemLokiGiB
	}
	return total
}

func MonitoringEnabled(cluster v1alpha1.StorageCluster) bool {
	monitoring := cluster.Spec.Ceph.Monitoring
	return monitoring == nil || monitoring.Enabled == nil || *monitoring.Enabled
}

func PrometheusRetentionSize(cluster v1alpha1.StorageCluster) string {
	if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil && monitoring.Prometheus != nil {
		if monitoring.Prometheus.RetentionSize != "" {
			return monitoring.Prometheus.RetentionSize
		}
	}
	return PrometheusDefaultRetentionSize
}

func monitoringHostServices(cluster v1alpha1.StorageCluster, host string) map[string]bool {
	out := map[string]bool{}
	for _, service := range monitoringDataServices(cluster) {
		if service.config == nil && service.role == "" {
			continue
		}
		placement := v1alpha1.StoragePlacement{}
		if service.config != nil {
			placement = service.config.Placement
		}
		for _, candidate := range ResolvePlacement(cluster, placement, service.role) {
			if candidate == host {
				out[service.serviceType] = true
				break
			}
		}
	}
	return out
}

type monitoringDataService struct {
	serviceType string
	role        string
	config      *v1alpha1.StorageCephMonitoringService
}

func monitoringDataServices(cluster v1alpha1.StorageCluster) []monitoringDataService {
	var prometheus, grafana, alertmanager, loki *v1alpha1.StorageCephMonitoringService
	if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil {
		prometheus = monitoring.Prometheus
		grafana = monitoring.Grafana
		alertmanager = monitoring.Alertmanager
		loki = monitoring.Loki
	}
	return []monitoringDataService{
		{"prometheus", v1alpha1.StorageCephRolePrometheus, prometheus},
		{"grafana", v1alpha1.StorageCephRoleGrafana, grafana},
		{"alertmanager", v1alpha1.StorageCephRoleAlertmanager, alertmanager},
		{"loki", "", loki},
	}
}

func prometheusBudgetGiB(cluster v1alpha1.StorageCluster) int {
	size := PrometheusRetentionSize(cluster)
	gib, ok := parseRetentionSizeGiB(size)
	if !ok {
		return prometheusRetentionSizeFallback
	}
	return gib + rootFilesystemPrometheusPadGiB
}

func parseRetentionSizeGiB(value string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	digits := 0
	for digits < len(trimmed) && (trimmed[digits] >= '0' && trimmed[digits] <= '9' || trimmed[digits] == '.') {
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	amount, err := strconv.ParseFloat(trimmed[:digits], 64)
	if err != nil || amount <= 0 {
		return 0, false
	}
	unit := strings.ToUpper(strings.TrimSpace(trimmed[digits:]))
	multiplier, ok := retentionSizeMultipliers()[unit]
	if !ok {
		return 0, false
	}
	gib := amount * multiplier / (1024 * 1024 * 1024)
	if gib <= 0 {
		return 0, false
	}
	return int(math.Ceil(gib)), true
}

func retentionSizeMultipliers() map[string]float64 {
	return map[string]float64{
		"B":   1,
		"KB":  1000,
		"MB":  1000 * 1000,
		"GB":  1000 * 1000 * 1000,
		"TB":  1000 * 1000 * 1000 * 1000,
		"PB":  1000 * 1000 * 1000 * 1000 * 1000,
		"KIB": 1024,
		"MIB": 1024 * 1024,
		"GIB": 1024 * 1024 * 1024,
		"TIB": 1024 * 1024 * 1024 * 1024,
		"PIB": 1024 * 1024 * 1024 * 1024 * 1024,
	}
}
