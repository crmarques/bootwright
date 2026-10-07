package prerequisites

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every held key a wanted claim conflicts with is its own diagnostic, in the
// held claim's key order. Each names the holder, the held key for a socket, a
// bridge and a prefix but only the class of a path, the holder's object and
// this context's object, with the destroy that releases the key or the field
// that chooses it as its remedy.
func TestEachConflictingHeldClaimIsItsOwnDiagnostic(t *testing.T) {
	held := []HostReservation{{Context: "alpha", Kind: "substrate-host", Service: "alpha-libvirt", Keys: []string{
		"bridge:virbr-lab", "path:/var/lib/libvirt/images/bootwright/alpha/vmedia", "prefix:198.51.100.0/24", "socket:0.0.0.0:8000",
	}}}
	wanted := []HostReservation{
		{Context: "lab", Kind: "substrate-machine", Service: "rhel-01", Keys: []string{"socket:192.0.2.1:8000"}},
		{Context: "lab", Kind: "substrate-host", Service: "lab-libvirt", Keys: []string{
			"bridge:virbr-lab", "path:/var/lib/libvirt/images/bootwright/alpha/vmedia", "prefix:198.51.100.128/25",
		}},
	}
	machine := &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Machine", Name: "rhel-01"}
	provider := &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "InfraProvider", Name: "lab-libvirt"}
	want := []diagnostics.Diagnostic{
		{Severity: "error", Code: "controller.conflict", Object: provider,
			Message:     "context alpha holds bridge virbr-lab for InfraProvider/alpha-libvirt, which conflicts with bridge virbr-lab that InfraProvider/lab-libvirt of this context claims",
			Remediation: "run bootwright destroy --context alpha first, or change spec.networkAttachments[].libvirt.bridge on InfraProvider/lab-libvirt"},
		{Severity: "error", Code: "controller.conflict", Object: provider,
			Message:     "context alpha holds a path reservation for InfraProvider/alpha-libvirt, which conflicts with the same path reservation that InfraProvider/lab-libvirt of this context claims",
			Remediation: "run bootwright destroy --context alpha first"},
		{Severity: "error", Code: "controller.conflict", Object: provider,
			Message:     "context alpha holds managed network prefix 198.51.100.0/24 for InfraProvider/alpha-libvirt, which conflicts with managed network prefix 198.51.100.128/25 that InfraProvider/lab-libvirt of this context claims",
			Remediation: "run bootwright destroy --context alpha first, or change spec.networkAttachments[].libvirt.address on InfraProvider/lab-libvirt"},
		{Severity: "error", Code: "controller.conflict", Object: machine,
			Message:     "context alpha holds socket 0.0.0.0:8000 for InfraProvider/alpha-libvirt, which conflicts with socket 192.0.2.1:8000 that Machine/rhel-01 of this context claims",
			Remediation: "run bootwright destroy --context alpha first, or change spec.libvirt.bmcEmulationDefaults.bindAddress or spec.libvirt.bmcEmulationDefaults.port on the InfraProvider of Machine/rhel-01"},
	}
	conflicts := Conflicts(held, wanted)
	if len(conflicts) != len(want) {
		t.Fatalf("conflicts = %+v, want %d", conflicts, len(want))
	}
	for index, conflict := range conflicts {
		got := ConflictDiagnostic(conflict)
		if got.Severity != want[index].Severity || got.Code != want[index].Code || got.Message != want[index].Message ||
			got.Remediation != want[index].Remediation || got.Object == nil || *got.Object != *want[index].Object {
			t.Fatalf("diagnostic %d = %+v (object %+v), want %+v (object %+v)", index, got, got.Object, want[index], want[index].Object)
		}
	}
}

// Two contexts that declare one physical server conflict over its controller,
// and the refusal names both Machines and the field that chooses the key,
// never the private endpoint.
func TestAPhysicalServerConflictNamesBothMachinesAndTheBMCField(t *testing.T) {
	key := "bmc:bmc.example.test:443/redfish/v1/Systems/1"
	held := []HostReservation{{Context: "alpha", Kind: "substrate-physical", Service: "node-a", Keys: []string{key}}}
	wanted := []HostReservation{{Context: "lab", Kind: "substrate-physical", Service: "node-b", Keys: []string{key}}}
	conflicts := Conflicts(held, wanted)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	got := ConflictDiagnostic(conflicts[0])
	message := "context alpha holds a bmc reservation for Machine/node-a, which conflicts with the same bmc reservation that Machine/node-b of this context claims"
	remediation := "run bootwright destroy --context alpha first, or change spec.hardware.management.bmc.address on Machine/node-b"
	if got.Message != message || got.Remediation != remediation || got.Object == nil || got.Object.Kind != "Machine" || got.Object.Name != "node-b" {
		t.Fatalf("diagnostic = %+v", got)
	}
}

