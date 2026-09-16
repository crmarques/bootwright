package libvirt

import (
	"encoding/json"
	"testing"
)

func hostRequest(t *testing.T) HostRequest {
	t.Helper()
	requests, err := HostRequests(labCatalog(), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	return requests[0]
}

func machineRequest(t *testing.T) MachineRequest {
	t.Helper()
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	return requests[0]
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Presence is accepted only when every part of the frozen request is proved.
// Each case below removes exactly one proof.
func TestHostPresenceRequiresEveryProof(t *testing.T) {
	request := hostRequest(t)
	complete := func() HostEvidence {
		var evidence HostEvidence
		if err := json.Unmarshal(hostEvidence(request, "digest"), &evidence); err != nil {
			t.Fatal(err)
		}
		return evidence
	}
	if err := ValidateHostPresence(encode(t, complete()), request, "digest"); err != nil {
		t.Fatalf("complete evidence was refused: %v", err)
	}
	for name, damage := range map[string]func(*HostEvidence){
		"no hypervisor":     func(e *HostEvidence) { e.Hypervisor = false },
		"daemon inactive":   func(e *HostEvidence) { e.Service = "failed" },
		"uri silent":        func(e *HostEvidence) { e.URI = false },
		"pool inactive":     func(e *HostEvidence) { e.Pool = "" },
		"no postcondition":  func(e *HostEvidence) { e.Postcondition = false },
		"network inactive":  func(e *HostEvidence) { e.Networks[0].State = "inactive" },
		"network unowned":   func(e *HostEvidence) { e.Networks[0].Owned = false },
		"bridge missing":    func(e *HostEvidence) { e.Networks[0].Bridge = false },
		"network dropped":   func(e *HostEvidence) { e.Networks = nil },
		"another request":   func(e *HostEvidence) { e.Request = "other" },
		"reported as gone":  func(e *HostEvidence) { e.Absent = true },
		"renamed a network": func(e *HostEvidence) { e.Networks[0].Name = "bootwright-other" },
	} {
		t.Run(name, func(t *testing.T) {
			evidence := complete()
			damage(&evidence)
			if err := ValidateHostPresence(encode(t, evidence), request, "digest"); err == nil {
				t.Fatal("incomplete evidence was accepted")
			}
		})
	}
}

// Removal is accepted only when it positively proves the owned objects are
// gone, never merely that they were not observed.
func TestHostAbsenceRequiresPositiveRemoval(t *testing.T) {
	gone := HostEvidence{Absent: true, Postcondition: true, Request: "digest"}
	if err := ValidateHostAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("removal evidence was refused: %v", err)
	}
	for name, evidence := range map[string]HostEvidence{
		"not absent":       {Postcondition: true, Request: "digest"},
		"no postcondition": {Absent: true, Request: "digest"},
		"pool remains":     {Absent: true, Postcondition: true, Pool: "active", Request: "digest"},
		"network remains":  {Absent: true, Postcondition: true, Request: "digest", Networks: []NetworkEvidence{{Managed: true, Name: "n"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHostAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("incomplete removal evidence was accepted")
			}
		})
	}
}

func TestMachinePresenceRequiresEveryProof(t *testing.T) {
	request := machineRequest(t)
	complete := func() MachineEvidence {
		var evidence MachineEvidence
		if err := json.Unmarshal(machineEvidence(request, "digest"), &evidence); err != nil {
			t.Fatal(err)
		}
		return evidence
	}
	if err := ValidateMachinePresence(encode(t, complete()), request, "digest"); err != nil {
		t.Fatalf("complete evidence was refused: %v", err)
	}
	for name, damage := range map[string]func(*MachineEvidence){
		"another domain":   func(e *MachineEvidence) { e.Domain = "bootwright-other" },
		"foreign domain":   func(e *MachineEvidence) { e.Owned = false },
		"unit inactive":    func(e *MachineEvidence) { e.Unit = "failed" },
		"another image":    func(e *MachineEvidence) { e.Controller = "docker.io/other@sha256:0" },
		"another system":   func(e *MachineEvidence) { e.System = "00000000-0000-0000-0000-000000000000" },
		"no power state":   func(e *MachineEvidence) { e.Power = "" },
		"disk missing":     func(e *MachineEvidence) { e.Disks[0].Present = false },
		"disk resized":     func(e *MachineEvidence) { e.Disks[0].SizeGiB = 10 },
		"disk dropped":     func(e *MachineEvidence) { e.Disks = nil },
		"no postcondition": func(e *MachineEvidence) { e.Postcondition = false },
	} {
		t.Run(name, func(t *testing.T) {
			evidence := complete()
			damage(&evidence)
			if err := ValidateMachinePresence(encode(t, evidence), request, "digest"); err == nil {
				t.Fatal("incomplete evidence was accepted")
			}
		})
	}
}

func TestMachineAbsenceRequiresPositiveRemoval(t *testing.T) {
	gone := MachineEvidence{Absent: true, Postcondition: true, Request: "digest"}
	if err := ValidateMachineAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("removal evidence was refused: %v", err)
	}
	for name, evidence := range map[string]MachineEvidence{
		"domain remains": {Absent: true, Postcondition: true, Domain: "bootwright-lab-rhel-01", Request: "digest"},
		"unit remains":   {Absent: true, Postcondition: true, Unit: "active", Request: "digest"},
		"power reported": {Absent: true, Postcondition: true, Power: "Off", Request: "digest"},
		"disk remains":   {Absent: true, Postcondition: true, Request: "digest", Disks: []DiskEvidence{{Name: "root", Present: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMachineAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("incomplete removal evidence was accepted")
			}
		})
	}
}

