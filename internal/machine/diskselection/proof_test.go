package diskselection

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func binding() Binding {
	return Binding{Context: "example", Incarnation: "example-incarnation", Machine: "example-node", Operation: "op-" + strings.Repeat("a", 32), Attempt: "attempt-000001", Nonce: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), MediaSHA256: strings.Repeat("d", 64), ToolDigest: strings.Repeat("e", 64), UUID: "11111111-2222-3333-4444-555555555555", MACs: []string{"02:00:00:00:00:01"}}
}

func TestAuthenticatedReportProvesAnExactTarget(t *testing.T) {
	report := Report{Version: Version, Source: SourceLinux, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
	data, err := reportWire(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := Prove(decoded, binding(), Policy{}, nil)
	if err != nil || proof.Selected != report.Disks[0].DiskIdentity || len(proof.ReportDigest) != 64 {
		t.Fatalf("proof %#v: %v", proof, err)
	}
}

func TestEveryReportBindingPreventsReplay(t *testing.T) {
	original := binding()
	report := Report{Version: Version, Source: SourceLinux, Binding: original, Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
	for _, change := range []func(*Binding){
		func(b *Binding) { b.Context = "other" }, func(b *Binding) { b.Incarnation = "other" }, func(b *Binding) { b.Machine = "other" },
		func(b *Binding) { b.Operation = "op-" + strings.Repeat("f", 32) }, func(b *Binding) { b.Attempt = "attempt-000002" },
		func(b *Binding) { b.Nonce = strings.Repeat("f", 64) }, func(b *Binding) { b.RequestDigest = strings.Repeat("f", 64) },
		func(b *Binding) { b.MediaSHA256 = strings.Repeat("f", 64) }, func(b *Binding) { b.ToolDigest = strings.Repeat("f", 64) },
		func(b *Binding) { b.UUID = "22222222-3333-4444-5555-666666666666" }, func(b *Binding) { b.MACs = []string{"02:00:00:00:00:02"} },
	} {
		expected := original
		change(&expected)
		if _, err := Prove(report, expected, Policy{}, nil); err == nil {
			t.Fatal("foreign or stale report accepted")
		}
	}
}

func TestNativeReportRefusesDuplicateUnknownOrCaseVariantFields(t *testing.T) {
	report := Report{Version: Version, Source: SourceLinux, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
	data, _ := reportWire(report)
	for _, bad := range [][]byte{
		append([]byte(`{"extra":true,`), data[1:]...),
		bytes.Replace(data, []byte(`"version":`), []byte(`"version":"wrong","version":`), 1),
		bytes.Replace(data, []byte(`"version":`), []byte(`"Version":`), 1),
		append(append([]byte{}, data...), '\n'),
		[]byte(strings.Repeat(" ", MaxReportBytes+1)),
	} {
		if _, err := DecodeReport(bad); err == nil {
			t.Fatal("noncanonical native report accepted")
		}
	}
}

func TestReportNamesItsInventorySource(t *testing.T) {
	for _, source := range []string{SourceLinux, SourceBMC} {
		report := Report{Version: Version, Source: source, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
		data, _ := reportWire(report)
		if _, err := DecodeReport(data); err != nil {
			t.Fatalf("%s report refused: %v", source, err)
		}
	}
	for _, source := range []string{"", "redfish", "Linux"} {
		report := Report{Version: Version, Source: source, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
		data, _ := reportWire(report)
		if _, err := DecodeReport(data); err == nil {
			t.Fatalf("report with source %q accepted", source)
		}
	}
}

func TestBMCReportRefusesSelectorsItCannotAnswer(t *testing.T) {
	report := Report{Version: Version, Source: SourceBMC, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192), disk("EXAMPLE-DATA", 16384)}}
	serialClaim := []DiskIdentity{{SerialNumber: "EXAMPLE-DATA", SizeBytes: 16384}}
	proof, err := Prove(report, binding(), Policy{}, serialClaim)
	if err != nil || proof.Selected.SerialNumber != "EXAMPLE-OS" || !slices.Equal(proof.Inventoried, []string{"EXAMPLE-DATA", "EXAMPLE-OS"}) {
		t.Fatalf("serial-named claim against a BMC inventory: %+v %v", proof, err)
	}
	for _, bad := range [][]string{{"EXAMPLE-OS", "EXAMPLE-DATA"}, {"EXAMPLE-DATA"}, {"EXAMPLE-OS", "EXAMPLE-OS"}, {"bad serial", "EXAMPLE-OS"}} {
		forged := proof
		forged.Inventoried = bad
		if forged.Validate() == nil {
			t.Fatalf("an invalid BMC serial set validated: %v", bad)
		}
	}
	for name, c := range map[string]struct {
		policy Policy
		claims []DiskIdentity
	}{
		"WWN claim":            {Policy{}, []DiskIdentity{{WWN: "0x5000000000000002"}}},
		"WWN and serial claim": {Policy{}, []DiskIdentity{{WWN: "0x5000000000000002", SerialNumber: "EXAMPLE-DATA", SizeBytes: 16384}}},
		"WWN exclusion":        {Policy{Exclude: []DiskIdentity{{WWN: "0x5000000000000002", SizeBytes: 16384}}}, serialClaim},
		"controller predicate": {Policy{Match: Match{ControllerPCIAddress: "0000:01:00.0"}}, serialClaim},
	} {
		if _, err := Prove(report, binding(), c.policy, c.claims); err == nil || !strings.Contains(err.Error(), "inventory to boot") {
			t.Fatalf("%s: BMC inventory answered a selector it cannot see: %v", name, err)
		}
	}
	linux := report
	linux.Source = SourceLinux
	if _, err := Prove(linux, binding(), Policy{Exclude: []DiskIdentity{{WWN: "0x5000000000000009", SizeBytes: 16384}}}, serialClaim); err != nil {
		t.Fatalf("a Linux inventory refused a WWN exclusion: %v", err)
	}
}
