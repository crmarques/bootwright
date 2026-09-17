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
