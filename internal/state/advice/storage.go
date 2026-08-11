package advice

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type AdvisorySeverity string

const (
	SeverityWarn AdvisorySeverity = "WARN"
	SeverityInfo AdvisorySeverity = "INFO"
)

const (
	cephBestPracticeGroup  = "Ceph best practice"
	rootFilesystemGroup    = "Ceph root filesystem"
	stretchPoolGroup       = "Stretch pools"
	stretchTiebreakerGroup = "Stretch tiebreaker"
	estateSiteGroup        = "Estate sites"
)

var (
	monitoringSidecarImageOptions = []string{
		"mgr/cephadm/container_image_prometheus",
		"mgr/cephadm/container_image_grafana",
		"mgr/cephadm/container_image_alertmanager",
		"mgr/cephadm/container_image_node_exporter",
	}
	ingressSidecarImageOptions = []string{
		"mgr/cephadm/container_image_haproxy",
		"mgr/cephadm/container_image_keepalived",
	}
	managementGatewaySidecarImageOptions = []string{
		"mgr/cephadm/container_image_nginx",
		"mgr/cephadm/container_image_keepalived",
	}
)

type StorageAdvisory struct {
	Severity    AdvisorySeverity `json:"severity"`
	Group       string           `json:"group"`
	Object      string           `json:"object"`
	Finding     string           `json:"finding"`
	Impact      string           `json:"impact,omitempty"`
	Remediation string           `json:"remediation,omitempty"`
}

func StorageAdvisories(state v1alpha1.State) []StorageAdvisory {
	disconnected := environmentIsDisconnected(state)
	out := unreferencedSiteAdvisories(state)
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || v1alpha1.StorageClusterExternal(cluster) {
			continue
		}
		object := fmt.Sprintf("%s/%s", v1alpha1.KindStorageCluster, cluster.Metadata.Name)
		out = append(out, storageMonitorAdvisories(object, cluster)...)
		out = append(out, storageManagerAdvisories(object, cluster)...)
		out = append(out, storageGrafanaCredentialAdvisories(object, cluster)...)
		out = append(out, storageImageAdvisories(object, cluster)...)
		out = append(out, storageSidecarImageAdvisories(object, state, cluster, disconnected)...)
		out = append(out, storageRootFilesystemAdvisories(object, state, cluster)...)
		out = append(out, storageStretchPoolAdvisories(object, state, cluster)...)
		out = append(out, storageStretchTiebreakerAdvisories(object, cluster)...)
		out = append(out, storageStretchDataSiteTiebreakerAdvisories(object, cluster)...)
	}
	return out
}

func storageRootFilesystemAdvisories(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	var out []StorageAdvisory
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		binding, ok := topology.ResolveNodeMachineProfile(state, node)
		if !ok {
			continue
		}
		diskGiB := binding.EffectiveDiskGiB
		budgetGiB := topology.NodeRootFilesystemGiB(cluster, node)
		if budgetGiB <= topology.RootFilesystemFloorGiB || diskGiB < topology.RootFilesystemFloorGiB || diskGiB >= budgetGiB {
			continue
		}
		out = append(out, StorageAdvisory{
			Severity: SeverityWarn,
			Group:    rootFilesystemGroup,
			Object:   object,
			Finding: fmt.Sprintf(
				"ceph node %q (Machine/%s) resolves InfraProvider/%s profile %q diskGiB to %d GiB, below its computed %d GiB root-filesystem service budget",
				node.Name, binding.Machine.Metadata.Name, binding.Provider.Metadata.Name, binding.Profile.Name, diskGiB, budgetGiB,
			),
			Impact: "the raw disk clears the absolute desired-state floor, but the live preflight measures free space after the OS is installed and will warn below this node's role and monitoring budget; Ceph data, images, and service state can exhaust the root filesystem",
			Remediation: fmt.Sprintf(
				"before the first apply, raise InfraProvider/%s spec.%s.machineProfiles profile %q diskGiB to at least %d; raw disk size cannot guarantee the same amount of free space, so leave additional room for the OS",
				binding.Provider.Metadata.Name, binding.Provider.Spec.Type, binding.Profile.Name, budgetGiB,
			),
		})
	}
	return out
}

