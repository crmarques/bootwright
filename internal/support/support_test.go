package support

import "testing"

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
			name:     "kubevirt scaffold",
			dispatch: Dispatch{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"},
			want:     StatusScaffold,
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
	if got := LookupProfileProvisioner("libvirt"); got.Roles.SubstrateApplyRole != "substrate_libvirt" {
		t.Fatalf("libvirt substrate role = %q", got.Roles.SubstrateApplyRole)
	}
	if got := LookupMachineProvisioner("baremetal"); got.Roles.BootApplyRole != "boot_redfish" {
		t.Fatalf("baremetal boot role = %q", got.Roles.BootApplyRole)
	}
}

func TestLookupServiceUsesRegistry(t *testing.T) {
	got := LookupService("nameResolution", "dnsmasq")
	if got.ApplyRole != "dns_dnsmasq" || got.DestroyRole != "dns_dnsmasq" {
		t.Fatalf("dnsmasq roles = %#v", got)
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
