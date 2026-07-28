package desiredstate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateArtifactServerRequirementsStorageBareMetalManagedOS(t *testing.T) {
	baseState := func() v1alpha1.State {
		return v1alpha1.State{
			InfraProviders: []v1alpha1.InfraProvider{{
				Metadata: v1alpha1.Metadata{Name: "baremetal"},
				Spec: v1alpha1.InfraProviderSpec{
					Type:      v1alpha1.ProvisionerBareMetal,
					BareMetal: &v1alpha1.InfraProviderBareMetal{},
				},
			}},
			Machines: []v1alpha1.Machine{{
				Metadata: v1alpha1.Metadata{Name: "ceph-0"},
				Spec: v1alpha1.MachineSpec{
					Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
					Substrate:    v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "baremetal"}},
					OS: v1alpha1.MachineOSSpec{
						Provided:          v1alpha1.BoolPtr(false),
						InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel-9-ceph"},
					},
				},
			}},
			StorageClusters: []v1alpha1.StorageCluster{{
				Metadata: v1alpha1.Metadata{Name: "ceph"},
				Spec: v1alpha1.StorageClusterSpec{
					Type: v1alpha1.StorageClusterTypeCeph,
					Ceph: &v1alpha1.StorageClusterCephSpec{
						Topology: v1alpha1.StorageCephTopology{
							Nodes: []v1alpha1.StorageCephNode{
								storageValidationCephNode("ceph-0", "", []string{"mon"}),
							},
						},
					},
				},
			}},
			MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
				Metadata: v1alpha1.Metadata{Name: "rhel-9-ceph"},
				Spec: v1alpha1.MachineInstallProfileSpec{
					Installer: v1alpha1.MachineInstallProfileInstaller{
						Anaconda: &v1alpha1.MachineInstallAnaconda{},
					},
				},
			}},
		}
	}

	t.Run("missing artifact server and defaults", func(t *testing.T) {
		state := baseState()
		state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
		errs := strings.Join(validateArtifactServerRequirements(state), "; ")
		if !strings.Contains(errs, "StorageCluster/ceph") || !strings.Contains(errs, "spec.infraComponents.artifactServers") {
			t.Fatalf("want a bare-metal managed-OS artifact-publication error, got %q", errs)
		}
	})

	t.Run("artifact server present but consumer endpoint unset", func(t *testing.T) {
		state := baseState()
		state.Environments = []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					ArtifactServers: []v1alpha1.EnvironmentArtifactServerComponent{{
						Name:         "default",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
					}},
				},
			},
		}}
		errs := strings.Join(validateArtifactServerRequirements(state), "; ")
		if !strings.Contains(errs, "MachineInstallProfile/rhel-9-ceph spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.endpointRef is required") {
			t.Fatalf("want a missing managed-OS artifact endpoint error, got %q", errs)
		}
	})

	t.Run("provided-OS bare-metal node needs no artifact access", func(t *testing.T) {
		state := baseState()
		state.Machines[0].Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)}
		state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
		if errs := validateArtifactServerRequirements(state); len(errs) != 0 {
			t.Fatalf("provided-OS node must not require artifact access, got %v", errs)
		}
	})
}

func TestStorageStretchValidationAcceptsCanonicalShape(t *testing.T) {
	if errs := validateStorage(storageValidationState()); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestStorageStretchTiebreakerSafetyChecksSurviveFQDNNormalization(t *testing.T) {
	state := storageValidationState()
	state.Environments = []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec:     v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"}},
	}}
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "ceph-arbiter" {
			state.Machines[i].Spec.OS.Provided = v1alpha1.BoolPtr(false)
		}
	}
	arbiter := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6]
	arbiter.Roles = []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleMGR}

	Normalize(&state)

	if got := state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Name; got != "ceph-arbiter.ceph.example.test" {
		t.Fatalf("precondition: normalize did not FQDN-qualify the arbiter hostname, got %q", got)
	}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `tiebreaker.node "ceph-arbiter" must be mon-only`) {
		t.Fatalf("stretch tiebreaker mon-only check did not fire after FQDN normalization; errors = %q", got)
	}
}

func TestStorageStretchAcceptsPerSiteObjectGatewayPair(t *testing.T) {
	state := storageValidationState()
	state.StorageObjectGateways = []v1alpha1.StorageObjectGateway{
		{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc1"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-dc1.example.test", Scheme: "https", Port: 443},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf.dc1",
					Placement: v1alpha1.StoragePlacement{Sites: []string{"dc1"}},
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "dc1", Address: "192.168.141.80", PrefixLength: 24,
						Placement: v1alpha1.StoragePlacement{Sites: []string{"dc1"}},
					}},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc2"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-dc2.example.test", Scheme: "https", Port: 443},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf.dc2",
					Placement: v1alpha1.StoragePlacement{Sites: []string{"dc2"}},
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "dc2", Address: "192.168.142.80", PrefixLength: 24,
						Placement: v1alpha1.StoragePlacement{Sites: []string{"dc2"}},
					}},
				},
			},
		},
	}
	state.StorageExports[0].Spec.DataFoundation.ObjectGatewayRef = v1alpha1.LocalObjectReference{Name: "rgw-dc1"}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("two per-site StorageObjectGateway objects, each scoped to its own data site, must validate cleanly under stretch mode: %v", errs)
	}
}

func TestStorageStretchRejectsUnnarrowedPlacementMissingASite(t *testing.T) {
	state := storageValidationState()
	state.StorageObjectGateways[0].Spec.Ceph.Placement = v1alpha1.StoragePlacement{
		Hosts: []string{"ceph-dc1-0", "ceph-dc1-1"},
	}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `data site "dc2"`) {
		t.Fatalf("an unnarrowed gateway placement that only reaches one site must still fail full-coverage validation; errors = %q", got)
	}
}

func TestStorageIngressVRRPCollisionOnSharedL2Rejected(t *testing.T) {
	state := storageValidationState()
	state.StorageObjectGateways[0].Spec.Ceph.Ingresses = []v1alpha1.StorageObjectGatewayIngress{{
		Name: "ha", Address: "192.168.140.80", PrefixLength: 24,
		VirtualInterfaceNetworks: []string{"192.168.140.0/24"},
		FirstVirtualRouterID:     51,
	}}
	state.StorageClusters[0].Spec.Ceph.Management = &v1alpha1.StorageCephManagement{
		DNSLabel: "mgr",
		Ingress: v1alpha1.StorageCephManagementIngress{
			Name: "mgmt", Address: "192.168.140.81", PrefixLength: 24,
			VirtualInterfaceNetworks: []string{"192.168.140.0/24"},
			FirstVirtualRouterID:     51,
		},
	}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "both set firstVirtualRouterID 51 on overlapping virtualInterfaceNetworks") {
		t.Fatalf("two ingress groups sharing both a VRRP router ID and an L2 network must be rejected; errors = %q", got)
	}
}