func unreferencedSiteAdvisories(state v1alpha1.State) []StorageAdvisory {
	if len(state.Environments) == 0 {
		return nil
	}
	env := state.Environments[0]
	used := map[string]bool{}
	for _, machine := range state.Machines {
		if site := v1alpha1.MachineSite(machine); site != "" {
			used[site] = true
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Site != "" {
				used[node.Site] = true
			}
		}
	}
	var out []StorageAdvisory
	for _, site := range v1alpha1.EnvironmentSiteNames(env) {
		if used[site] {
			continue
		}
		out = append(out, StorageAdvisory{
			Severity:    SeverityInfo,
			Group:       estateSiteGroup,
			Object:      fmt.Sprintf("%s/%s", v1alpha1.KindEnvironment, env.Metadata.Name),
			Finding:     fmt.Sprintf("site %q is declared but no Machine stands in it", site),
			Impact:      "a site no machine references places nothing; if it was meant to hold a standby arbiter, the candidate machine is missing, and replace-arbiter will have nowhere to move the tiebreaker",
			Remediation: fmt.Sprintf("declare a Machine with spec.placement.site %q, or drop the site from spec.sites", site),
		})
	}
	return out
}

func storageStretchDataSiteTiebreakerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || stretch.Tiebreaker.Node == "" || stretch.Tiebreaker.Site == "" {
		return nil
	}
	for _, site := range stretch.DataSites {
		if site != stretch.Tiebreaker.Site {
			continue
		}
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       stretchTiebreakerGroup,
			Object:      object,
			Finding:     fmt.Sprintf("the tiebreaker mon stands in data site %q rather than a third site", site),
			Impact:      "losing that data site takes its two replicas and the tiebreaker vote at once, so the surviving site cannot win the monitor election and the cluster stops serving; a cluster not already in stretch mode also cannot enter it in this shape, because Ceph refuses enable_stretch_mode when the tiebreaker shares a data site",
			Remediation: fmt.Sprintf("this is the acknowledged emergency shape after `--authorize same-site-arbiter`; once a third site is available, choose a declared arbiter-capable Machine outside %s and run the storage-cluster replacement workflow for StorageCluster/%s with that exact machine", strings.Join(stretch.DataSites, " and "), strings.TrimPrefix(object, v1alpha1.KindStorageCluster+"/")),
		}}
	}
	return nil
}

func storageStretchTiebreakerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || stretch.Tiebreaker.Node != "" {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       stretchTiebreakerGroup,
		Object:      object,
		Finding:     "stretch mode is declared with no tiebreaker/arbiter mon",
		Impact:      "a two-site stretch cluster without a tiebreaker mon in a third site loses monitor quorum if either data site fails, and apply cannot enable stretch mode, so every pool places two replicas per site (apply reconciles the Ceph-internal pools onto the same rule in place of the stretch-mode re-homing) but without automatic degraded-mode failover",
		Remediation: "add a mon-only arbiter node in a third site and set spec.ceph.topology.stretch.tiebreaker.node before relying on this cluster",
	}}
}

func environmentIsDisconnected(state v1alpha1.State) bool {
	if len(state.Environments) == 0 {
		return false
	}
	registries := state.Environments[0].Spec.Registries
	if registries == nil {
		return false
	}
	return registries.Mirror != nil || len(registries.ImageDigestSources) > 0
}

func storageSidecarImageAdvisories(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster, disconnected bool) []StorageAdvisory {
	ceph := cluster.Spec.Ceph
	vendorMismatch := ceph.Distribution == v1alpha1.StorageCephDistributionIBM
	if !disconnected && !vendorMismatch {
		return nil
	}
	required := storageSidecarImageOptions(state, cluster)
	if len(required) == 0 {
		return nil
	}
	pins := v1alpha1.StorageCephSidecarImagePins(ceph)
	var missing []string
	for _, option := range required {
		if strings.TrimSpace(pins[option]) == "" {
			missing = append(missing, option)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     "declared monitoring, ingress, or management-gateway sidecar images are not pinned: " + strings.Join(missing, ", "),
		Impact:      "cephadm pulls these declared sidecars from compiled-in upstream defaults that this disconnected or IBM cluster may not reach, preventing their monitoring, ingress, or management-gateway services from deploying",
		Remediation: "set each missing mgr/cephadm/container_image_* option under spec.ceph.config[mgr] to a pullable mirror or entitled registry reference: " + strings.Join(missing, ", "),
	}}
}

