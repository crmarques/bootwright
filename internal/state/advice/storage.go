// Package advice produces non-blocking, best-practice advisories over a
// validated desired State. Advisories never affect the load -> normalize ->
// validate -> render -> apply outcome; they are operator-facing guidance the
// CLI surfaces as warnings. This is a read-only analysis service that returns
// plain data, separate from the loader/validator that owns the State contract.
package advice

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// StorageAdvisory is a non-fatal best-practice finding about a managed Ceph
// StorageCluster. Advisories never block validate, render, or apply — a
// single-node or lab cluster is a legitimate authored shape — but
// `bootwright validate` surfaces them as warnings so a cluster that departs
// from IBM Storage Ceph / upstream production recommendations (sub-quorum
// monitors, a single manager, an unpinned subscription image) is visible at
// author time rather than discovered in production.
type StorageAdvisory struct {
	Object      string `json:"object"`
	Finding     string `json:"finding"`
	Impact      string `json:"impact"`
	Remediation string `json:"remediation"`
}

// StorageAdvisories returns the best-practice advisories for every managed Ceph
// StorageCluster in the state, in cluster order and, within a cluster, monitor
// then manager then image order. The result is deterministic so the CLI and its
// golden tests render a stable warning list. It reads only the Ceph spec, so it
// is safe to call on any validated state.
func StorageAdvisories(state v1alpha1.State) []StorageAdvisory {
	disconnected := environmentIsDisconnected(state)
	var out []StorageAdvisory
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || v1alpha1.StorageClusterExternal(cluster) {
			continue
		}
		object := fmt.Sprintf("%s/%s", v1alpha1.KindStorageCluster, cluster.Metadata.Name)
		out = append(out, storageMonitorAdvisories(object, cluster)...)
		out = append(out, storageManagerAdvisories(object, cluster)...)
		out = append(out, storageImageAdvisories(object, cluster)...)
		out = append(out, storageSidecarImageAdvisories(object, cluster, disconnected)...)
	}
	return out
}

// environmentIsDisconnected reports whether the Environment routes images
// through a mirror or digest-source remap — the disconnected posture in which
// cephadm's upstream sidecar image defaults are unreachable. It reads the
// Environment off the state directly to keep advice free of cross-package deps.
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

// storageSidecarImageAdvisories flags the airgap foot-gun: cephadm pulls the
// monitoring and ingress sidecars (prometheus, grafana, alertmanager,
// node-exporter, haproxy, keepalived) from compiled-in upstream defaults that a
// disconnected estate — or an IBM cluster entitled only to cp.icr.io — cannot
// reach, even though spec.ceph.image only pins the Ceph daemon image. Pinning
// these is the difference between a working and a non-working airgapped install.
// Suppressed once the operator pins any sidecar image under config[mgr], and for
// connected non-IBM clusters whose upstream defaults resolve.
func storageSidecarImageAdvisories(object string, cluster v1alpha1.StorageCluster, disconnected bool) []StorageAdvisory {
	ceph := cluster.Spec.Ceph
	monitoring := ceph.Monitoring
	monitoringEnabled := monitoring == nil || monitoring.Enabled == nil || *monitoring.Enabled
	if !monitoringEnabled {
		return nil
	}
	// IBM's compiled-in cephadm defaults point at registry.redhat.io, which an
	// IBM entitlement (cp.icr.io only) cannot pull, so it needs the pins even
	// when otherwise connected.
	vendorMismatch := ceph.Distribution == v1alpha1.StorageCephDistributionIBM
	if !disconnected && !vendorMismatch {
		return nil
	}
	for key := range ceph.Config["mgr"] {
		if strings.HasPrefix(key, "mgr/cephadm/container_image_") && key != "mgr/cephadm/container_image_base" {
			return nil
		}
	}
	return []StorageAdvisory{{
		Object:      object,
		Finding:     "monitoring/ingress sidecar images are not pinned to the entitled registry or mirror",
		Impact:      "cephadm pulls prometheus/grafana/alertmanager/node-exporter/haproxy/keepalived from upstream defaults this cluster cannot reach; the monitoring stack and every HA VIP fail to deploy",
		Remediation: "pin mgr/cephadm/container_image_{prometheus,grafana,alertmanager,node_exporter,haproxy,keepalived} under spec.ceph.config[mgr] to the mirror/entitled registry references",
	}}
}

// storageMonitorAdvisories flags a monitor count that cannot form, or barely
// forms, quorum. Stretch clusters are exempt: their monitor topology (two data
// sites plus a tiebreaker) is governed by the dedicated stretch validation, and
// the plain count heuristic would misread their intentional shape.
func storageMonitorAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if cluster.Spec.Ceph.Topology.Stretch != nil {
		return nil
	}
	mons := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON))
	switch {
	case mons < 3:
		return []StorageAdvisory{{
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mon role", mons),
			Impact:      "IBM Storage Ceph recommends at least 3 monitors for quorum; fewer cannot survive a single monitor failure without losing the cluster",
			Remediation: "give the mon role to at least 3 hosts (an odd count); single-monitor clusters are for lab or single-node use only",
		}}
	case mons%2 == 0:
		return []StorageAdvisory{{
			Object:      object,
			Finding:     fmt.Sprintf("%d hosts carry the mon role (an even count)", mons),
			Impact:      "an even monitor count tolerates no more failures than the next-lower odd count, so the extra monitor adds cost without availability",
			Remediation: "use an odd number of mon hosts (3, 5, 7)",
		}}
	}
	return nil
}

// storageManagerAdvisories flags a manager count with no standby.
func storageManagerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	mgrs := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR))
	if mgrs < 2 {
		return []StorageAdvisory{{
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mgr role", mgrs),
			Impact:      "IBM Storage Ceph recommends at least 2 managers; a single manager is a single point of failure for orchestration, the dashboard, and metrics",
			Remediation: "give the mgr role to at least 2 hosts for an active/standby pair",
		}}
	}
	return nil
}

// storageImageAdvisories flags a subscription-backed distribution that pins no
// container image. The distribution-packaged cephadm supplies a default image
// tag in that case, but it floats, so the running Ceph version is not
// reproducible across re-installs — and validation already forbids an explicit
// mutable :latest, so leaving the image unset is the same anti-pattern by
// omission.
func storageImageAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	distribution := cluster.Spec.Ceph.Distribution
	if distribution != v1alpha1.StorageCephDistributionIBM && distribution != v1alpha1.StorageCephDistributionRedHat {
		return nil
	}
	if cluster.Spec.Ceph.Image != "" {
		return nil
	}
	return []StorageAdvisory{{
		Object:      object,
		Finding:     fmt.Sprintf("distribution %q pins no spec.ceph.image", distribution),
		Impact:      "the install uses the distribution-packaged cephadm's default image tag, which floats; the running Ceph version is not reproducible across re-installs",
		Remediation: "set spec.ceph.image to a digest-pinned reference (for example cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:...)",
	}}
}