func TestStorageIngressVRRPSameIDDisjointNetworksAccepted(t *testing.T) {
	state := storageValidationState()
	state.StorageObjectGateways = []v1alpha1.StorageObjectGateway{
		{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc1"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-dc1.example.test", Scheme: "https", Port: 443},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf.dc1",
					Placement: v1alpha1.StoragePlacement{Sites: []string{"dc1"}},
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "dc1", Address: "192.168.141.80", PrefixLength: 24,
						VirtualInterfaceNetworks: []string{"192.168.141.0/24"},
						FirstVirtualRouterID:     51,
						Placement:                v1alpha1.StoragePlacement{Sites: []string{"dc1"}},
					}},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc2"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-dc2.example.test", Scheme: "https", Port: 443},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf.dc2",
					Placement: v1alpha1.StoragePlacement{Sites: []string{"dc2"}},
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "dc2", Address: "192.168.142.80", PrefixLength: 24,
						VirtualInterfaceNetworks: []string{"192.168.142.0/24"},
						FirstVirtualRouterID:     51,
						Placement:                v1alpha1.StoragePlacement{Sites: []string{"dc2"}},
					}},
				},
			},
		},
	}
	state.StorageExports[0].Spec.DataFoundation.ObjectGatewayRef = v1alpha1.LocalObjectReference{Name: "rgw-dc1"}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("reusing a VRRP router ID across two disjoint, site-local subnets must not be rejected: %v", errs)
	}
}

func TestStorageStretchValidationAcceptsDeferredTiebreaker(t *testing.T) {
	state := storageValidationState()
	state.Machines = state.Machines[:6]
	cluster := &state.StorageClusters[0].Spec.Ceph.Topology
	cluster.Nodes = cluster.Nodes[:6]
	cluster.Stretch.Tiebreaker = v1alpha1.StorageCephTiebreaker{}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("an arbiter-less stretch cluster must validate with no hard errors, got %v", errs)
	}
}

func TestStorageStretchValidationRejectsPartialTiebreaker(t *testing.T) {
	state := storageValidationState()
	cluster := &state.StorageClusters[0].Spec.Ceph.Topology
	cluster.Nodes = cluster.Nodes[:6]
	cluster.Stretch.Tiebreaker = v1alpha1.StorageCephTiebreaker{Site: "dc3"}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "tiebreaker.node is required") {
		t.Fatalf("a partially authored tiebreaker must still fail; errors = %q", got)
	}
}

func TestStorageValidationAcceptsManagedManagementValue(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Management = v1alpha1.StorageClusterManagementManaged
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestStorageValidationAcceptsReleaseAndImagePins(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
	}{
		{
			name: "oss-version",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "19.2.1"
			},
		},
		{
			name: "oss-name",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "squid"
			},
		},
		{
			name: "oss-image-tag",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Version: "v19.2.1"}
			},
		},
		{
			name: "oss-image-digest",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Version: "sha256:" + strings.Repeat("a", 64)}
			},
		},
		{
			name: "redhat-stream-and-image",
			edit: func(state *v1alpha1.State) {
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeRedHatCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "9"
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Base: "registry.redhat.io/rhceph/rhceph-9-rhel9", Version: "9"}
			},
		},
		{
			name: "redhat-major-minor",
			edit: func(state *v1alpha1.State) {
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeRedHatCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "9.0"
			},
		},
		{
			name: "ibm-full-product-version",
			edit: func(state *v1alpha1.State) {
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeIBMStorageCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionIBM
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "9.9.1.0"
				state.StorageClusters[0].Spec.Ceph.IBM = &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			if errs := validateStorage(state); len(errs) != 0 {
				t.Fatalf("validateStorage returned errors: %v", errs)
			}
		})
	}
}

