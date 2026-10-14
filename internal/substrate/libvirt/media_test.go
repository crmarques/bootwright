package libvirt

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func mediaRequest(t *testing.T, disks ...api.Value) MachineRequest {
	t.Helper()
	profile := api.MapValue(text("name", "rhel"), number("cpu", "4"), number("memoryMiB", "8192"), number("diskGiB", "60"), field("dataDisks", api.ListValue(disks...)))
	p := provider(field("libvirt", provider().Spec().Get("libvirt").With("machineProfiles", api.ListValue(profile))))
	requests, err := MachineRequests(catalogOf(controller(), p, networkConfig(), guest("rhel-01")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	return requests[0]
}

func mediaDisk(name string, ssd bool) api.Value {
	value := api.MapValue(text("name", name), number("sizeGiB", "20"))
	if ssd {
		value = value.With("media", api.StringValue("ssd"))
	}
	return value
}

func TestMixedDiskMediaUsesIndependentStableTargetsAndKeepsRoot(t *testing.T) {
	request := mediaRequest(t, mediaDisk("ssd-a", true), mediaDisk("ordinary-a", false), mediaDisk("ssd-b", true), mediaDisk("ordinary-b", false))
	var targets []string
	for _, disk := range request.Disks {
		targets = append(targets, disk.Name+"="+disk.Target)
		if disk.Media == "ssd" && (disk.Bus != "scsi" || disk.RotationRate != 1) || disk.Media == "" && (disk.Bus != "" || disk.RotationRate != 0) {
			t.Fatal("media presentation changed an omitted disk or failed SSD admission")
		}
	}
	if !slices.Equal(targets, []string{"root=vda", "ssd-a=sda", "ordinary-a=vdb", "ssd-b=sdb", "ordinary-b=vdc"}) {
		t.Fatalf("targets=%v", targets)
	}
	first := request.Disks[2]
	withoutSSD := mediaRequest(t, mediaDisk("ordinary-a", false), mediaDisk("ordinary-b", false))
	if withoutSSD.Disks[1] != first {
		t.Fatal("adding an SSD changed the original ordinary disk identity or target")
	}
	if err := validateDiskMedia(request.Disks); err != nil {
		t.Fatal(err)
	}
}

func TestAllSSDAndMixedProfilesKeepTheExistingTotalDiskBound(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		disks := []api.Value{}
		for index := range substrate.MaxDataDisks {
			disks = append(disks, mediaDisk(fmt.Sprintf("data-%d", index), !mixed || index%2 == 0))
		}
		if len(mediaRequest(t, disks...).Disks) != substrate.MaxDataDisks+1 {
			t.Fatal("admitted data-disk bound changed")
		}
		profile := api.MapValue(number("diskGiB", "60"), field("dataDisks", api.ListValue(append(disks, mediaDisk("excess", true))...)))
		if _, err := disksFor(profile, "Machine/synthetic", testContext, "synthetic"); err == nil {
			t.Fatal("excess mixed/SSD disk passed the original total bound")
		}
	}
}

func TestMediaSelectionAndFrozenRequestsRefuseUnsupportedOrRootChanges(t *testing.T) {
	for _, disk := range []api.Value{mediaDisk("root", true), mediaDisk("data", true).With("media", api.StringValue("hdd")), mediaDisk("data", true).With("media", api.StringValue("")), mediaDisk("data", true).With("media", api.BoolValue(true))} {
		profile := api.MapValue(number("diskGiB", "60"), field("dataDisks", api.ListValue(disk)))
		if _, err := disksFor(profile, "Machine/synthetic", testContext, "synthetic"); err == nil {
			t.Fatal("unsupported media or root switch reached a request")
		}
	}
	request := mediaRequest(t, mediaDisk("data", true))
	for _, damage := range []string{"root", "bus", "target", "rate", "unknown", "no-media"} {
		changed := request
		changed.Disks = slices.Clone(request.Disks)
		switch damage {
		case "root":
			changed.Disks[0].Media, changed.Disks[0].Bus, changed.Disks[0].RotationRate = "ssd", "scsi", 1
		case "bus":
			changed.Disks[1].Bus = "virtio"
		case "target":
			changed.Disks[1].Target = "sdb"
		case "rate":
			changed.Disks[1].RotationRate = 0
		case "unknown":
			changed.Disks[1].Media = "hdd"
		case "no-media":
			changed.Disks[1].Media = ""
		}
		data, err := changed.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeMachineRequest(data); err == nil {
			t.Fatal("malformed frozen disk presentation was accepted")
		}
	}
}

func TestSSDPresenceRequiresObservedBusTargetAndNonrotationalRate(t *testing.T) {
	request := mediaRequest(t, mediaDisk("data", true))
	var evidence MachineEvidence
	if err := json.Unmarshal(machineEvidence(request, "digest"), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Disks[1].Bus, evidence.Disks[1].Target, evidence.Disks[1].RotationRate = "scsi", "sda", 1
	if err := ValidateMachinePresence(encode(t, evidence), request, "digest"); err != nil {
		t.Fatal(err)
	}
	for _, damage := range []string{"bus", "target", "rate", "missing"} {
		changed := evidence
		changed.Disks = slices.Clone(evidence.Disks)
		switch damage {
		case "bus":
			changed.Disks[1].Bus = "virtio"
		case "target":
			changed.Disks[1].Target = "sdb"
		case "rate":
			changed.Disks[1].RotationRate = 7200
		case "missing":
			changed.Disks[1].Bus, changed.Disks[1].Target, changed.Disks[1].RotationRate = "", "", 0
		}
		if err := ValidateMachinePresence(encode(t, changed), request, "digest"); err == nil {
			t.Fatal("SSD readiness accepted missing or conflicting native media proof")
		}
	}
}

func TestSSDRequestAndPresenceGolden(t *testing.T) {
	request := mediaRequest(t, mediaDisk("ssd", true), mediaDisk("ordinary", false))
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-machine-ssd", canonical)
	if _, err := DecodeMachineRequest(canonical); err != nil {
		t.Fatal(err)
	}
	var evidence MachineEvidence
	_ = json.Unmarshal(machineEvidence(request, evidenceDigest), &evidence)
	evidence.Disks[1].Bus, evidence.Disks[1].Target, evidence.Disks[1].RotationRate = "scsi", "sda", 1
	data, err := reconciliation.Freeze(evidence, "libvirt SSD presence")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "evidence-machine-ssd", data)
	if err := ValidateMachinePresence(data, request, evidenceDigest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "machine-content-digest", encode(t, map[string]string{"digest": MachineContentDigest()}))
	legacy := mediaRequest(t, mediaDisk("ordinary", false))
	data, _ = legacy.Canonical()
	for _, key := range []string{"bus", "media", "rotationRate"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatal("omitted media changed legacy request bytes")
		}
	}
}
