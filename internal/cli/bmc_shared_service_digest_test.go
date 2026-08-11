package cli

import (
	"testing"

	"github.com/crmarques/bootwright/internal/ownership"
)

func bmcSharedServiceRecordFixture() ownership.ResourceRecord {
	name := "provider-a"
	contextName := "lab"
	stateRoot := "/srv/bootwright/provider-state/bmc/" + name
	vMediaRoot := "/var/lib/libvirt/images/bootwright/" + contextName + "/bmc/" + name + "/vmedia"
	claimPath := "/var/lib/bootwright/shared-services/bmc-emulator/" + name
	redfishUnit := "bootwright-sushy-" + name + ".service"
	vMediaUnit := "bootwright-vmedia-" + name + ".service"
	return ownership.ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       string(ownership.KindBMCEmulator),
		Name:       name,
		Owner:      ownership.Owner,
		Context:    contextName,
		Host:       "bastion",
		Provider:   name,
		Paths: []string{
			stateRoot,
			vMediaRoot,
			"/etc/systemd/system/" + redfishUnit,
			"/etc/systemd/system/" + vMediaUnit,
			claimPath,
		},
		Attributes: map[string]string{
			"redfishUnit": redfishUnit, "vMediaUnit": vMediaUnit,
			"redfishPort": "8000", "vMediaPort": "8001",
			"libvirtURI": "qemu:///system", "bindAddress": "127.0.0.1",
			"authPath": stateRoot + "/htpasswd", "pool": "bootwright-" + name + "-vmedia",
			"claimPath": claimPath, "stateRoot": stateRoot, "vMediaRoot": vMediaRoot,
			"firewallManaged": "true",
		},
	}
}

func TestBMCSharedServiceRecordDigestsMatchPhysicalConsequence(t *testing.T) {
	record := bmcSharedServiceRecordFixture()
	selectionDigest, claimDigest, err := bmcSharedServiceRecordDigests(record)
	if err != nil {
		t.Fatalf("bmcSharedServiceRecordDigests: %v", err)
	}
	if selectionDigest == "" || selectionDigest != claimDigest {
		t.Fatalf("record digests = %q/%q, want one nonempty exact digest", selectionDigest, claimDigest)
	}
	if want := "sha256:3428dc02ec67ca9c9787ccb49059c914fc821eeb20e5ad9d6c040222f6a80720"; selectionDigest != want {
		t.Fatalf("record digest = %q, want host-filter canonical digest %q", selectionDigest, want)
	}
	record.Attributes["firewallManaged"] = "false"
	selectionWithoutObservedFirewall, _, err := bmcSharedServiceRecordDigests(record)
	if err != nil {
		t.Fatalf("bmcSharedServiceRecordDigests without observed firewall: %v", err)
	}
	if selectionWithoutObservedFirewall != selectionDigest {
		t.Fatalf("observed firewall state changed desired physical digest: before=%q after=%q", selectionDigest, selectionWithoutObservedFirewall)
	}
	record.Attributes["redfishPort"] = "9000"
	changed, _, err := bmcSharedServiceRecordDigests(record)
	if err != nil {
		t.Fatalf("bmcSharedServiceRecordDigests changed port: %v", err)
	}
	if changed == selectionDigest {
		t.Fatalf("changed BMC port did not change physical consequence digest %q", changed)
	}
}

func TestBMCSharedServiceRecordDigestsRejectContradictoryEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*ownership.ResourceRecord){
		"foreign":   func(record *ownership.ResourceRecord) { record.Owner = "foreign" },
		"path":      func(record *ownership.ResourceRecord) { record.Paths[0] = "/tmp/foreign" },
		"port":      func(record *ownership.ResourceRecord) { record.Attributes["redfishPort"] = "08000" },
		"attribute": func(record *ownership.ResourceRecord) { record.Attributes["future"] = "unsafe" },
	} {
		t.Run(name, func(t *testing.T) {
			record := bmcSharedServiceRecordFixture()
			mutate(&record)
			if _, _, err := bmcSharedServiceRecordDigests(record); err == nil {
				t.Fatal("contradictory BMC record produced execution-authority digests")
			}
		})
	}
}