func TestStorageRemovedSSHFieldsRejectUnknown(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "node-ssh",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      nodeSSH:
        keyPairRef: ceph-node-ssh
`,
			want: "field nodeSSH not found",
		},
		{
			name: "cluster-ssh",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        keyPairRef: ceph-cluster-ssh
`,
			want: "field keyPairRef not found",
		},
		{
			name: "cluster-ssh-key-ref-retired",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      clusterSSHKeyRef: ceph-cluster-ssh
`,
			want: "field clusterSSHKeyRef not found",
		},
		{
			name: "cluster-ssh-user-retired",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      clusterSSHUser: cephadm
`,
			want: "field clusterSSHUser not found",
		},
		{
			name: "export-ssh-execution-retired",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata: { name: export }
spec:
  type: dataFoundation
  storageClusterRef: ceph
  externalDetails:
    sshExecution:
      knownHostsRef: removed-known-hosts
`,
			want: "field sshExecution not found",
		},
		{
			name: "community-release-retired",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    community:
      release: squid
`,
			want: "field release not found",
		},
		{
			name: "stretch-replicated-pool-defaults-retired",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata: { name: ceph }
spec:
  type: ceph
  ceph:
    topology:
      stretch:
        failureDomain: datacenter
        replicatedPoolDefaults:
          size: 4
          minSize: 2
`,
			want: "field replicatedPoolDefaults not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"storage.yaml": tc.body})
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected unknown field error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestStorageFilesystemDataPoolRefsAcceptPlainNames(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"filesystem.yaml": `apiVersion: bootwright.io/v1alpha1
kind: StorageFilesystem
metadata: { name: cephfs }
spec:
  storageClusterRef: ceph
  cephfs:
    metadataPoolRef: cephfs-meta
    dataPoolRefs:
      - cephfs-data
      - name: cephfs-archive
        default: true
`})
	state, err := Load([]string{dir})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	got := state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs
	want := []v1alpha1.StorageCephFSDataPoolRef{
		{Name: "cephfs-data"},
		{Name: "cephfs-archive", Default: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dataPoolRefs = %#v, want %#v", got, want)
	}
}

func TestStorageFilesystemDataPoolRefsRejectUnknownObjectFields(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"filesystem.yaml": `apiVersion: bootwright.io/v1alpha1
kind: StorageFilesystem
metadata: { name: cephfs }
spec:
  storageClusterRef: ceph
  cephfs:
    metadataPoolRef: cephfs-meta
    dataPoolRefs:
      - name: cephfs-data
        weight: 2
`})
	_, err := Load([]string{dir})
	if err == nil {
		t.Fatal("expected unknown field error, got nil")
	}
	if !strings.Contains(err.Error(), "field weight not found") {
		t.Fatalf("error %q does not reject unknown field", err)
	}
}

const cephNodeSSHSecretYAML = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: { name: ceph-node-ssh }
spec:
  type: sshKeyPair
  source:
    generated:
      comment: bootwright-ceph-node
`

func TestStorageCephRoleVocabularyAuthorableFromYAML(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
`
	const machine = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  capabilities:
    - ceph-node
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.10
  access:
    ssh:
      user: root
      auth:
        privateKeyRef: ceph-node-ssh
      addressRef: ssh
`
	const storage = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      bootstrap:
        node: node01
    monitoring:
      prometheus:
        retentionTime: 15d
    topology:
      nodes:
        - name: node01
          machineRef: ceph-0
          site: lab
          roles: [ROLES]
`
	cases := []struct {
		name  string
		roles string
		want  string
	}{
		{name: "monitoring-role-passes", roles: "mon, prometheus"},
		{
			name:  "bogus-role-fails",
			roles: "mon, observer",
			want:  `roles[1] "observer" must be one of {mon, mgr, osd, mds, rgw, ingress, prometheus, grafana, alertmanager}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"environment.yaml": environment,
				"secrets.yaml":     cephNodeSSHSecretYAML,
				"machine.yaml":     machine,
				"storage.yaml":     strings.ReplaceAll(storage, "ROLES", tc.roles),
			})
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.want == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected invalid role error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestStorageCephNodeSiteRequiredOnlyWhereItHasEffect(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
`
	const machine = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  capabilities:
    - ceph-node
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.10
  access:
    ssh:
      user: root
      auth:
        privateKeyRef: ceph-node-ssh
      addressRef: ssh
`
	const storage = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      bootstrap:
        node: node01
MONITORING
    topology:
      nodes:
        - name: node01
          machineRef: ceph-0
          roles: [mon, prometheus]
`
	cases := []struct {
		name       string
		monitoring string
		want       string
	}{
		{
			name:       "site-optional-without-stretch-or-site-placement",
			monitoring: "",
		},
		{
			name:       "site-required-under-site-narrowed-placement",
			monitoring: "    monitoring:\n      prometheus:\n        placement:\n          sites: [lab]\n",
			want:       "spec.ceph.topology.nodes[0].site is required when spec.ceph.monitoring.prometheus.placement narrows by sites",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"environment.yaml": environment,
				"secrets.yaml":     cephNodeSSHSecretYAML,
				"machine.yaml":     machine,
				"storage.yaml":     strings.Replace(storage, "MONITORING\n", tc.monitoring, 1),
			})
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.want == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected site requirement error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestStorageStretchValidationRejectsInvalidRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "tiebreaker-not-mon-only",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Roles = append(state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Roles, v1alpha1.StorageCephRoleMGR)
			},
			want: `tiebreaker.node "ceph-arbiter" must be mon-only`,
		},
		{
			name: "bad-data-site-mon-count",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[1].Roles = []string{v1alpha1.StorageCephRoleOSD}
			},
			want: `requires exactly two mon nodes in data site "dc1"`,
		},
		{
			name: "stretch-host-missing-site",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Site = ""
			},
			want: "spec.ceph.topology.nodes[2].site is required when spec.ceph.topology.stretch is set",
		},
		{
			name: "erasure-coded-pool",
			edit: func(state *v1alpha1.State) {
				state.StoragePools[0].Spec.Ceph.Type = v1alpha1.StoragePoolTypeErasureCode
				state.StoragePools[0].Spec.Ceph.ErasureCoded = &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}
			},
			want: `ceph.type "erasure" is not supported for stretch-mode`,
		},
		{
			name: "cephfs-data-pool-equals-metadata",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Name = "metadata"
			},
			want: `must be distinct from metadataPoolRef`,
		},
		{
			name: "mds-placement-does-not-cover-sites",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.MDS.Placement.Hosts = []string{"ceph-dc1-0", "ceph-dc1-1"}
			},
			want: `must include at least two mds-capable hosts in data site "dc2"`,
		},
		{
			name: "invalid-ceph-distribution",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Distribution = "enterprise"
			},
			want: `spec.ceph.distribution "enterprise" must be one of`,
		},
		{
			name: "oss-entitlement-ref",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
			},
			want: "spec.ceph.entitlementRef must be empty when distribution=oss",
		},
		{
			name: "redhat-missing-entitlement-ref",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
			},
			want: "spec.ceph.entitlementRef is required when distribution requires subscription or license handling",
		},
		{
			name: "redhat-wrong-entitlement-type",
			edit: func(state *v1alpha1.State) {
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeIBMStorageCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
			},
			want: `resolves to type "ibm-storage-ceph", want "redhat-ceph"`,
		},
		{
			name: "community-on-non-oss",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeRedHatCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Community = &v1alpha1.StorageCephCommunitySpec{Mirror: "https://download.ceph.com"}
			},
			want: "spec.ceph.community must be empty unless distribution=oss",
		},
		{
			name: "release-bad-oss-name",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "Squid!"
			},
			want: `spec.ceph.release "Squid!" must be an upstream Ceph release name (e.g. squid) or an x.y.z version (e.g. 19.2.1)`,
		},
		{
			name: "release-bad-oss-partial-version",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "19.2"
			},
			want: `spec.ceph.release "19.2" must be an upstream Ceph release name`,
		},
		{
			name: "release-malformed-oss-version",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "20.2"
			},
			want: `spec.ceph.release "20.2" must be an upstream Ceph release name`,
		},
		{
			name: "release-bad-redhat-stream",
			edit: func(state *v1alpha1.State) {
				state.Entitlements = []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
					Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeRedHatCeph},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "squid"
			},
			want: `spec.ceph.release "squid" must be a dot-separated numeric product version such as 9, 9.1, or 9.9.1.0; its leading component selects the product stream`,
		},
		{
			name: "image-mutable-latest",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Version: "latest"}
			},
			want: `spec.ceph.image.version "latest" must be an image tag or a sha256: digest`,
		},
		{
			name: "image-base-carries-a-tag",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Base: "quay.io/ceph/ceph:v19.2.1", Version: "v19.2.1"}
			},
			want: `spec.ceph.image.base "quay.io/ceph/ceph:v19.2.1" must be a bare <registry>/<path> reference`,
		},
		{
			name: "image-base-without-version",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Release = "squid"
				state.StorageClusters[0].Spec.Ceph.Image = &v1alpha1.StorageCephImageSpec{Base: "quay.io/ceph/ceph"}
			},
			want: `spec.ceph.image.base names no image until .version completes it`,
		},
		{
			name: "package-version-on-oss",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.PackageVersion = "19.2.1-245.el9cp"
			},
			want: `spec.ceph.packageVersion must be empty when distribution=oss`,
		},
		{
			name: "community-bad-mirror",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Community = &v1alpha1.StorageCephCommunitySpec{Mirror: "ftp://mirror.example.test/ceph"}
			},
			want: "spec.ceph.community.mirror \"ftp://mirror.example.test/ceph\" must be an https URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageCephNodeOSDDeviceSelectionExplicit(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "osd-role-without-device-selection",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Devices = nil
			},
			want: `spec.ceph.topology.nodes[2] carries the "osd" role but selects no devices; author devices, osd.dataDevices, or cover it with a fleet osdDrivegroup`,
		},
		{
			name: "devices-without-osd-role",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Devices = []string{"/dev/vdb"}
			},
			want: `spec.ceph.topology.nodes[6].devices requires the "osd" role`,
		},
		{
			name: "explicit-all-devices-passes",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
				}
			},
		},
		{
			name: "model-vendor-selection-passes",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{Model: "MZ7", Vendor: "ATA"},
					FilterLogic: "OR",
				}
			},
		},
		{
			name: "filter-logic-rejects-non-and-or",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
					FilterLogic: "and",
				}
			},
			want: `.osd.filterLogic "and" must be AND or OR`,
		},
		{
			name: "tpm2-requires-encrypted",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
					TPM2:        true,
				}
			},
			want: `.osd.tpm2 requires encrypted: true`,
		},
		{
			name: "data-allocate-fraction-out-of-range",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices:          &v1alpha1.StorageCephDeviceSelection{All: true},
					DataAllocateFraction: 1.5,
				}
			},
			want: `must be in (0, 1]`,
		},
		{
			name: "pathspecs-and-paths-mutually-exclusive",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{
						Paths:     []string{"/dev/sdb"},
						PathSpecs: []v1alpha1.StorageCephDevicePath{{Path: "/dev/sdc"}},
					},
				}
			},
			want: `must set only one of paths or pathSpecs`,
		},
		{
			name: "size-range-rejects-dash-separator",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{Size: "10G-40G"},
				}
			},
			want: `uses '-' as a range separator`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			errs := validateStorage(state)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("validateStorage returned errors: %v", errs)
				}
				return
			}
			got := strings.Join(errs, "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageCephDevicePathValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "empty-shorthand-path",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Devices = []string{""}
			},
			want: `spec.ceph.topology.nodes[2].devices[0] must not be empty`,
		},
		{
			name: "relative-shorthand-path",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Devices = []string{"sdb"}
			},
			want: `must be an absolute /dev path`,
		},
		{
			name: "duplicate-shorthand-path",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Devices = []string{"/dev/sdb", "/dev/sdb"}
			},
			want: `is listed more than once`,
		},
		{
			name: "duplicate-datadevices-path",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/sdb", "/dev/sdb"}},
				}
			},
			want: `spec.ceph.topology.nodes[2].osd.dataDevices.paths[1] "/dev/sdb" is listed more than once`,
		},
		{
			name: "by-id-path-passes",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2].Devices = []string{"/dev/disk/by-id/wwn-0x5000c500a"}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			errs := validateStorage(state)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("validateStorage returned errors: %v", errs)
				}
				return
			}
			got := strings.Join(errs, "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStoragePoolTuningValidation(t *testing.T) {
	neg := int64(-1)
	cases := []struct {
		name string
		spec v1alpha1.StoragePoolCephSpec
		want string
	}{
		{name: "valid", spec: v1alpha1.StoragePoolCephSpec{
			Autoscale:   &v1alpha1.StoragePoolAutoscale{Mode: "warn", TargetSizeRatio: 0.5},
			Compression: &v1alpha1.StoragePoolCompression{Mode: "passive", Algorithm: "lz4"},
		}},
		{name: "bad-autoscale-mode", spec: v1alpha1.StoragePoolCephSpec{
			Autoscale: &v1alpha1.StoragePoolAutoscale{Mode: "auto"},
		}, want: `.autoscale.mode "auto" must be one of`},
		{name: "target-ratio-and-bytes", spec: v1alpha1.StoragePoolCephSpec{
			Autoscale: &v1alpha1.StoragePoolAutoscale{TargetSizeRatio: 0.5, TargetSizeBytes: "10G"},
		}, want: `at most one of targetSizeRatio or targetSizeBytes`},
		{name: "pgnum-min-exceeds-max", spec: v1alpha1.StoragePoolCephSpec{
			Autoscale: &v1alpha1.StoragePoolAutoscale{PGNumMin: 64, PGNumMax: 32},
		}, want: `pgNumMin must not exceed pgNumMax`},
		{name: "negative-quota", spec: v1alpha1.StoragePoolCephSpec{
			Quota: &v1alpha1.StoragePoolQuota{MaxBytes: &neg},
		}, want: `.quota.maxBytes must be non-negative`},
		{name: "bad-compression-algorithm", spec: v1alpha1.StoragePoolCephSpec{
			Compression: &v1alpha1.StoragePoolCompression{Mode: "force", Algorithm: "gzip"},
		}, want: `.compression.algorithm "gzip" must be one of`},
		{name: "compression-without-mode", spec: v1alpha1.StoragePoolCephSpec{
			Compression: &v1alpha1.StoragePoolCompression{Algorithm: "zstd"},
		}, want: `compression sets tuning without compression.mode`},
		{name: "valid-mirroring", spec: v1alpha1.StoragePoolCephSpec{
			Mirroring: &v1alpha1.StoragePoolMirroring{Mode: "image"},
		}},
		{name: "bad-mirroring-mode", spec: v1alpha1.StoragePoolCephSpec{
			Mirroring: &v1alpha1.StoragePoolMirroring{Mode: "snapshot"},
		}, want: `.mirroring.mode "snapshot" must be one of`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateStoragePoolTuning("StoragePool/p spec.ceph", tc.spec)
			got := strings.Join(errs, "; ")
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageECProfileValidation(t *testing.T) {
	cases := []struct {
		name string
		ec   *v1alpha1.StoragePoolErasureCode
		want string
	}{
		{name: "valid", ec: &v1alpha1.StoragePoolErasureCode{DataChunks: 4, CodingChunks: 2, Plugin: "isa", Parameters: map[string]string{"d": "5"}}},
		{name: "bad-plugin", ec: &v1alpha1.StoragePoolErasureCode{Plugin: "raid5"}, want: `.plugin "raid5" must be one of`},
		{name: "parameters-duplicate-first-class", ec: &v1alpha1.StoragePoolErasureCode{Parameters: map[string]string{"crush-device-class": "ssd"}}, want: `is owned by a first-class erasure field`},
		{name: "parameters-empty-value", ec: &v1alpha1.StoragePoolErasureCode{Parameters: map[string]string{"d": ""}}, want: `.parameters[d] must not be empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateStorageECProfile("StoragePool/p spec.ceph.erasure", tc.ec)
			got := strings.Join(errs, "; ")
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestValidateSingleHostDefaults(t *testing.T) {
	base := func() v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{SingleHostDefaults: true}},
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
					Name:    "ceph-0",
					Roles:   []string{v1alpha1.StorageCephRoleOSD},
					Devices: []string{"/dev/sdb", "/dev/sdc"},
				}}},
			}},
		}
	}
	if errs := validateStorageCephSingleHostDefaults("p", base()); len(errs) != 0 {
		t.Fatalf("single-host cluster should pass, got %v", errs)
	}
	multi := base()
	multi.Spec.Ceph.Topology.Nodes = append(multi.Spec.Ceph.Topology.Nodes, v1alpha1.StorageCephNode{Name: "ceph-1"})
	if got := strings.Join(validateStorageCephSingleHostDefaults("p", multi), "; "); !strings.Contains(got, "single-host topology") {
		t.Fatalf("multi-host should be rejected, got %q", got)
	}
	conflict := base()
	conflict.Spec.Ceph.Config = map[string]map[string]string{"global": {"osd_pool_default_size": "1"}}
	if got := strings.Join(validateStorageCephSingleHostDefaults("p", conflict), "; "); !strings.Contains(got, "osd_pool_default_size") {
		t.Fatalf("config conflict should be rejected, got %q", got)
	}
	oneOSD := base()
	oneOSD.Spec.Ceph.Topology.Nodes[0].Devices = []string{"/dev/sdb"}
	if got := strings.Join(validateStorageCephSingleHostDefaults("p", oneOSD), "; "); !strings.Contains(got, "requires at least 2 OSDs") {
		t.Fatalf("one static OSD should be rejected, got %q", got)
	}
	dynamic := base()
	dynamic.Spec.Ceph.Topology.Nodes[0].Devices = nil
	dynamic.Spec.Ceph.Topology.Nodes[0].OSD = &v1alpha1.StorageCephNodeOSD{
		DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
	}
	if errs := validateStorageCephSingleHostDefaults("p", dynamic); len(errs) != 0 {
		t.Fatalf("dynamic single-host selection should defer count to readiness, got %v", errs)
	}
}

