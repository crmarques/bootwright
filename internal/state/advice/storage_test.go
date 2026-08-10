package advice

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func adviceCephCluster(name, distribution, image string, roleSets ...[]string) v1alpha1.StorageCluster {
	hosts := make([]v1alpha1.StorageCephNode, 0, len(roleSets))
	for i, roles := range roleSets {
		hosts = append(hosts, v1alpha1.StorageCephNode{Name: fmt.Sprintf("h%d", i), Roles: roles})
	}
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: distribution,
				Image:        adviceImageSpec(image),
				Topology:     v1alpha1.StorageCephTopology{Nodes: hosts},
			},
		},
	}
}

func adviceImageSpec(version string) *v1alpha1.StorageCephImageSpec {
	if version == "" {
		return nil
	}
	return &v1alpha1.StorageCephImageSpec{Version: version}
}

func adviceState(clusters ...v1alpha1.StorageCluster) v1alpha1.State {
	return v1alpha1.State{StorageClusters: clusters}
}

func findingsWith(advisories []StorageAdvisory, substr string) []StorageAdvisory {
	var out []StorageAdvisory
	for _, a := range advisories {
		if strings.Contains(a.Finding, substr) || strings.Contains(a.Impact, substr) {
			out = append(out, a)
		}
	}
	return out
}

