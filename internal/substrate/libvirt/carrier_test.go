package libvirt

import (
	"testing"
)

func TestCarrierPolicyIsRequiredForPresenceAndPositiveRemoval(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	for name, damage := range map[string]func(*HostEvidence){
		"missing":        func(e *HostEvidence) { e.CarrierPolicy = CarrierPolicyEvidence{} },
		"unread":         func(e *HostEvidence) { e.CarrierPolicy.Answered = false },
		"foreign":        func(e *HostEvidence) { e.CarrierPolicy.Owned = false },
		"another owner":  func(e *HostEvidence) { e.CarrierPolicy.Name = "90-bootwright-other-host-vlan-carrier.conf" },
		"pending reload": func(e *HostEvidence) { e.CarrierPolicy.Definition = false },
		"absent":         func(e *HostEvidence) { e.CarrierPolicy.Present = false },
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHostPresence(evidenceOf(t, request, damage), request, evidenceDigest); err == nil {
				t.Fatal("incomplete carrier policy accepted")
			}
		})
	}
	gone := HostEvidence{Absent: true, CarrierPolicy: CarrierPolicyEvidence{Answered: true}, ConnectionsAnswered: true,
		Directory: observed(false), FirewallReady: FirewallReadyEvidence{Answered: true}, PoolAnswered: true,
		Postcondition: true, Request: evidenceDigest, URI: true}
	if err := ValidateHostAbsence(encode(t, gone), evidenceDigest); err != nil {
		t.Fatal(err)
	}
	for _, residual := range []CarrierPolicyEvidence{{}, {Answered: true, Present: true, Owned: true}, {Answered: true, Owned: true}} {
		gone.CarrierPolicy = residual
		if err := ValidateHostAbsence(encode(t, gone), evidenceDigest); err == nil {
			t.Fatal("carrier policy removal not proved")
		}
	}
}

func TestCarrierPolicyAloneIsAnOwnedPartialEffect(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	evidence := HostEvidence{Request: evidenceDigest, CarrierPolicy: CarrierPolicyEvidence{Answered: true, Present: true, Owned: true, Name: carrierPolicyName(request)}}
	if err := ValidateHostPartial(encode(t, evidence), evidenceDigest); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHostRemovalUnfinished(encode(t, evidence), request, evidenceDigest); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []CarrierPolicyEvidence{{Present: true}, {Answered: true, Present: true}, {Owned: true}} {
		evidence.CarrierPolicy = policy
		if err := ValidateHostPartial(encode(t, evidence), evidenceDigest); err == nil {
			t.Fatal("unknown or foreign policy counted as owned")
		}
	}
	plain := hostRequest(t)
	data := evidenceOf(t, plain, func(e *HostEvidence) {
		e.CarrierPolicy = CarrierPolicyEvidence{Answered: true, Definition: true, Present: true, Owned: true, Name: carrierPolicyName(plain)}
	})
	if err := ValidateHostPresence(data, plain, evidenceDigest); err == nil {
		t.Fatal("policy left after last leg accepted")
	}
}