func TestStorageValidationRejectsSingleHostWithOneStaticOSD(t *testing.T) {
	state := storageValidationState()
	state.ContainerClusters = nil
	state.Machines = state.Machines[:1]
	state.StoragePools = nil
	state.StorageFilesystems = nil
	state.StorageObjectGateways = nil
	state.StorageExports = nil
	state.ClusterAddons = nil
	state.ClusterAddonBindings = nil
	cluster := &state.StorageClusters[0]
	cluster.Spec.Ceph.Topology.Stretch = nil
	cluster.Spec.Ceph.Topology.Nodes = []v1alpha1.StorageCephNode{
		storageValidationCephNode("ceph-dc1-0", "", []string{"mon", "mgr", "osd"}),
	}
	cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults = true

	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "spec.ceph.cephadm.bootstrap.singleHostDefaults requires at least 2 OSDs; the statically declared topology creates 1") {
		t.Fatalf("validateStorage errors = %q, want the single-host static OSD minimum", got)
	}

	cluster.Spec.Ceph.Topology.Nodes[0].Devices = []string{"/dev/vdb", "/dev/vdc"}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("two-OSD single-host topology should validate, got %v", errs)
	}
}

func TestStorageManagementAuthGate(t *testing.T) {
	on := true
	off := false
	state := v1alpha1.State{Secrets: []v1alpha1.Secret{
		{Metadata: v1alpha1.Metadata{Name: "cert"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeTLSCertificate}},
		{Metadata: v1alpha1.Metadata{Name: "key"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeTLSCertificate}},
		{Metadata: v1alpha1.Metadata{Name: "client"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeToken}},
	}}
	clusterWith := func(mgmt *v1alpha1.StorageCephManagement) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Management: mgmt,
			Topology:   v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"ingress"}}}},
		}}}
	}
	baseIngress := v1alpha1.StorageCephManagementIngress{Name: "m", Address: "10.0.0.9", PrefixLength: 24}
	validOAuth := &v1alpha1.StorageCephOAuth2Proxy{ProviderDisplayName: "Corp", ClientID: "id", ClientSecretRef: v1alpha1.LocalObjectReference{Name: "client"}, OIDCIssuerURL: "https://idp"}

	cases := []struct {
		name string
		mgmt *v1alpha1.StorageCephManagement
		want string
	}{
		{name: "auth-without-oauth2", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "d", EnableAuth: &on, Ingress: baseIngress}, want: "enableAuth requires oauth2Proxy"},
		{name: "oauth2-without-auth", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "d", EnableAuth: &off, OAuth2Proxy: validOAuth, Ingress: baseIngress}, want: "oauth2Proxy requires enableAuth"},
		{name: "valid-auth", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "d", EnableAuth: &on, OAuth2Proxy: validOAuth, Ingress: baseIngress}},
		{name: "tls-bad-ref", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "d", TLS: &v1alpha1.StorageCephManagementTLS{CertificateRef: v1alpha1.LocalObjectReference{Name: "missing"}, KeyRef: v1alpha1.LocalObjectReference{Name: "key"}}, Ingress: baseIngress}, want: `"missing" is not a declared Secret`},
		{name: "dns-label-fqdn", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "dash.example.test", Ingress: baseIngress}, want: `dnsLabel "dash.example.test" is not a valid DNS label`},
		{name: "dns-label-uppercase", mgmt: &v1alpha1.StorageCephManagement{DNSLabel: "Dash", Ingress: baseIngress}, want: `dnsLabel "Dash" is not a valid DNS label`},
		{name: "dns-label-omitted", mgmt: &v1alpha1.StorageCephManagement{Ingress: baseIngress}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(validateStorageCephManagement("spec.ceph.management", clusterWith(tc.mgmt), state), "; ")
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

func TestStorageGatewayIngressTLSValidation(t *testing.T) {
	baseState := v1alpha1.State{Secrets: []v1alpha1.Secret{
		{Metadata: v1alpha1.Metadata{Name: "cert"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeTLSCertificate}},
		{Metadata: v1alpha1.Metadata{Name: "key"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeTLSCertificate}},
		{Metadata: v1alpha1.Metadata{Name: "wrong-type"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeToken}},
	}}
	stateWith := func(tls *v1alpha1.StorageObjectGatewayIngressTLS) v1alpha1.State {
		s := baseState
		s.StorageObjectGateways = []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf",
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "ha", Address: "10.0.0.9", PrefixLength: 24, TLS: tls,
					}},
				},
			},
		}}
		return s
	}

	cases := []struct {
		name string
		tls  *v1alpha1.StorageObjectGatewayIngressTLS
		want string
	}{
		{name: "no-tls", tls: nil},
		{name: "valid", tls: &v1alpha1.StorageObjectGatewayIngressTLS{
			CertificateRef: v1alpha1.LocalObjectReference{Name: "cert"},
			KeyRef:         v1alpha1.LocalObjectReference{Name: "key"},
		}},
		{name: "missing-ref", tls: &v1alpha1.StorageObjectGatewayIngressTLS{
			CertificateRef: v1alpha1.LocalObjectReference{Name: "missing"},
			KeyRef:         v1alpha1.LocalObjectReference{Name: "key"},
		}, want: `"missing" is not a declared Secret`},
		{name: "wrong-type", tls: &v1alpha1.StorageObjectGatewayIngressTLS{
			CertificateRef: v1alpha1.LocalObjectReference{Name: "wrong-type"},
			KeyRef:         v1alpha1.LocalObjectReference{Name: "key"},
		}, want: `"wrong-type" is a token Secret but a tlsCertificate Secret is required`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(validateStorageGatewayIngressTLS(stateWith(tc.tls)), "; ")
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

func TestStorageGatewayRealmAndConfigValidation(t *testing.T) {
	gwWith := func(c v1alpha1.StorageObjectGatewayCephSpec) v1alpha1.StorageObjectGateway {
		return v1alpha1.StorageObjectGateway{Metadata: v1alpha1.Metadata{Name: "s3"}, Spec: v1alpha1.StorageObjectGatewaySpec{Ceph: c}}
	}
	if errs := validateStorageGatewayRealm("p", gwWith(v1alpha1.StorageObjectGatewayCephSpec{Realm: "r", ZoneGroup: "zg", Zone: "z"})); len(errs) != 0 {
		t.Fatalf("complete realm binding should pass, got %v", errs)
	}
	if got := strings.Join(validateStorageGatewayRealm("p", gwWith(v1alpha1.StorageObjectGatewayCephSpec{Realm: "r"})), "; "); !strings.Contains(got, "all-or-nothing") {
		t.Fatalf("partial realm binding should be rejected, got %q", got)
	}

	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Config: map[string]map[string]string{"client.rgw.s3": {"rgw_thread_pool_size": "512"}},
	}}}
	dup := gwWith(v1alpha1.StorageObjectGatewayCephSpec{ServiceID: "s3", Config: map[string]string{"rgw_thread_pool_size": "256"}})
	if got := strings.Join(validateStorageGatewayConfig("p", dup, cluster, true), "; "); !strings.Contains(got, "declare it in one place") {
		t.Fatalf("config key set in both places should be rejected, got %q", got)
	}
	reserved := gwWith(v1alpha1.StorageObjectGatewayCephSpec{ServiceID: "s3", Config: map[string]string{"rgw_frontend_port": "8080"}})
	if got := strings.Join(validateStorageGatewayConfig("p", reserved, v1alpha1.StorageCluster{}, false), "; "); !strings.Contains(got, "owned by spec.ceph.frontendPort") {
		t.Fatalf("rgw_frontend_port in config should be rejected, got %q", got)
	}
}