func TestStorageAdvisoriesCleanProductionClusterIsSilent(t *testing.T) {
	cluster := adviceCephCluster("prod", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "osd"},
	)
	if got := StorageAdvisories(adviceState(cluster)); len(got) != 0 {
		t.Fatalf("3-mon/2-mgr oss cluster must raise no advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagSubQuorumMonAndSingleMgr(t *testing.T) {
	cluster := adviceCephCluster("lab", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"},
	)
	got := StorageAdvisories(adviceState(cluster))
	if len(findingsWith(got, "mon role")) != 1 {
		t.Fatalf("single-mon cluster must raise one mon advisory, got %+v", got)
	}
	if len(findingsWith(got, "at least 3 monitors")) != 1 {
		t.Fatalf("single-mon advisory must cite the 3-monitor minimum, got %+v", got)
	}
	if len(findingsWith(got, "mgr role")) != 1 {
		t.Fatalf("single-mgr cluster must raise one mgr advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagEvenMonCount(t *testing.T) {
	cluster := adviceCephCluster("even", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr"},
		[]string{"mon", "mgr"},
		[]string{"mon"},
		[]string{"mon"},
	)
	got := StorageAdvisories(adviceState(cluster))
	if len(findingsWith(got, "even monitor count")) != 1 {
		t.Fatalf("4-mon cluster must raise the even-count advisory, got %+v", got)
	}
	if len(findingsWith(got, "mgr role")) != 0 {
		t.Fatalf("2-mgr cluster must not raise a mgr advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagUnpinnedSubscriptionImage(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionIBM, v1alpha1.StorageCephDistributionRedHat} {
		cluster := adviceCephCluster("sub", distribution, "",
			[]string{"mon", "mgr", "osd"},
			[]string{"mon", "mgr", "osd"},
			[]string{"mon", "osd"},
		)
		got := StorageAdvisories(adviceState(cluster))
		if len(findingsWith(got, "spec.ceph.image")) != 1 {
			t.Fatalf("%s without a pinned image must raise the image advisory, got %+v", distribution, got)
		}
	}
}

func TestStorageAdvisoriesPinnedImageAndOSSAreSilentOnImage(t *testing.T) {
	pinned := adviceCephCluster("ibm-pinned", v1alpha1.StorageCephDistributionIBM,
		"9.9.1.0-123",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	if got := findingsWith(StorageAdvisories(adviceState(pinned)), "spec.ceph.image"); len(got) != 0 {
		t.Fatalf("a pinned ibm image must not raise an image advisory, got %+v", got)
	}
	oss := adviceCephCluster("oss", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	if got := findingsWith(StorageAdvisories(adviceState(oss)), "spec.ceph.image"); len(got) != 0 {
		t.Fatalf("oss derives its image and must not raise an image advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesNeverNagAboutThePackageBuild(t *testing.T) {
	cluster := adviceCephCluster("ibm", v1alpha1.StorageCephDistributionIBM, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	cluster.Spec.Ceph.PackageVersion = ""
	for _, a := range StorageAdvisories(adviceState(cluster)) {
		if strings.Contains(a.Finding+a.Impact+a.Remediation, "packageVersion") {
			t.Fatalf("no advisory may nag about an unpinned cephadm build; that is release-matrix territory Bootwright does not enter: %+v", a)
		}
	}
}

func TestStorageAdvisoriesFlagUnpinnedSidecarImages(t *testing.T) {
	healthy := func(name, distribution string) v1alpha1.StorageCluster {
		return adviceCephCluster(name, distribution, "",
			[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	}
	option := func(name string) string {
		return "mgr/cephadm/container_image_" + name
	}
	requireSidecarOptions := func(state v1alpha1.State, want, absent []string) {
		t.Helper()
		got := findingsWith(StorageAdvisories(state), "sidecar images")
		if len(got) != 1 {
			t.Fatalf("sidecar advisory = %+v, want one", got)
		}
		for _, name := range want {
			if !strings.Contains(got[0].Finding, option(name)) || !strings.Contains(got[0].Remediation, option(name)) {
				t.Fatalf("sidecar advisory must report and remedy %q, got %+v", option(name), got[0])
			}
		}
		for _, name := range absent {
			if strings.Contains(got[0].Finding, option(name)) || strings.Contains(got[0].Remediation, option(name)) {
				t.Fatalf("sidecar advisory must not report already-pinned or undeclared %q, got %+v", option(name), got[0])
			}
		}
	}
	disconnectedEnv := v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{
		Registries: &v1alpha1.EnvironmentRegistriesSpec{Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{URL: "mirror.test:5000"}},
	}}

	ibm := adviceState(healthy("ibm", v1alpha1.StorageCephDistributionIBM))
	requireSidecarOptions(ibm, []string{"prometheus", "grafana", "alertmanager", "node_exporter"}, []string{"haproxy", "keepalived", "nginx", "oauth2_proxy"})

	ossConnected := adviceState(healthy("oss", v1alpha1.StorageCephDistributionOSS))
	if got := findingsWith(StorageAdvisories(ossConnected), "sidecar"); len(got) != 0 {
		t.Fatalf("connected oss cluster must not flag sidecars, got %+v", got)
	}
	ossDisconnected := adviceState(healthy("oss", v1alpha1.StorageCephDistributionOSS))
	ossDisconnected.Environments = []v1alpha1.Environment{disconnectedEnv}
	requireSidecarOptions(ossDisconnected, []string{"prometheus", "grafana", "alertmanager", "node_exporter"}, nil)

	pinned := healthy("ibm-pinned", v1alpha1.StorageCephDistributionIBM)
	pinned.Spec.Ceph.Config = map[string]map[string]string{"mgr": {
		option("prometheus"): "mirror.test:5000/prometheus:v2",
		option("grafana"):    " ",
	}}
	requireSidecarOptions(adviceState(pinned), []string{"grafana", "alertmanager", "node_exporter"}, []string{"prometheus"})

	noMon := healthy("ibm-nomon", v1alpha1.StorageCephDistributionIBM)
	disabled := false
	noMon.Spec.Ceph.Monitoring = &v1alpha1.StorageCephMonitoring{Enabled: &disabled}
	if got := findingsWith(StorageAdvisories(adviceState(noMon)), "sidecar"); len(got) != 0 {
		t.Fatalf("monitoring-disabled cluster must not flag sidecars, got %+v", got)
	}
	unboundIngress := adviceState(noMon)
	unboundIngress.StorageObjectGateways = []v1alpha1.StorageObjectGateway{{
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "other"},
			Ceph:              v1alpha1.StorageObjectGatewayCephSpec{Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "vip"}}},
		},
	}}
	if got := findingsWith(StorageAdvisories(unboundIngress), "sidecar"); len(got) != 0 {
		t.Fatalf("an ingress bound to another cluster must not require this cluster's sidecars, got %+v", got)
	}

	ingress := healthy("ibm-ingress", v1alpha1.StorageCephDistributionIBM)
	ingress.Spec.Ceph.Monitoring = &v1alpha1.StorageCephMonitoring{Enabled: &disabled}
	ingressState := adviceState(ingress)
	ingressState.StorageObjectGateways = []v1alpha1.StorageObjectGateway{{
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: ingress.Metadata.Name},
			Ceph:              v1alpha1.StorageObjectGatewayCephSpec{Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "vip"}}},
		},
	}}
	requireSidecarOptions(ingressState, []string{"haproxy", "keepalived"}, []string{"prometheus", "grafana", "alertmanager", "node_exporter", "nginx", "oauth2_proxy"})

	exportIngress := healthy("ibm-export-ingress", v1alpha1.StorageCephDistributionIBM)
	exportIngress.Spec.Ceph.Monitoring = &v1alpha1.StorageCephMonitoring{Enabled: &disabled}
	exportState := adviceState(exportIngress)
	exportState.StorageNFSExports = []v1alpha1.StorageNFSExport{{
		Spec: v1alpha1.StorageNFSExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: exportIngress.Metadata.Name},
			Ceph:              v1alpha1.StorageNFSExportCephSpec{Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "vip"}}},
		},
	}}
	requireSidecarOptions(exportState, []string{"haproxy", "keepalived"}, []string{"prometheus", "grafana", "alertmanager", "node_exporter", "nginx", "oauth2_proxy"})

	management := healthy("ibm-management", v1alpha1.StorageCephDistributionIBM)
	management.Spec.Ceph.Monitoring = &v1alpha1.StorageCephMonitoring{Enabled: &disabled}
	management.Spec.Ceph.MgmtGateway = &v1alpha1.StorageCephMgmtGateway{OAuth2Proxy: &v1alpha1.StorageCephOAuth2Proxy{}}
	requireSidecarOptions(adviceState(management), []string{"nginx", "keepalived", "oauth2_proxy"}, []string{"prometheus", "grafana", "alertmanager", "node_exporter", "haproxy"})

	combined := management
	combinedState := adviceState(combined)
	combinedState.StorageObjectGateways = []v1alpha1.StorageObjectGateway{{
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: combined.Metadata.Name},
			Ceph:              v1alpha1.StorageObjectGatewayCephSpec{Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "vip"}}},
		},
	}}
	combined.Spec.Ceph.Config = map[string]map[string]string{"mgr": {
		option("haproxy"):      "mirror.test:5000/haproxy:v2",
		option("nginx"):        "mirror.test:5000/nginx:v1",
		option("oauth2_proxy"): "mirror.test:5000/oauth2-proxy:v7",
	}}
	combinedState.StorageClusters = []v1alpha1.StorageCluster{combined}
	requireSidecarOptions(combinedState, []string{"keepalived"}, []string{"haproxy", "nginx", "oauth2_proxy"})
	got := findingsWith(StorageAdvisories(combinedState), "sidecar images")
	if strings.Count(got[0].Finding, option("keepalived")) != 1 || strings.Count(got[0].Remediation, option("keepalived")) != 1 {
		t.Fatalf("shared ingress/management keepalived requirement must be reported once, got %+v", got[0])
	}
}

