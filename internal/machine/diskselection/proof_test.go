package diskselection

import (
	"bytes"
	"strings"
	"testing"
)

func binding() Binding {
	return Binding{Context: "example", Incarnation: "example-incarnation", Machine: "example-node", Operation: "op-" + strings.Repeat("a", 32), Attempt: "attempt-000001", Nonce: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), MediaSHA256: strings.Repeat("d", 64), ToolDigest: strings.Repeat("e", 64), UUID: "11111111-2222-3333-4444-555555555555", MACs: []string{"02:00:00:00:00:01"}}
}

func TestAuthenticatedReportProvesAnExactTarget(t *testing.T) {
	report := Report{Version: Version, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
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
	report := Report{Version: Version, Binding: original, Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
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
	report := Report{Version: Version, Binding: binding(), Disks: []Disk{disk("EXAMPLE-OS", 8192)}}
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