func TestValidCephConfigSectionMasks(t *testing.T) {
	valid := []string{"global", "osd", "mds.fs1", "osd/class:ssd", "osd/rack:r1", "client.rgw.s3/datacenter:dc1"}
	for _, s := range valid {
		if !validCephConfigSection(s) {
			t.Errorf("section %q should be valid", s)
		}
	}
	invalid := []string{"", "bogus", "osd/class:", "osd/:ssd", "osd/class:ssd/extra", "osd/nocolon", "mon."}
	for _, s := range invalid {
		if validCephConfigSection(s) {
			t.Errorf("section %q should be invalid", s)
		}
	}
}

func TestStorageServiceOverridesValidation(t *testing.T) {
	ok := validateStorageServiceOverrides("p", &v1alpha1.StorageCephServiceOverrides{
		Networks:      []string{"10.0.0.0/24"},
		CustomConfigs: []v1alpha1.StorageCephCustomConfig{{MountPath: "/etc/x", Content: "y"}},
	})
	if len(ok) != 0 {
		t.Fatalf("valid overrides: %v", ok)
	}
	bad := strings.Join(validateStorageServiceOverrides("p", &v1alpha1.StorageCephServiceOverrides{
		Networks:      []string{"not-a-cidr"},
		CustomConfigs: []v1alpha1.StorageCephCustomConfig{{MountPath: "rel/path", Content: ""}},
	}), "; ")
	for _, want := range []string{"is not a valid CIDR", "must be absolute", "content must not be empty"} {
		if !strings.Contains(bad, want) {
			t.Fatalf("errors = %q, want substring %q", bad, want)
		}
	}
}