// A service claim names its own bind fields, and a claim kind no API kind
// names is printed by its kind with no object.
func TestAConflictRemedyNamesTheFieldThatChoosesTheKey(t *testing.T) {
	for _, test := range []struct {
		kind, object, remediation string
		named                     bool
	}{
		{"proxy", "Proxy/b", "run bootwright destroy --context alpha first, or change spec.bindAddress or spec.port on Proxy/b", true},
		{"dns", "DNSServer/b", "run bootwright destroy --context alpha first, or change spec.bindAddress or spec.port on DNSServer/b", true},
		{"ntp", "NTPServer/b", "run bootwright destroy --context alpha first, or change spec.bindAddress or spec.port on NTPServer/b", true},
		{"artifact-server", "ArtifactServer/b", "run bootwright destroy --context alpha first, or change spec.bindAddress or a spec.listeners[].port on ArtifactServer/b", true},
		{"os-install", "Machine/b", "run bootwright destroy --context alpha first", true},
		{"cluster-media", "ContainerCluster/b", "run bootwright destroy --context alpha first", true},
		{"substrate-physical", "Machine/b", "run bootwright destroy --context alpha first", true},
		{"unnamed-kind", "unnamed-kind b", "run bootwright destroy --context alpha first", false},
	} {
		t.Run(test.kind, func(t *testing.T) {
			held := []HostReservation{{Context: "alpha", Kind: "proxy", Service: "a", Keys: []string{"socket:fd00::1:3128"}}}
			wanted := []HostReservation{{Context: "lab", Kind: test.kind, Service: "b", Keys: []string{"socket::::3128"}}}
			conflicts := Conflicts(held, wanted)
			if len(conflicts) != 1 {
				t.Fatalf("conflicts = %+v", conflicts)
			}
			got := ConflictDiagnostic(conflicts[0])
			message := "context alpha holds socket [fd00::1]:3128 for Proxy/a, which conflicts with socket [::]:3128 that " + test.object + " of this context claims"
			if got.Message != message || got.Remediation != test.remediation || (got.Object != nil) != test.named {
				t.Fatalf("diagnostic = %+v, want %q and %q", got, message, test.remediation)
			}
		})
	}
}

func TestManagedPrefixesConflictWhenTheyOverlap(t *testing.T) {
	for name, test := range map[string]struct {
		held, wanted string
		shared       bool
		conflict     bool
	}{
		"identical IPv4":         {held: "prefix:198.51.100.0/24", wanted: "prefix:198.51.100.0/24", conflict: true},
		"a nested IPv4 prefix":   {held: "prefix:198.51.100.0/24", wanted: "prefix:198.51.100.128/25", conflict: true},
		"an enclosing IPv4":      {held: "prefix:198.51.100.128/25", wanted: "prefix:198.51.100.0/24", conflict: true},
		"identical IPv6":         {held: "prefix:fd00:1::/64", wanted: "prefix:fd00:1::/64", conflict: true},
		"a nested IPv6 prefix":   {held: "prefix:fd00:1::/64", wanted: "prefix:fd00:1::/48", conflict: true},
		"disjoint IPv4":          {held: "prefix:198.51.100.0/25", wanted: "prefix:198.51.100.128/25"},
		"disjoint IPv6":          {held: "prefix:fd00:1::/64", wanted: "prefix:fd00:2::/64"},
		"IPv4 beside IPv6":       {held: "prefix:0.0.0.0/8", wanted: "prefix:::/8"},
		"a shared nested prefix": {held: "prefix:198.51.100.0/24", wanted: "prefix:198.51.100.128/25", shared: true},
		"a prefix beside a path": {held: "prefix:198.51.100.0/24", wanted: "path:198.51.100.0/24"},
		"an unreadable prefix":   {held: "prefix:198.51.100.0/24", wanted: "prefix:198.51.100.0/33"},
	} {
		t.Run(name, func(t *testing.T) {
			held := []HostReservation{{Context: "alpha", Kind: "substrate-host", Service: "a", Keys: []string{test.held}, Shared: test.shared}}
			wanted := []HostReservation{{Context: "lab", Kind: "substrate-host", Service: "b", Keys: []string{test.wanted}}}
			conflicts := Conflicts(held, wanted)
			if len(conflicts) > 0 != test.conflict {
				t.Fatalf("conflicts = %+v, want %t", conflicts, test.conflict)
			}
			if !test.conflict {
				return
			}
			message := ConflictDiagnostic(conflicts[0]).Message
			want := "context alpha holds managed network prefix " + test.held[len("prefix:"):] + " for InfraProvider/a, which conflicts with managed network prefix " +
				test.wanted[len("prefix:"):] + " that InfraProvider/b of this context claims"
			if message != want {
				t.Fatalf("message = %q, want %q", message, want)
			}
		})
	}
}
