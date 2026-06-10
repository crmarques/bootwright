package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageStretchValidationAcceptsCanonicalShape(t *testing.T) {
	if errs := validateStorage(storageValidationState()); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
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
				state.StorageClusters[0].Spec.Ceph.Release = "reef"
			},
		},
		{
			name: "oss-image-tag",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = "quay.io/ceph/ceph:v19.2.1"
			},
		},
		{
			name: "oss-image-digest",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = "quay.io/ceph/ceph@sha256:" + strings.Repeat("a", 64)
			},
		},
		{
			name: "redhat-stream-and-image",
			edit: func(state *v1alpha1.State) {
				state.Environments = []v1alpha1.Environment{{
					Metadata: v1alpha1.Metadata{Name: "env"},
					Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{{
						Name:     "ceph-entitlement",
						Provider: v1alpha1.EntitlementProviderRedHat,
						Product:  v1alpha1.EntitlementProductCeph,
					}}},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "9"
				state.StorageClusters[0].Spec.Ceph.Image = "registry.redhat.io/rhceph/rhceph-9-rhel9:9"
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
			want: "field clusterSSH not found",
		},
		{
			name: "ssh-execution-known-hosts",
			body: `apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata: { name: export }
spec:
  type: data-foundation
  storageClusterRef: ceph
  externalDetails:
    sshExecution:
      knownHostsRef: removed-known-hosts
`,
			want: "field knownHostsRef not found",
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

// TestStorageCephRoleVocabularyAuthorableFromYAML guards the one role
// vocabulary end-to-end: an authored host carrying a monitoring role plus the
// matching monitoring service block must pass validation (placement derives
// from the role), and a role outside v1alpha1.StorageCephRoles() must still
// be rejected with the full vocabulary in the error.
func TestStorageCephRoleVocabularyAuthorableFromYAML(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  baseDomain: bootwright.test
  secrets:
    - ceph-node-ssh:
        generated:
          sshKeyPair:
            comment: bootwright-ceph-node
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
      keyRef: ceph-node-ssh
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
      bootstrap:
        host: ceph-0
    monitoring:
      prometheus:
        retentionTime: 15d
    topology:
      hosts:
        - machineRef: ceph-0
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

func TestStorageStretchValidationRejectsInvalidRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "tiebreaker-not-mon-only",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Hosts[6].Roles = append(state.StorageClusters[0].Spec.Ceph.Topology.Hosts[6].Roles, v1alpha1.StorageCephRoleMGR)
			},
			want: `tiebreaker.node "ceph-arbiter" must be mon-only`,
		},
		{
			name: "bad-data-site-mon-count",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Hosts[1].Roles = []string{v1alpha1.StorageCephRoleOSD}
			},
			want: `requires exactly two mon nodes in data site "dc1"`,
		},
		{
			name: "bad-stretch-replicas",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Stretch.ReplicatedPoolDefaults.Size = 3
			},
			want: "replicatedPoolDefaults must set size: 4 and minSize: 2",
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
			name: "redhat-wrong-entitlement-provider",
			edit: func(state *v1alpha1.State) {
				state.Environments = []v1alpha1.Environment{{
					Metadata: v1alpha1.Metadata{Name: "env"},
					Spec: v1alpha1.EnvironmentSpec{
						Entitlements: []v1alpha1.EnvironmentEntitlement{{
							Name:     "ceph-entitlement",
							Provider: v1alpha1.EntitlementProviderIBM,
							Product:  v1alpha1.EntitlementProductIBMStorageCeph,
						}},
					},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
			},
			want: `resolves to provider "ibm", want "redhat"`,
		},
		{
			name: "community-on-non-oss",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.Environments = []v1alpha1.Environment{{
					Metadata: v1alpha1.Metadata{Name: "env"},
					Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{{
						Name:     "ceph-entitlement",
						Provider: v1alpha1.EntitlementProviderRedHat,
						Product:  v1alpha1.EntitlementProductCeph,
					}}},
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
			name: "release-bad-redhat-stream",
			edit: func(state *v1alpha1.State) {
				state.Environments = []v1alpha1.Environment{{
					Metadata: v1alpha1.Metadata{Name: "env"},
					Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{{
						Name:     "ceph-entitlement",
						Provider: v1alpha1.EntitlementProviderRedHat,
						Product:  v1alpha1.EntitlementProductCeph,
					}}},
				}}
				state.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
				state.StorageClusters[0].Spec.Ceph.EntitlementRef.Name = "ceph-entitlement"
				state.StorageClusters[0].Spec.Ceph.Release = "squid"
			},
			want: `spec.ceph.release "squid" must be a product stream version such as 9 or 9.1`,
		},
		{
			name: "image-mutable-latest",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = "quay.io/ceph/ceph:latest"
			},
			want: `spec.ceph.image "quay.io/ceph/ceph:latest" must not use mutable :latest tag`,
		},
		{
			name: "image-unpinned",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Image = "quay.io/ceph/ceph"
			},
			want: `spec.ceph.image "quay.io/ceph/ceph" must pin a version tag or digest`,
		},
		{
			name: "community-bad-mirror",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Community = &v1alpha1.StorageCephCommunitySpec{Mirror: "ftp://mirror.example.test/ceph"}
			},
			want: "spec.ceph.community.mirror \"ftp://mirror.example.test/ceph\" must be an http or https URL",
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

