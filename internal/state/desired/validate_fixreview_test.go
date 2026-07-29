package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEntitlementSatelliteTrustBundleRefMustBeDeclared(t *testing.T) {
	state := func(trustRef string) v1alpha1.State {
		return v1alpha1.State{
			Secrets: []v1alpha1.Secret{
				{Metadata: v1alpha1.Metadata{Name: "redhat-org"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
				{Metadata: v1alpha1.Metadata{Name: "redhat-key"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
				{Metadata: v1alpha1.Metadata{Name: "satellite-ca"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
			},
			Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
			Entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-key"},
						Satellite: &v1alpha1.EntitlementRHSMSatellite{
							Hostname:       "satellite.example.com",
							TrustBundleRef: v1alpha1.SecretRef{Name: trustRef},
						},
					},
				},
			}},
		}
	}
	if errs := validateSecretReferences(state("missing-ca")); !containsSubstr(errs, "satellite.trustBundleRef") {
		t.Fatalf("undeclared satellite trustBundleRef must be rejected, got %v", errs)
	}
	if errs := validateSecretReferences(state("satellite-ca")); len(errs) != 0 {
		t.Fatalf("declared satellite trustBundleRef must pass, got %v", errs)
	}
}

func TestClusterAddonStepSecretRefsMustBeDeclared(t *testing.T) {
	state := func(ref string) v1alpha1.State {
		return v1alpha1.State{
			Secrets:      []v1alpha1.Secret{{Metadata: v1alpha1.Metadata{Name: "hook-secret"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}}},
			Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
			ClusterAddons: []v1alpha1.ClusterAddon{{
				Metadata: v1alpha1.Metadata{Name: "df"},
				Spec: v1alpha1.ClusterAddonSpec{
					Steps: []v1alpha1.ClusterAddonStep{{
						SecretRefs: []v1alpha1.SecretRef{{Name: ref}},
					}},
				},
			}},
		}
	}
	if errs := validateSecretReferences(state("missing")); !containsSubstr(errs, "steps[0].secretRefs[0]") {
		t.Fatalf("undeclared hook secretRef must be rejected, got %v", errs)
	}
	if errs := validateSecretReferences(state("hook-secret")); len(errs) != 0 {
		t.Fatalf("declared hook secretRef must pass, got %v", errs)
	}
}

func TestValidateOSDDevicesExcludeRootDiskPathSpecsAndFleet(t *testing.T) {
	pathSpec := func(path string) *v1alpha1.StorageCephDeviceSelection {
		return &v1alpha1.StorageCephDeviceSelection{PathSpecs: []v1alpha1.StorageCephDevicePath{{Path: path}}}
	}
	if errs := validateOSDDevicesExcludeRootDisk(osdNodeState("/dev/sda", nil, &v1alpha1.StorageCephNodeOSD{DataDevices: pathSpec("/dev/sda")})); len(errs) != 1 {
		t.Fatalf("root disk in osd.dataDevices.pathSpecs must refuse, got %v", errs)
	}
	if errs := validateOSDDevicesExcludeRootDisk(osdNodeState("/dev/sda", nil, &v1alpha1.StorageCephNodeOSD{DBDevices: pathSpec("/dev/sda")})); len(errs) != 1 {
		t.Fatalf("root disk in osd.dbDevices.pathSpecs must refuse, got %v", errs)
	}
	if errs := validateOSDDevicesExcludeRootDisk(osdFleetDrivegroupState("/dev/sda", "/dev/sda")); len(errs) != 1 {
		t.Fatalf("root disk in a covering fleet osdDrivegroup must refuse, got %v", errs)
	}
	if e := validateOSDDevicesExcludeRootDisk(osdFleetDrivegroupState("/dev/sda", "/dev/sdb")); len(e) != 0 {
		t.Fatalf("fleet drivegroup device distinct from root disk must pass, got %v", e)
	}
}

func osdFleetDrivegroupState(root, fleetDevice string) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	m.Spec.OS.Install.RootDeviceHints = &v1alpha1.RootDeviceHints{DeviceName: root}
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{m},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}},
					},
					OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
						ServiceID: "fleet",
						Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-0"}},
						OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{Paths: []string{fleetDevice}}},
					}},
				},
			}},
		}},
	}
}

func containsSubstr(errs []string, want string) bool {
	return strings.Contains(strings.Join(errs, "; "), want)
}