func TestStorageFleetOSDDrivegroupOverlap(t *testing.T) {
	dg := func(id string, hosts ...string) v1alpha1.StorageCephOSDDrivegroup {
		return v1alpha1.StorageCephOSDDrivegroup{
			ServiceID: id,
			Placement: v1alpha1.StoragePlacement{Hosts: hosts},
			OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}},
		}
	}
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "fleet-covers-osd-role-host-passes",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				state.StorageClusters[0].Spec.Ceph.Topology.OSDDrivegroups = []v1alpha1.StorageCephOSDDrivegroup{dg("fleet", host.Name)}
			},
		},
		{
			name: "per-host-and-fleet-overlap-rejected",
			edit: func(state *v1alpha1.State) {
				host := state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				state.StorageClusters[0].Spec.Ceph.Topology.OSDDrivegroups = []v1alpha1.StorageCephOSDDrivegroup{dg("fleet", host.Name)}
			},
			want: "covered by a fleet osdDrivegroup; a host is owned by one OSD spec",
		},
		{
			name: "duplicate-fleet-service-id-rejected",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[2]
				host.Devices = nil
				state.StorageClusters[0].Spec.Ceph.Topology.OSDDrivegroups = []v1alpha1.StorageCephOSDDrivegroup{dg("fleet", host.Name), dg("fleet")}
			},
			want: `.serviceID "fleet" is duplicated`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
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

func TestStoragePoolTypeRejectsIncompatibleArms(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.StoragePoolCephSpec)
		want string
	}{
		{
			name: "erasure-coded-missing-arm",
			edit: func(spec *v1alpha1.StoragePoolCephSpec) {
				spec.Type = v1alpha1.StoragePoolTypeErasureCode
			},
			want: "ceph.erasure is required when ceph.type=erasure",
		},
		{
			name: "erasure-coded-replicated-arm",
			edit: func(spec *v1alpha1.StoragePoolCephSpec) {
				spec.Type = v1alpha1.StoragePoolTypeErasureCode
				spec.ErasureCoded = &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}
				spec.Replicated.Size = 3
			},
			want: "ceph.type=erasure must not set replicated",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			state.StorageClusters[0].Spec.Ceph.Topology.Stretch = nil
			tc.edit(&state.StoragePools[0].Spec.Ceph)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageFilesystemMDSPlacementValidated(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "dangling-placement-host",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.MDS.Placement.Hosts = []string{"ceph-typo"}
			},
			want: `spec.cephfs.mds.placement.hosts[0] "ceph-typo" does not match any node name in StorageCluster/ceph spec.ceph.topology.nodes`,
		},
		{
			name: "no-mds-role-anywhere",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.MDS.Placement = v1alpha1.StoragePlacement{}
				hosts := state.StorageClusters[0].Spec.Ceph.Topology.Nodes
				for i := range hosts {
					var roles []string
					for _, role := range hosts[i].Roles {
						if role != v1alpha1.StorageCephRoleMDS {
							roles = append(roles, role)
						}
					}
					hosts[i].Roles = roles
				}
			},
			want: `spec.cephfs.mds.placement resolves to no hosts: no StorageCluster/ceph spec.ceph.topology.nodes[] entry carries role "mds" within the selection`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			state.StorageClusters[0].Spec.Ceph.Topology.Stretch = nil
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageFilesystemMDSRolePlacementPasses(t *testing.T) {
	state := storageValidationState()
	state.StorageFilesystems[0].Spec.CephFS.MDS.Placement = v1alpha1.StoragePlacement{}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestStorageAttachmentRequiresDataFoundationProvider(t *testing.T) {
	state := storageValidationState()
	state.ClusterAddons[0].Spec.Provides = nil
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `requires ClusterAddon/odf to provide "dataFoundation"`) {
		t.Fatalf("validateStorage errors = %q, want dataFoundation provider error", got)
	}
}

