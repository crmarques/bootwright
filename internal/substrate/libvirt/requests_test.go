package libvirt

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every version but the one this build writes refuses, and the refusal names
// it so the remedy is the executable that registered the operation.
func TestAFrozenHostRequestOfAnyOtherVersionRefuses(t *testing.T) {
	for _, version := range []string{"substrate-host-libvirt-v1", "substrate-host-libvirt-v3", "substrate-host-libvirt-v4", "substrate-host-libvirt-v5", "substrate-host-libvirt-v6", ""} {
		_, err := DecodeHostRequest([]byte(`{"version":"` + version + `"}`))
		if err == nil {
			t.Fatalf("version %q was accepted", version)
		}
		reported := diagnostics.Of(err)
		if len(reported) == 0 {
			t.Fatalf("the refusal carried no diagnostic: %v", err)
		}
		if version != "" && !strings.Contains(reported[0].Message, version) {
			t.Fatalf("the refusal did not name %q: %v", version, reported[0].Message)
		}
	}
}

// A machine request of any version but the one this build writes refuses by its
// version.
func TestAFrozenMachineRequestOfAnyOtherVersionRefuses(t *testing.T) {
	for _, version := range []string{"machine-libvirt-v1", "machine-libvirt-v2", "machine-libvirt-v4", "machine-libvirt-v5", "machine-libvirt-v6", "machine-libvirt-v7", "machine-libvirt-v8", ""} {
		_, err := DecodeMachineRequest([]byte(`{"version":"` + version + `"}`))
		if reported := diagnostics.Of(err); err == nil || len(reported) == 0 || !strings.Contains(reported[0].Message, "unsupported version") {
			t.Fatalf("version %q was not refused by its version: %v", version, err)
		}
	}
}

func TestAFrozenHostRequestOfThisVersionRoundTrips(t *testing.T) {
	frozen := HostRequest{
		Identity:    Identity{Block: HostBlockID("lab"), Context: "lab-rhel", Object: "lab"},
		Networks:    []Network{{Bridge: "virbr-lab", Managed: true, Name: "bootwright-lab-rhel-lab-guests"}},
		Packages:    HypervisorPackages(),
		PoolName:    "bootwright-lab-rhel-lab-vmedia",
		PoolPath:    "/var/lib/bootwright-hosted/lab-rhel/services/vmedia",
		Provisioned: true,
		Services:    ServiceUnits(),
		URI:         "qemu:///system",
		Version:     hostRequestVersion,
	}
	data, err := frozen.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeHostRequest(data)
	if err != nil {
		t.Fatalf("this build's own version was unreadable: %v", err)
	}
	reencoded, err := decoded.Canonical()
	if err != nil {
		t.Fatalf("the read request is not canonical: %v", err)
	}
	if string(reencoded) != string(data) {
		t.Fatalf("the read changed the host it realized:\n%s\n%s", data, reencoded)
	}
}

// A body carrying a key this shape does not declare refuses rather than being
// read as a neighbouring shape.
func TestAFrozenHostRequestWithAnUnknownKeyRefuses(t *testing.T) {
	_, err := DecodeHostRequest([]byte(`{"daemon":"libvirtd.service","version":"` + hostRequestVersion + `"}`))
	if err == nil {
		t.Fatal("an unknown body was accepted")
	}
	reported := diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Message, "malformed") {
		t.Fatalf("the refusal did not say the body is unknown: %v", err)
	}
}

// withAnUndeclaredField gives canonical bytes a member this build does not
// declare, as a field another version carried. It sorts first, so the bytes
// stay canonical and only the shape refuses them.
func withAnUndeclaredField(t *testing.T, canonical []byte) []byte {
	t.Helper()
	if !strings.HasPrefix(string(canonical), `{"`) {
		t.Fatalf("not a canonical object: %s", canonical)
	}
	return []byte(`{"anUndeclaredField":true,` + string(canonical[1:]))
}

func expectVersionRefusal(t *testing.T, err error, subject, version string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen "+subject+" request has an unsupported version: "+version ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("a %s request of another version refused as %+v", subject, reported)
	}
}

func TestAHostRequestOfAnotherVersionRefusesByItsVersion(t *testing.T) {
	other := HostRequest{
		Identity: Identity{Block: HostBlockID("lab"), Context: "lab-rhel", Object: "lab"},
		Networks: []Network{{Bridge: "virbr-lab", Managed: true, Name: "bootwright-lab-rhel-lab-guests"}},
		Packages: HypervisorPackages(), PoolName: "bootwright-lab-rhel-lab-vmedia",
		PoolPath: "/var/lib/bootwright-hosted/lab-rhel/services/vmedia", Provisioned: true,
		Services: ServiceUnits(), URI: "qemu:///system", Version: "substrate-host-libvirt-v1",
	}
	data, err := other.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeHostRequest(withAnUndeclaredField(t, data))
	expectVersionRefusal(t, err, "provider host", "substrate-host-libvirt-v1")
}

func TestAMachineRequestOfAnotherVersionRefusesByItsVersion(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	other := requests[0]
	other.Version = "machine-libvirt-v1"
	data, err := other.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeMachineRequest(withAnUndeclaredField(t, data))
	expectVersionRefusal(t, err, "machine", "machine-libvirt-v1")
}