// TestStorageCephHostOSDDeviceSelectionExplicit covers F12/F43/F48: OSD
// device consumption is explicit opt-in — an osd-role host must author
// devices or osd.dataDevices (all-devices is the explicit
// osd: {dataDevices: {all: true}}), and devices requires the osd role
// exactly like the drivegroup-shaped osd block.
func TestStorageCephHostOSDDeviceSelectionExplicit(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "osd-role-without-device-selection",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Hosts[2].Devices = nil
			},
			want: `spec.ceph.topology.hosts[2] carries the "osd" role but selects no devices; author devices or osd.dataDevices (osd: {dataDevices: {all: true}} consumes all available devices)`,
		},
		{
			name: "devices-without-osd-role",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Hosts[6].Devices = []string{"/dev/vdb"}
			},
			want: `spec.ceph.topology.hosts[6].devices requires the "osd" role`,
		},
		{
			name: "explicit-all-devices-passes",
			edit: func(state *v1alpha1.State) {
				host := &state.StorageClusters[0].Spec.Ceph.Topology.Hosts[2]
				host.Devices = nil
				host.OSD = &v1alpha1.StorageCephHostOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
				}
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

// TestStorageFilesystemMDSPlacementValidated covers F13: spec.cephfs.mds.placement
// goes through validateStoragePlacementHosts like every sibling placement, so a
// dangling host and a topology with no mds-capable host fail validate instead of
// silently rendering a filesystem with zero MDS daemons.
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
			want: `spec.cephfs.mds.placement.hosts[0] "ceph-typo" is not listed in StorageCluster/ceph spec.ceph.topology.nodes`,
		},
		{
			name: "no-mds-role-anywhere",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.MDS.Placement = v1alpha1.StoragePlacement{}
				hosts := state.StorageClusters[0].Spec.Ceph.Topology.Hosts
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
			want: `spec.cephfs.mds.placement resolves to no hosts: no StorageCluster/ceph spec.ceph.topology.hosts[] entry carries role "mds" within the selection`,
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

// TestStorageFilesystemMDSRolePlacementPasses confirms the default role-derived
// placement still validates on a topology whose hosts carry the mds role.
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
	if !strings.Contains(got, `requires ClusterAddon/odf to provide "data-foundation"`) {
		t.Fatalf("validateStorage errors = %q, want data-foundation provider error", got)
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
		t.Fatalf("storage export type = %q, want data-foundation", state.StorageExports[0].Spec.Type)
	}
	if state.StorageExports[0].Spec.ExternalDetails == nil || state.StorageExports[0].Spec.ExternalDetails.Generated == nil {
		t.Fatalf("storage export externalDetails = %#v, want generated default", state.StorageExports[0].Spec.ExternalDetails)
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
  baseDomain: bootwright.test
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

func TestExternalStorageValidationRequiresExternalDetailsSource(t *testing.T) {
	state := externalStorageValidationState()
	state.StorageExports[0].Spec.ExternalDetails = nil
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "spec.externalDetails is required when storageClusterRef points to external Ceph") {
		t.Fatalf("validateStorage errors = %q, want externalDetails requirement", got)
	}
}

func TestStorageValidationAcceptsManagedGeneratedExternalDetailsDefault(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Management = v1alpha1.StorageClusterManagementManaged
	state.StorageExports[0].Spec.ExternalDetails = nil
	Normalize(&state)
	if state.StorageExports[0].Spec.ExternalDetails == nil || state.StorageExports[0].Spec.ExternalDetails.Generated == nil {
		t.Fatalf("managed externalDetails = %#v, want generated default", state.StorageExports[0].Spec.ExternalDetails)
	}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestExternalStorageValidationAcceptsSSHExecution(t *testing.T) {
	state := externalStorageValidationState()
	state.Environments[0].Spec.Secrets["ceph-admin-known-hosts"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["ceph-admin-ssh"] = v1alpha1.EnvironmentSecretSpec{}
	state.Machines = []v1alpha1.Machine{storageValidationAdminMachine("ceph-admin-01")}
	state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
		SSHExecution: &v1alpha1.StorageExportExternalDetailsSSHExecution{
			MachineRefs: []v1alpha1.LocalObjectReference{{Name: "ceph-admin-01"}},
			Timeout:     "10m",
			Exporter: v1alpha1.StorageExportExternalDetailsExporter{
				Source: v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon,
			},
			Config: v1alpha1.StorageExportExternalDetailsExporterConfig{
				RBDDataPoolName: "rbdpool",
			},
		},
	}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestManagedStorageValidationAcceptsSSHExecutionWithoutHostRefs(t *testing.T) {
	state := storageValidationState()
	state.Environments = []v1alpha1.Environment{{
		Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
			"ceph-admin-known-hosts": {},
		}},
	}}
	state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
		SSHExecution: &v1alpha1.StorageExportExternalDetailsSSHExecution{
			Exporter: v1alpha1.StorageExportExternalDetailsExporter{
				Source: v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon,
			},
			Config: v1alpha1.StorageExportExternalDetailsExporterConfig{
				RBDDataPoolName: "rbdpool",
			},
		},
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
				state.StorageClusters[0].Spec.Ceph.Topology.Hosts[0].MachineRef = v1alpha1.LocalObjectReference{}
			},
			want: "spec.ceph.topology.hosts[0].machineRef is required",
		},
		{
			name: "missing-machine-ssh",
			edit: func(state *v1alpha1.State) {
				state.Machines[0].Spec.Access.SSH = nil
			},
			want: "Machine/ceph-dc1-0 spec.access.ssh is required",
		},
		{
			name: "missing-ceph-node-capability",
			edit: func(state *v1alpha1.State) {
				state.Machines[0].Spec.Capabilities = []string{v1alpha1.MachineCapabilityLibvirt}
			},
			want: `lacks capability "ceph-node"`,
		},
		{
			name: "mixed-users",
			edit: func(state *v1alpha1.State) {
				state.Machines[1].Spec.Access.SSH.User = "ceph"
			},
			want: `with ssh.user "ceph"; all storage node Machines in one StorageCluster must use "root"`,
		},
		{
			name: "mixed-key-refs",
			edit: func(state *v1alpha1.State) {
				state.Machines[1].Spec.Access.SSH.KeyRef.Name = "other-ceph-node-ssh"
			},
			want: `with ssh.keyRef "other-ceph-node-ssh"; all storage node Machines in one StorageCluster must use "ceph-node-ssh"`,
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
			want: `values.exportRef "export" must reference a data-foundation StorageExport`,
		},
		{
			name: "external-ceph-spec",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
			},
			want: "ceph must be empty when spec.management=external",
		},
		{
			name: "external-generated-details",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{Generated: &v1alpha1.StorageExportExternalDetailsGenerated{}}
			},
			want: "spec.externalDetails.generated must be empty when storageClusterRef points to external Ceph",
		},
		{
			name: "multiple-external-details-sources",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.ExternalDetails.Generated = &v1alpha1.StorageExportExternalDetailsGenerated{}
			},
			want: "spec.externalDetails must set exactly one of fromSecret, generated, or sshExecution",
		},
		{
			name: "external-ssh-host-without-ceph-admin-capability",
			edit: func(state *v1alpha1.State) {
				state.Environments[0].Spec.Secrets["ceph-admin-known-hosts"] = v1alpha1.EnvironmentSecretSpec{}
				state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
					SSHExecution: &v1alpha1.StorageExportExternalDetailsSSHExecution{
						MachineRefs: []v1alpha1.LocalObjectReference{{Name: "ceph-admin-01"}},
						Exporter: v1alpha1.StorageExportExternalDetailsExporter{
							Source: v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon,
						},
						Config: v1alpha1.StorageExportExternalDetailsExporterConfig{RBDDataPoolName: "rbdpool"},
					},
				}
				machine := storageValidationAdminMachine("ceph-admin-01")
				machine.Spec.Capabilities = []string{v1alpha1.MachineCapabilityLibvirt}
				state.Machines = []v1alpha1.Machine{machine}
			},
			want: `must reference a Machine with capability "ceph-admin"`,
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
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Host: "ceph-dc1-0",
						},
					},
					Topology: v1alpha1.StorageCephTopology{
						Stretch: &v1alpha1.StorageCephStretch{
							FailureDomain: "datacenter",
							DataSites:     []string{"dc1", "dc2"},
							Tiebreaker: v1alpha1.StorageCephTiebreaker{
								Site: "dc3",
								Host: "ceph-arbiter",
							},
							ReplicatedPoolDefaults: v1alpha1.StorageCephPoolReplicas{Size: 4, MinSize: 2},
							RuleName:               "stretch-replicated",
						},
						Hosts: []v1alpha1.StorageCephHost{
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
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{Type: v1alpha1.ClusterAddonReadinessResourceExists}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("export")},
			},
		}},
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
					KeyRef:        v1alpha1.SecretRef{Name: "ceph-node-ssh"},
					KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-known-hosts"},
				},
			},
		},
	}
}

func storageValidationAdminMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephAdmin},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: name + ".example.test"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef:    v1alpha1.LocalObjectReference{Name: "ssh"},
					User:          "ceph",
					KeyRef:        v1alpha1.SecretRef{Name: "ceph-admin-ssh"},
					KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-admin-known-hosts"},
				},
			},
		},
	}
}

func storageValidationCephNode(name, site string, roles []string) v1alpha1.StorageCephHost {
	node := v1alpha1.StorageCephHost{
		Hostname: name,
		MachineRef: v1alpha1.LocalObjectReference{
			Name: name,
		},
		Site:  site,
		Roles: roles,
	}
	// An osd-role host must select devices explicitly; there is no
	// all-devices omission default.
	for _, role := range roles {
		if role == v1alpha1.StorageCephRoleOSD {
			node.Devices = []string{"/dev/vdb"}
		}
	}
	return node
}

func externalStorageValidationState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"shared-ceph-external-details": {},
			}},
		}},
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
					FromSecret: "shared-ceph-external-details",
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
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{Type: v1alpha1.ClusterAddonReadinessResourceExists}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("export")},
			},
		}},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef": {RefKind: v1alpha1.KindStorageExport},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export string) v1alpha1.ClusterAddonBindingAddon {
	values := map[string]any{
		"exportRef": export,
	}
	return v1alpha1.ClusterAddonBindingAddon{
		Name: "odf",
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: values,
		}},
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