func TestStorageDefaultsAndPublicEndpointNormalize(t *testing.T) {
	state := storageValidationState()
	cluster := &state.StorageClusters[0]
	cluster.Spec.Ceph.Cephadm.Bootstrap.AddressRef = v1alpha1.LocalObjectReference{}
	state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Default = false
	state.StorageExports[0].Spec.Type = ""

	Normalize(&state)

	bootstrap := state.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap
	if got := state.StorageClusters[0].Spec.Ceph.Distribution; got != v1alpha1.StorageCephDistributionOSS {
		t.Fatalf("ceph distribution = %q, want oss", got)
	}
	if bootstrap.AddressRef.Name != state.StorageClusters[0].Spec.Ceph.Cephadm.AddressRef.Name {
		t.Fatalf("bootstrap addressRef = %q, want cephadm addressRef", bootstrap.AddressRef.Name)
	}
	if !state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Default {
		t.Fatal("single CephFS data pool did not default to default=true")
	}
	if state.StorageExports[0].Spec.Type != v1alpha1.StorageExportTypeDataFoundation {
		t.Fatalf("storage export type = %q, want dataFoundation", state.StorageExports[0].Spec.Type)
	}
	if state.StorageExports[0].Spec.ExternalDetails != nil {
		t.Fatalf("storage export externalDetails = %#v, want nil (the consuming add-on produces the details)", state.StorageExports[0].Spec.ExternalDetails)
	}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors after defaults: %v", errs)
	}
}

func TestStorageObjectGatewayRejectsOldClientEndpoint(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
`,
		"gateway.yaml": `apiVersion: bootwright.io/v1alpha1
kind: StorageObjectGateway
metadata:
  name: odf-rgw
spec:
  storageClusterRef: ceph
  ceph:
    serviceID: odf
    clientEndpoint:
      host: rgw-ceph.example.test
`,
	}
	writeFiles(t, dir, files)
	_, err := Load([]string{dir})
	if err == nil {
		t.Fatal("expected old clientEndpoint field to be rejected")
	}
	if !strings.Contains(err.Error(), "field clientEndpoint not found") {
		t.Fatalf("error %q does not reject clientEndpoint", err)
	}
}

func TestExternalStorageValidationAcceptsImportedDataFoundation(t *testing.T) {
	if errs := validateStorage(externalStorageValidationState()); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestExternalStorageValidationRejectsPlaybookHookAgainstExternalCluster(t *testing.T) {
	state := externalStorageValidationState()
	state.ClusterAddons[0].Spec.Steps = []v1alpha1.ClusterAddonStep{{
		Name:     "attach-external-storage",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "playbooks/export-external-details.yaml",
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	}}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "spec.management=external") || !strings.Contains(got, "export-external-details.yaml") {
		t.Fatalf("validateStorage errors = %q, want an external-Ceph playbook-hook rejection naming the playbook", got)
	}
}

func TestExternalStorageValidationRequiresExternalDetailsSource(t *testing.T) {
	state := externalStorageValidationState()
	state.StorageExports[0].Spec.ExternalDetails = nil
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "spec.externalDetails is required when storageClusterRef points to external Ceph") {
		t.Fatalf("validateStorage errors = %q, want externalDetails requirement", got)
	}
}

func TestManagedStorageExportRejectsExternalDetails(t *testing.T) {
	state := storageValidationState()
	state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
		FromSecretRef: v1alpha1.SecretRef{Name: "stale-export-details"},
	}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "must be empty when storageClusterRef points to StorageCluster/ceph with managed Ceph") {
		t.Fatalf("validateStorage errors = %q, want a managed-Ceph externalDetails rejection", got)
	}
}

func TestManagedStorageExportOmittedExternalDetailsStaysNil(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Management = v1alpha1.StorageClusterManagementManaged
	state.StorageExports[0].Spec.ExternalDetails = nil
	Normalize(&state)
	if state.StorageExports[0].Spec.ExternalDetails != nil {
		t.Fatalf("managed externalDetails = %#v, want nil", state.StorageExports[0].Spec.ExternalDetails)
	}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestManagedStorageValidationRejectsInvalidHostSSH(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "missing-machine-ref",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[0].MachineRef = v1alpha1.LocalObjectReference{}
			},
			want: "spec.ceph.topology.nodes[0].machineRef is required",
		},
		{
			name: "missing-machine-ssh",
			edit: func(state *v1alpha1.State) {
				state.Machines[0].Spec.Access.SSH = nil
			},
			want: "Machine/ceph-dc1-0 declares no login",
		},
		{
			name: "missing-ceph-node-capability",
			edit: func(state *v1alpha1.State) {
				state.Machines[0].Spec.Capabilities = []string{v1alpha1.MachineCapabilityLibvirt}
			},
			want: `lacks capability "ceph-node"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestExternalStorageValidationRejectsInvalidFieldCombinations(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "non-data-foundation-export",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.Type = "nfs"
			},
			want: `input[external-storage].value "export" must reference a dataFoundation StorageExport`,
		},
		{
			name: "external-ceph-spec",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
			},
			want: "ceph must be empty when spec.management=external",
		},
		{
			name: "external-details-empty-from-secret-ref",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{}
			},
			want: "spec.externalDetails.fromSecretRef is required when externalDetails is set",
		},
		{
			name: "imported-and-managed-refs",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.DataFoundation = &v1alpha1.StorageExportDataFoundationSpec{
					RBDPoolRef: v1alpha1.LocalObjectReference{Name: "rbd"},
				}
			},
			want: "dataFoundation must be empty when storageClusterRef points to StorageCluster/shared-ceph with spec.management=external",
		},
		{
			name: "pool-on-external-cluster",
			edit: func(state *v1alpha1.State) {
				state.StoragePools = []v1alpha1.StoragePool{{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: storagePoolSpec("shared-ceph", v1alpha1.StoragePoolRoleRBD)}}
			},
			want: "Bootwright-managed pools are not declared for imported Ceph",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := externalStorageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageExportTypeMustEqualArmKey(t *testing.T) {
	state := storageValidationState()
	state.StorageExports[0].Spec.Type = "data-foundation"
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `StorageExport/export spec.type "data-foundation" must be "dataFoundation"`) {
		t.Fatalf("validateStorage errors = %q, want type/arm-key error", got)
	}
}

