package support

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestLookupDispatchClassifiesApplySupport(t *testing.T) {
	tests := []struct {
		name      string
		dispatch  Dispatch
		want      Status
		supported bool
	}{
		{
			name:      "libvirt redfish lab",
			dispatch:  Dispatch{SubstrateRole: "libvirt", BMCRole: "emulated", BootRole: "redfish"},
			want:      StatusSupported,
			supported: true,
		},
		{
			name:      "bare metal redfish",
			dispatch:  Dispatch{SubstrateRole: "baremetal", BMCRole: "redfish", BootRole: "redfish"},
			want:      StatusSupported,
			supported: true,
		},
		{
			name:      "kubevirt",
			dispatch:  Dispatch{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"},
			want:      StatusSupported,
			supported: true,
		},
		{
			name:     "unknown dispatch",
			dispatch: Dispatch{SubstrateRole: "none", BMCRole: "none", BootRole: "none"},
			want:     StatusUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupDispatch(tt.dispatch.SubstrateRole, tt.dispatch.BMCRole, tt.dispatch.BootRole)
			if got.Status != tt.want {
				t.Fatalf("status = %s, want %s", got.Status, tt.want)
			}
			if got.ApplySupported() != tt.supported {
				t.Fatalf("ApplySupported = %t, want %t", got.ApplySupported(), tt.supported)
			}
			if got.Status != StatusUnknown && got.Roles.BootApplyRole == "" {
				t.Fatalf("registered dispatch has no boot apply role: %#v", got)
			}
		})
	}
}

func TestLookupProvisionerUsesRegistry(t *testing.T) {
	if got := LookupProfileProvisioner("libvirt"); got.Roles.SubstrateApplyRole != "bootwright.core.machine_substrate_libvirt" {
		t.Fatalf("libvirt substrate role = %q", got.Roles.SubstrateApplyRole)
	}
	if got := LookupMachineProvisioner("baremetal"); got.Roles.BootApplyRole != "bootwright.core.container_cluster_boot_redfish" {
		t.Fatalf("baremetal boot role = %q", got.Roles.BootApplyRole)
	}
}

func TestLookupServiceUsesRegistry(t *testing.T) {
	got := LookupService(v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)
	if got.ApplyRole != "bootwright.core.infra_component_name_resolution_dnsmasq" || got.DestroyRole != "bootwright.core.infra_component_name_resolution_dnsmasq" {
		t.Fatalf("dnsmasq roles = %#v", got)
	}
	if got.DefaultPort != v1alpha1.DefaultDNSPort {
		t.Fatalf("dnsmasq default port = %d, want %d", got.DefaultPort, v1alpha1.DefaultDNSPort)
	}
	ntp := LookupService(v1alpha1.ComponentSlotNTP, v1alpha1.InfraComponentTypeChrony)
	if ntp.ApplyRole != "bootwright.core.infra_component_ntp_chrony" || ntp.DestroyRole != "bootwright.core.infra_component_ntp_chrony" {
		t.Fatalf("chrony roles = %#v", ntp)
	}
	if ntp.DefaultPort != v1alpha1.DefaultNTPPort {
		t.Fatalf("chrony default port = %d, want %d", ntp.DefaultPort, v1alpha1.DefaultNTPPort)
	}
	bmc := LookupService(v1alpha1.ProviderServiceKindBMC, "emulated")
	if bmc.ApplyRole != "bootwright.core.provider_service_bmc_emulated" || bmc.DestroyRole != "bootwright.core.provider_service_bmc_emulated" {
		t.Fatalf("bmc roles = %#v", bmc)
	}
}

func TestEntriesAreSorted(t *testing.T) {
	entries := Entries()
	for i := 1; i < len(entries); i++ {
		prev, cur := entries[i-1].Dispatch, entries[i].Dispatch
		if prev.SubstrateRole > cur.SubstrateRole {
			t.Fatalf("entries are not sorted: %#v before %#v", prev, cur)
		}
	}
}

func TestServiceEntriesAreSorted(t *testing.T) {
	entries := ServiceEntries()
	for i := 1; i < len(entries); i++ {
		prev, cur := entries[i-1].Key, entries[i].Key
		if prev.Kind > cur.Kind {
			t.Fatalf("service entries are not sorted: %#v before %#v", prev, cur)
		}
	}
}
