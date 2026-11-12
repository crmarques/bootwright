package diskselection

import (
	"strings"
	"testing"
)

func disk(serial string, size int64) Disk {
	return Disk{DiskIdentity: DiskIdentity{SerialNumber: serial, SizeBytes: size}, Model: "EXAMPLE BOOT", ControllerPCIAddress: "0000:01:00.0", Transport: "scsi", Fixed: true, Writable: true, Whole: true}
}

func TestSelectRequiresUniqueExactEligibleIdentity(t *testing.T) {
	root, data := disk("EXAMPLE-OS", 8192), disk("EXAMPLE-DATA", 16384)
	selected, err := Select(Policy{}, []Disk{root, data}, []DiskIdentity{data.DiskIdentity})
	if err != nil || selected != root.DiskIdentity {
		t.Fatalf("selected %#v: %v", selected, err)
	}
	for _, test := range []struct {
		name      string
		policy    Policy
		disks     []Disk
		protected []DiskIdentity
	}{
		{"empty", Policy{}, nil, nil},
		{"multiple", Policy{}, []Disk{root, data}, nil},
		{"protected", Policy{}, []Disk{root}, []DiskIdentity{root.DiskIdentity}},
		{"missing protected", Policy{}, []Disk{root}, []DiskIdentity{data.DiskIdentity}},
		{"changed protected", Policy{}, []Disk{root, disk("EXAMPLE-DATA", 32768)}, []DiskIdentity{data.DiskIdentity}},
		{"duplicate serial", Policy{Match: Match{MaxSizeBytes: 8192}}, []Disk{root, disk("EXAMPLE-OS", 16384)}, nil},
		{"wrong model", Policy{Match: Match{Model: "OTHER"}}, []Disk{root}, nil},
		{"wrong controller", Policy{Match: Match{ControllerPCIAddress: "0000:02:00.0"}}, []Disk{root}, nil},
		{"too small", Policy{Match: Match{MinSizeBytes: 16384}}, []Disk{root}, nil},
		{"excluded", Policy{Exclude: []DiskIdentity{root.DiskIdentity}}, []Disk{root}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Select(test.policy, test.disks, test.protected); err == nil {
				t.Fatal("unproved target accepted")
			}
		})
	}
}

func TestTransportAndKernelEligibilityNeverGuess(t *testing.T) {
	original := disk("EXAMPLE-OS", 8192)
	for _, mutate := range []func(*Disk){
		func(d *Disk) { d.Fixed = false }, func(d *Disk) { d.Writable = false }, func(d *Disk) { d.Whole = false }, func(d *Disk) { d.Hidden = true },
		func(d *Disk) { d.Transport = "usb" }, func(d *Disk) { d.Transport = "unknown" }, func(d *Disk) { d.SerialNumber = "" },
		func(d *Disk) { d.SizeBytes = 0 }, func(d *Disk) { d.SizeBytes++ }, func(d *Disk) { d.WWN = "0x0000000000000000" },
		func(d *Disk) { d.SerialNumber = "unsafe\n" }, func(d *Disk) { d.Model = strings.Repeat("m", 257) },
	} {
		candidate := original
		mutate(&candidate)
		if _, err := Select(Policy{}, []Disk{candidate}, nil); err == nil {
			t.Fatalf("unqualified inventory accepted: %#v", candidate)
		}
	}
}

func TestWWNDuplicatesAndMultipathRefuseEvenWithDifferentSerials(t *testing.T) {
	first, second := disk("EXAMPLE-ONE", 8192), disk("EXAMPLE-TWO", 16384)
	first.WWN = "0x5000000000000001"
	second.WWN = first.WWN
	if _, err := Select(Policy{Match: Match{MaxSizeBytes: 8192}}, []Disk{first, second}, nil); err == nil {
		t.Fatal("duplicate WWN accepted")
	}
	second.Hidden = true
	if _, err := Select(Policy{}, []Disk{first, second}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintBindsProtectedClaimsAndCanonicalOrdering(t *testing.T) {
	a, b := disk("EXAMPLE-A", 8192).DiskIdentity, disk("EXAMPLE-B", 16384).DiskIdentity
	p := Policy{Exclude: []DiskIdentity{a, b}}
	first, err := Fingerprint(p, []DiskIdentity{a, b})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(Policy{Exclude: []DiskIdentity{b, a, a}}, []DiskIdentity{b, a, b})
	if err != nil || first != second {
		t.Fatalf("authoring order moved proof: %v", err)
	}
	third, _ := Fingerprint(p, []DiskIdentity{a})
	if first == third {
		t.Fatal("changed storage protection reused proof")
	}
	changed, _ := Fingerprint(Policy{Match: Match{Model: "EXAMPLE"}, Exclude: p.Exclude}, []DiskIdentity{a, b})
	if changed == first {
		t.Fatal("changed policy reused proof")
	}
	if p.Exclude[0] != a || p.Exclude[1] != b {
		t.Fatal("fingerprint mutated authored policy")
	}
}

func TestBoundsAndInvalidPolicyRefuse(t *testing.T) {
	for _, p := range []Policy{
		{Match: Match{MinSizeBytes: 8192, MaxSizeBytes: 4096}},
		{Match: Match{MinSizeBytes: 1}},
		{Match: Match{ControllerPCIAddress: "../controller"}},
		{Match: Match{Model: "space\n"}},
		{Exclude: make([]DiskIdentity, MaxDisks+1)},
	} {
		if p.Validate() == nil {
			t.Fatalf("invalid policy accepted: %#v", p)
		}
	}
	if _, err := Select(Policy{}, make([]Disk, MaxDisks+1), nil); err == nil {
		t.Fatal("unbounded inventory accepted")
	}
}