func TestStorageAdvisoriesExemptStretchFromMonCount(t *testing.T) {
	cluster := adviceCephCluster("stretch", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr"}, []string{"mon", "mgr"},
		[]string{"mon"}, []string{"mon"},
	)
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	if got := findingsWith(StorageAdvisories(adviceState(cluster)), "mon role"); len(got) != 0 {
		t.Fatalf("stretch clusters are governed by stretch validation and must be exempt from mon-count advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesWarnStretchWithoutTiebreaker(t *testing.T) {
	cluster := adviceCephCluster("stretch", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"},
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"},
	)
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	got := findingsWith(StorageAdvisories(adviceState(cluster)), "no tiebreaker/arbiter mon")
	if len(got) != 1 {
		t.Fatalf("an arbiter-less stretch cluster must raise one tiebreaker advisory, got %+v", StorageAdvisories(adviceState(cluster)))
	}
	if got[0].Severity != SeverityWarn {
		t.Fatalf("the tiebreaker advisory must be WARN, got %q", got[0].Severity)
	}
	cluster.Spec.Ceph.Topology.Stretch.Tiebreaker = v1alpha1.StorageCephTiebreaker{Site: "dc3", Node: "arbiter"}
	if got := findingsWith(StorageAdvisories(adviceState(cluster)), "no tiebreaker/arbiter mon"); len(got) != 0 {
		t.Fatalf("a stretch cluster with a tiebreaker must raise no tiebreaker advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesNoticeStretchPoolInheritance(t *testing.T) {
	cluster := adviceCephCluster("ceph", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	policyLess := v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "rbd"},
		Spec:     v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
	}
	policied := v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: "ec-policy"},
		},
	}
	state := adviceState(cluster)
	state.StoragePools = []v1alpha1.StoragePool{policyLess, policied}

	notices := findingsWith(StorageAdvisories(state), "policy-less pools inherit the stretch rule")
	if len(notices) != 1 {
		t.Fatalf("a stretch cluster with a policy-less pool must raise one stretch-pool notice, got %+v", StorageAdvisories(state))
	}
	got := notices[0]
	if got.Severity != SeverityInfo {
		t.Fatalf("stretch-pool notice must be INFO, got %q", got.Severity)
	}
	wantSize := fmt.Sprintf("size %d/minSize %d", topology.StretchReplicatedPoolSize, topology.StretchReplicatedPoolMinSize)
	if !strings.Contains(got.Finding, wantSize) {
		t.Fatalf("stretch-pool notice must cite the topology constants %q, got %q", wantSize, got.Finding)
	}
	if !strings.Contains(got.Finding, "rbd") {
		t.Fatalf("stretch-pool notice must name the policy-less pool, got %q", got.Finding)
	}
	if strings.Contains(got.Finding, "rgw") {
		t.Fatalf("stretch-pool notice must not name the policied pool, got %q", got.Finding)
	}
}