// A partial realization is what an interrupted effect usually leaves. It is
// accepted only for what this context owns, because a foreign object is never
// converged and the hypervisor closure is shared software this block never
// removes.
func TestHostPartialRequiresSomethingThisContextOwns(t *testing.T) {
	for name, evidence := range map[string]HostEvidence{
		"pool alone":       {Request: "digest", Pool: "active"},
		"owned network":    {Request: "digest", Networks: []NetworkEvidence{{Managed: true, Name: "n", Owned: true, State: "inactive"}}},
		"network and pool": {Request: "digest", Pool: "inactive", Networks: []NetworkEvidence{{Managed: true, Name: "n", Owned: true, State: "active"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHostPartial(encode(t, evidence), "digest"); err != nil {
				t.Fatalf("partial evidence was refused: %v", err)
			}
		})
	}
	for name, evidence := range map[string]HostEvidence{
		"nothing owned":    {Request: "digest"},
		"hypervisor alone": {Request: "digest", Hypervisor: true},
		"foreign network":  {Request: "digest", Networks: []NetworkEvidence{{Managed: true, Name: "n", State: "active"}}},
		"external bridge":  {Request: "digest", Networks: []NetworkEvidence{{Name: "n", Bridge: true}}},
		"already complete": {Request: "digest", Postcondition: true, Pool: "active"},
		"already absent":   {Request: "digest", Absent: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHostPartial(encode(t, evidence), "digest"); err == nil {
				t.Fatal("evidence that proves no partial realization was accepted")
			}
		})
	}
}

func TestMachinePartialRequiresSomethingThisContextOwns(t *testing.T) {
	for name, evidence := range map[string]MachineEvidence{
		"owned domain":    {Request: "digest", Domain: "bootwright-lab-rhel-01", Owned: true},
		"controller only": {Request: "digest", Unit: "active"},
		"disk only":       {Request: "digest", Disks: []DiskEvidence{{Name: "root", Present: true, SizeGiB: 40}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMachinePartial(encode(t, evidence), "digest"); err != nil {
				t.Fatalf("partial evidence was refused: %v", err)
			}
		})
	}
	for name, evidence := range map[string]MachineEvidence{
		"nothing present":  {Request: "digest"},
		"foreign domain":   {Request: "digest", Domain: "bootwright-lab-rhel-01"},
		"already complete": {Request: "digest", Postcondition: true, Domain: "bootwright-lab-rhel-01", Owned: true},
		"already absent":   {Request: "digest", Absent: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMachinePartial(encode(t, evidence), "digest"); err == nil {
				t.Fatal("evidence that proves no partial realization was accepted")
			}
		})
	}
}

// An adapter cannot widen the result shape: unknown fields, trailing data and
// an unbounded payload are refused before anything is read from them.
func TestEvidenceIsBoundedAndStrictlyShaped(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":         {},
		"unknown field": []byte(`{"request":"digest","invented":true}`),
		"trailing":      []byte(`{"request":"digest"}{}`),
		"not an object": []byte(`"digest"`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHostAbsence(data, "digest"); err == nil {
				t.Fatal("malformed evidence was accepted")
			}
			if err := ValidateMachineAbsence(data, "digest"); err == nil {
				t.Fatal("malformed evidence was accepted")
			}
		})
	}
	oversized := make([]byte, maxEvidenceBytes+1)
	for index := range oversized {
		oversized[index] = ' '
	}
	if err := ValidateMachineAbsence(oversized, "digest"); err == nil {
		t.Fatal("unbounded evidence was accepted")
	}
}