func TestStorageExportFromSecretRefRejectsObjectForm(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"export.yaml": `apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata: { name: export }
spec:
  type: dataFoundation
  storageClusterRef: shared-ceph
  externalDetails:
    fromSecretRef:
      name: shared-ceph-external-details
`})
	_, err := Load([]string{dir})
	if err == nil {
		t.Fatal("expected object-form fromSecretRef to be rejected")
	}
	if !strings.Contains(err.Error(), "a reference is a plain name string") {
		t.Fatalf("error %q does not reject the object form", err)
	}
}

func TestStorageExportFromSecretRefWalksEnvironmentSecrets(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "undeclared",
			ref:  "missing-details",
			want: `StorageExport/export spec.externalDetails.fromSecretRef "missing-details" is not a declared Secret`,
		},
		{
			name: "not-a-dns-label",
			ref:  "Not_A_Label",
			want: `StorageExport/export spec.externalDetails.fromSecretRef.name "Not_A_Label" is not a DNS label`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := externalStorageValidationState()
			state.Environments[0].Metadata.Name = "env"
			state.StorageExports[0].Spec.ExternalDetails.FromSecretRef = v1alpha1.SecretRef{Name: tc.ref}
			got := strings.Join(validateSecretReferences(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateSecretReferences errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func storageValidationState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Endpoints: map[string]v1alpha1.Endpoint{
						"rgw-public": {DNSName: "rgw-ceph.example.test", Port: 443, Scheme: "https"},
					},
				},
			},
		}},
		Machines: []v1alpha1.Machine{
			storageValidationHost("ceph-dc1-0"),
			storageValidationHost("ceph-dc1-1"),
			storageValidationHost("ceph-dc1-2"),
			storageValidationHost("ceph-dc2-0"),
			storageValidationHost("ceph-dc2-1"),
			storageValidationHost("ceph-dc2-2"),
			storageValidationHost("ceph-arbiter"),
		},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						ClusterSSH: v1alpha1.StorageCephadmSSHSpec{KeyRef: v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}},
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Node: "ceph-dc1-0",
						},
					},
					Topology: v1alpha1.StorageCephTopology{
						Stretch: &v1alpha1.StorageCephStretch{
							FailureDomain: "datacenter",
							DataSites:     []string{"dc1", "dc2"},
							Tiebreaker: v1alpha1.StorageCephTiebreaker{
								Site: "dc3",
								Node: "ceph-arbiter",
							},
							RuleName: "stretch-replicated",
						},
						Nodes: []v1alpha1.StorageCephNode{
							storageValidationCephNode("ceph-dc1-0", "dc1", []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-dc1-1", "dc1", []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-dc1-2", "dc1", []string{"osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-dc2-0", "dc2", []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-dc2-1", "dc2", []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-dc2-2", "dc2", []string{"osd", "mds", "rgw", "ingress"}),
							storageValidationCephNode("ceph-arbiter", "dc3", []string{"mon"}),
						},
					},
				},
			},
		}},
		StoragePools: []v1alpha1.StoragePool{
			{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleRBD)},
			{Metadata: v1alpha1.Metadata{Name: "metadata"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleCephFSMetadata)},
			{Metadata: v1alpha1.Metadata{Name: "data"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleCephFSData)},
		},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "cephfs"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "metadata"},
					DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "data", Default: true}},
					MDS: v1alpha1.StorageCephFSMetadataServices{
						Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"}},
					},
				},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-ceph.example.test", Scheme: "https", Port: 443},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf",
					Placement: v1alpha1.StoragePlacement{
						Hosts: []string{"ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"},
					},
				},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
					RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "rbd"},
					FilesystemRef:    v1alpha1.LocalObjectReference{Name: "cephfs"},
					ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "rgw"},
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
				Readiness: v1alpha1.ClusterAddonReadiness{
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
							APIVersion: "v1",
							Kind:       "Namespace",
							Name:       "openshift-storage",
						},
					}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf"}},
				AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
					AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
					Inputs:   []v1alpha1.ClusterAddonBindingInput{dataFoundationBindingInput("export")},
				}},
			},
		}},
		Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
		Secrets:      []v1alpha1.Secret{clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeSSHKeyPair)},
	}
}

func storageValidationHost(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: name + ".example.test"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef:    v1alpha1.LocalObjectReference{Name: "ssh"},
					User:          "root",
					Auth:        v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
					KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-known-hosts"},
				},
			},
		},
	}
}

func storageValidationCephNode(name, site string, roles []string) v1alpha1.StorageCephNode {
	node := v1alpha1.StorageCephNode{
		Name: name,
		MachineRef: v1alpha1.LocalObjectReference{
			Name: name,
		},
		Site:  site,
		Roles: roles,
	}
	for _, role := range roles {
		if role == v1alpha1.StorageCephRoleOSD {
			node.Devices = []string{"/dev/vdb"}
		}
	}
	return node
}

func opaqueSecret(name string) v1alpha1.Secret {
	return v1alpha1.Secret{Metadata: v1alpha1.Metadata{Name: name}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}}
}

func externalStorageValidationState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{}},
		Secrets:      []v1alpha1.Secret{opaqueSecret("shared-ceph-external-details")},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementExternal,
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
				ExternalDetails: &v1alpha1.StorageExportExternalDetailsSpec{
					FromSecretRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
				Readiness: v1alpha1.ClusterAddonReadiness{
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
							APIVersion: "v1",
							Kind:       "Namespace",
							Name:       "openshift-storage",
						},
					}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf"}},
				AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
					AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
					Inputs:   []v1alpha1.ClusterAddonBindingInput{dataFoundationBindingInput("export")},
				}},
			},
		}},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name:        "external-storage",
		ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
		}},
	}}}
}

func dataFoundationBindingInput(export string) v1alpha1.ClusterAddonBindingInput {
	return v1alpha1.ClusterAddonBindingInput{
		Name:  "external-storage",
		Value: export,
	}
}

func storagePoolSpec(cluster, role string) v1alpha1.StoragePoolSpec {
	return v1alpha1.StoragePoolSpec{
		StorageClusterRef: v1alpha1.LocalObjectReference{Name: cluster},
		Ceph: v1alpha1.StoragePoolCephSpec{
			Type:       v1alpha1.StoragePoolTypeReplicated,
			Role:       role,
			Replicated: v1alpha1.StorageCephPoolReplicas{Size: 4, MinSize: 2},
		},
	}
}