func TestStorageAdvisoriesNoStretchPoolNoticeWithoutStretchOrPools(t *testing.T) {
	plain := adviceCephCluster("plain", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	state := adviceState(plain)
	state.StoragePools = []v1alpha1.StoragePool{{
		Metadata: v1alpha1.Metadata{Name: "rbd"},
		Spec:     v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "plain"}},
	}}
	if got := findingsWith(StorageAdvisories(state), "policy-less pools inherit the stretch rule"); len(got) != 0 {
		t.Fatalf("a non-stretch cluster must raise no stretch-pool notice, got %+v", got)
	}

	stretchNoPolicyless := adviceCephCluster("ceph", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	stretchNoPolicyless.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	onlyPolicied := adviceState(stretchNoPolicyless)
	onlyPolicied.StoragePools = []v1alpha1.StoragePool{{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: "ec-policy"},
		},
	}}
	if got := findingsWith(StorageAdvisories(onlyPolicied), "policy-less pools inherit the stretch rule"); len(got) != 0 {
		t.Fatalf("a stretch cluster whose pools all reference a policy must raise no stretch-pool notice, got %+v", got)
	}
}

func TestStorageAdvisoriesSkipExternalClusters(t *testing.T) {
	external := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ext"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}
	if got := StorageAdvisories(adviceState(external)); len(got) != 0 {
		t.Fatalf("external clusters have no authored topology and must raise no advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesQuorumGuidanceIsDistributionNeutral(t *testing.T) {
	for _, distribution := range []string{
		v1alpha1.StorageCephDistributionOSS,
		v1alpha1.StorageCephDistributionRedHat,
		v1alpha1.StorageCephDistributionIBM,
	} {
		cluster := adviceCephCluster("q", distribution, "", []string{"mon", "mgr", "osd"})
		got := StorageAdvisories(adviceState(cluster))
		mon := findingsWith(got, "mon role")
		mgr := findingsWith(got, "mgr role")
		if len(mon) != 1 || len(mgr) != 1 {
			t.Fatalf("%s single-host cluster must raise one mon and one mgr advisory, got %+v", distribution, got)
		}
		for _, a := range append(mon, mgr...) {
			if strings.Contains(a.Impact, "IBM Storage Ceph") {
				t.Errorf("%s: quorum advisory must not brand general Ceph guidance as IBM Storage Ceph: %q", distribution, a.Impact)
			}
		}
	}
}

func storageRootFilesystemAdviceState(diskGiB int) v1alpha1.State {
	cluster := adviceCephCluster("ceph", v1alpha1.StorageCephDistributionOSS, "", []string{"mon", "mgr", "osd"})
	cluster.Spec.Ceph.Topology.Nodes[0].MachineRef = v1alpha1.LocalObjectReference{Name: "node-0"}
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "node-0"},
			Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "virt"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			}},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "virt"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerLibvirt,
				Libvirt: &v1alpha1.InfraProviderLibvirt{MachineProfiles: []v1alpha1.MachineProfile{{
					Name: "ceph", DiskGiB: diskGiB,
				}}},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{cluster},
	}
}

func TestStorageAdvisoriesWarnBelowComputedRootFilesystemBudget(t *testing.T) {
	state := storageRootFilesystemAdviceState(30)
	got := findingsWith(StorageAdvisories(state), "root-filesystem service budget")
	if len(got) != 1 {
		t.Fatalf("a root disk between the hard floor and computed budget must raise one advisory, got %+v", StorageAdvisories(state))
	}
	advisory := got[0]
	if advisory.Severity != SeverityWarn || advisory.Group != rootFilesystemGroup {
		t.Fatalf("root-filesystem advisory severity/group = %q/%q", advisory.Severity, advisory.Group)
	}
	for _, want := range []string{"node-0", "InfraProvider/virt", "profile \"ceph\"", "30 GiB", "40 GiB"} {
		if !strings.Contains(advisory.Finding, want) {
			t.Errorf("root-filesystem advisory finding missing %q: %s", want, advisory.Finding)
		}
	}
	for _, want := range []string{"spec.libvirt.machineProfiles", "diskGiB", "at least 40", "before the first apply"} {
		if !strings.Contains(advisory.Remediation, want) {
			t.Errorf("root-filesystem advisory remediation missing %q: %s", want, advisory.Remediation)
		}
	}
}

func TestStorageAdvisoriesLeaveHardFloorAndSatisfiedBudgetToTheirOwners(t *testing.T) {
	for _, diskGiB := range []int{topology.RootFilesystemFloorGiB - 1, 40} {
		state := storageRootFilesystemAdviceState(diskGiB)
		if got := findingsWith(StorageAdvisories(state), "root-filesystem service budget"); len(got) != 0 {
			t.Fatalf("diskGiB %d must not raise the computed-budget advisory, got %+v", diskGiB, got)
		}
	}
}
