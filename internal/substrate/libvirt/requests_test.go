package libvirt

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A provider host realized under the version before this one is removable here:
// the single daemon it froze reads back as the driver set this build carries,
// and nothing else about the host it realized changes.
func TestAFrozenHostRequestOfThePriorVersionUpgradesIntoThisOne(t *testing.T) {
	prior := hostRequestV1{
		Identity:    Identity{Block: HostBlockID("lab"), Context: "lab-rhel", Object: "lab"},
		Networks:    []Network{{Bridge: "virbr-lab", Name: "bootwright-lab-rhel-lab-guests"}},
		Packages:    HypervisorPackages(),
		PoolName:    "bootwright-lab-rhel-lab-vmedia",
		PoolPath:    "/var/lib/bootwright-services/lab-rhel/vmedia",
		Provisioned: true,
		Service:     "libvirtd.service",
		URI:         "qemu:///system",
		Version:     priorHostRequestVersion,
	}
	data, err := prior.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeHostRequest(data)
	if err != nil {
		t.Fatalf("the prior version was unreadable: %v", err)
	}
	if decoded.Version != hostRequestVersion {
		t.Fatalf("version = %q", decoded.Version)
	}
	if !slices.Equal(decoded.Services, []string{"libvirtd.service"}) {
		t.Fatalf("the upgrade substituted this build's daemons: %v", decoded.Services)
	}
	if decoded.URI != prior.URI || decoded.PoolPath != prior.PoolPath || !decoded.Provisioned {
		t.Fatalf("the upgrade changed the host it realized: %+v", decoded)
	}
	if len(decoded.Networks) != 1 || decoded.Networks[0].Bridge != "virbr-lab" {
		t.Fatalf("networks = %+v", decoded.Networks)
	}
	if _, err := decoded.Canonical(); err != nil {
		t.Fatalf("the upgraded request is not canonical: %v", err)
	}
}

// The builds between the driver-set change and its version bump froze this
// build's body under the prior label. Such a host is removable here: the
// drivers it froze read back unchanged, and only the label moves.
func TestAFrozenHostRequestOfThePriorLabelOverThisBodyReadsAsThisOne(t *testing.T) {
	frozen := HostRequest{
		Identity:    Identity{Block: HostBlockID("lab"), Context: "lab-rhel", Object: "lab"},
		Networks:    []Network{{Bridge: "virbr-lab", Managed: true, Name: "bootwright-lab-rhel-lab-guests"}},
		Packages:    HypervisorPackages(),
		PoolName:    "bootwright-lab-rhel-lab-vmedia",
		PoolPath:    "/var/lib/bootwright-services/lab-rhel/vmedia",
		Provisioned: true,
		Services:    ServiceUnits(),
		URI:         "qemu:///system",
		Version:     priorHostRequestVersion,
	}
	data, err := frozen.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeHostRequest(data)
	if err != nil {
		t.Fatalf("the prior label over this body was unreadable: %v", err)
	}
	if decoded.Version != hostRequestVersion {
		t.Fatalf("version = %q", decoded.Version)
	}
	if !slices.Equal(decoded.Services, ServiceUnits()) {
		t.Fatalf("the drivers it froze changed: %v", decoded.Services)
	}
	frozen.Version = hostRequestVersion
	if !slices.EqualFunc(decoded.Networks, frozen.Networks, func(a, b Network) bool { return a == b }) ||
		decoded.URI != frozen.URI || decoded.PoolPath != frozen.PoolPath || !decoded.Provisioned {
		t.Fatalf("the read changed the host it realized: %+v", decoded)
	}
	if _, err := decoded.Canonical(); err != nil {
		t.Fatalf("the read request is not canonical: %v", err)
	}
}

// A prior label over a body that is neither of the two it was frozen with
// refuses, whichever key it carries.
func TestAFrozenHostRequestOfThePriorLabelOverAnUnknownBodyRefuses(t *testing.T) {
	for _, data := range []string{
		`{"daemon":"libvirtd.service","version":"substrate-host-libvirt-v1"}`,
		`{"service":"libvirtd.service","services":["virtqemud.service"],"version":"substrate-host-libvirt-v1"}`,
	} {
		_, err := DecodeHostRequest([]byte(data))
		if err == nil {
			t.Fatalf("an unknown body was accepted: %s", data)
		}
		reported := diagnostics.Of(err)
		if len(reported) == 0 || !strings.Contains(reported[0].Message, "malformed") {
			t.Fatalf("the refusal did not say the body is unknown: %v", err)
		}
	}
}

// A version this build never wrote and no longer reads refuses, and says which.
func TestAFrozenHostRequestOlderThanOneVersionRefuses(t *testing.T) {
	_, err := DecodeHostRequest([]byte(`{"version":"substrate-host-libvirt-v0"}`))
	if err == nil {
		t.Fatal("an unreadable version was accepted")
	}
	reported := diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Message, "substrate-host-libvirt-v0") {
		t.Fatalf("the refusal did not name the version: %v", err)
	}
}