func storageSidecarImageOptions(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	var required []string
	seen := map[string]bool{}
	add := func(options ...string) {
		for _, option := range options {
			if seen[option] {
				continue
			}
			seen[option] = true
			required = append(required, option)
		}
	}
	if topology.MonitoringEnabled(cluster) {
		add(monitoringSidecarImageOptions...)
	}
	if storageClusterHasGatewayOrExportIngress(state, cluster.Metadata.Name) {
		add(ingressSidecarImageOptions...)
	}
	mgmtGateway := cluster.Spec.Ceph.MgmtGateway
	if mgmtGateway == nil {
		return required
	}
	add(managementGatewaySidecarImageOptions...)
	if mgmtGateway.OAuth2Proxy != nil {
		add("mgr/cephadm/container_image_oauth2_proxy")
	}
	return required
}

func storageClusterHasGatewayOrExportIngress(state v1alpha1.State, clusterName string) bool {
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Spec.StorageClusterRef.Name == clusterName && len(gateway.Spec.Ceph.Ingresses) > 0 {
			return true
		}
	}
	for _, export := range state.StorageNFSExports {
		if export.Spec.StorageClusterRef.Name == clusterName && len(export.Spec.Ceph.Ingresses) > 0 {
			return true
		}
	}
	return false
}

func storageMonitorAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if cluster.Spec.Ceph.Topology.Stretch != nil {
		return nil
	}
	mons := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON))
	switch {
	case mons < 3:
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mon role", mons),
			Impact:      "Ceph recommends at least 3 monitors for quorum; fewer cannot survive a single monitor failure without losing the cluster",
			Remediation: "give the mon role to at least 3 hosts (an odd count); single-monitor clusters are for lab or single-node use only",
		}}
	case mons%2 == 0:
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d hosts carry the mon role (an even count)", mons),
			Impact:      "an even monitor count tolerates no more failures than the next-lower odd count, so the extra monitor adds cost without availability",
			Remediation: "use an odd number of mon hosts (3, 5, 7)",
		}}
	}
	return nil
}

func storageManagerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	mgrs := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR))
	if mgrs < 2 {
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mgr role", mgrs),
			Impact:      "Ceph recommends at least 2 managers; a single manager is a single point of failure for orchestration, the dashboard, and metrics",
			Remediation: "give the mgr role to at least 2 hosts for an active/standby pair",
		}}
	}
	return nil
}

func storageGrafanaCredentialAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if v1alpha1.StorageCephGrafanaHasCredential(cluster) {
		return nil
	}
	if len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleGrafana)) == 0 {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityInfo,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     "grafana is deployed with no spec.ceph.monitoring.grafana.initialAdminPasswordRef",
		Impact:      "cephadm renders disable_initial_admin_creation into grafana.ini when the spec carries no initial_admin_password, so Grafana creates no administrator at all: its login page appears through the management gateway and refuses every credential, including the dashboard's. The dashboard's embedded panels keep working, because cephadm grants them anonymous viewer access",
		Remediation: "declare spec.ceph.monitoring.grafana.initialAdminPasswordRef naming a Secret; the apply seeds the account and recreates the Grafana daemons so grafana.ini carries it",
	}}
}

func storageImageAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	distribution := cluster.Spec.Ceph.Distribution
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return nil
	}
	if v1alpha1.StorageCephImagePinned(cluster.Spec.Ceph) {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     fmt.Sprintf("distribution %q pins no spec.ceph.image.version", distribution),
		Impact:      "the install uses the distribution-packaged cephadm's default image tag, which floats; the running Ceph version is not reproducible across re-installs",
		Remediation: "set spec.ceph.image.version to the build you run, as a tag or a sha256: digest, while keeping spec.ceph.image.base as the declared image repository",
	}}
}

func storageStretchPoolAdvisories(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if cluster.Spec.Ceph.Topology.Stretch == nil {
		return nil
	}
	var pools []string
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name == cluster.Metadata.Name && pool.Spec.PlacementPolicyRef.Name == "" {
			pools = append(pools, pool.Metadata.Name)
		}
	}
	if len(pools) == 0 {
		return nil
	}
	return []StorageAdvisory{{
		Severity: SeverityInfo,
		Group:    stretchPoolGroup,
		Object:   object,
		Finding: fmt.Sprintf(
			"policy-less pools inherit the stretch rule and size %d/minSize %d: %s",
			topology.StretchReplicatedPoolSize, topology.StretchReplicatedPoolMinSize, strings.Join(pools, ", "),
		),
	}}
}
