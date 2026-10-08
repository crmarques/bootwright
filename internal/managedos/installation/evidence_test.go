package installation

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Positive no effect is exactly a powered-off machine holding no marker with
// nothing published. Anything else stays unknown, so a resolution never claims
// an installation did not happen when it cannot prove it.
func TestNoEffectRequiresAPoweredOffMachineWithNothingPublished(t *testing.T) {
	if err := ValidateNoEffect(encode(t, Evidence{Power: "Off", Request: "digest"}), "digest"); err != nil {
		t.Fatalf("a fresh machine was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"running":          {Power: "On", Request: "digest"},
		"marker present":   {Marker: "{}", Power: "Off", Request: "digest"},
		"image published":  {Image: true, Power: "Off", Request: "digest"},
		"tree published":   {Power: "Off", Request: "digest", Tree: true},
		"tree unmarked":    {Power: "Off", Request: "digest", TreeContent: true},
		"tree staged":      {Power: "Off", Request: "digest", TreeStaging: true},
		"no power reading": {Request: "digest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNoEffect(encode(t, evidence), "digest"); err == nil {
				t.Fatal("unproved absence was accepted as no effect")
			}
		})
	}
}

// The work area is never served, so an apply stopped with nothing else left
// had no effect a machine could fetch; its removal still takes the area back.
func TestAWorkAreaAloneProvesNoEffectButNotAbsence(t *testing.T) {
	work := Evidence{Power: "Off", Request: "digest", Work: true}
	if err := ValidateNoEffect(encode(t, work), "digest"); err != nil {
		t.Fatalf("an apply that left only its work area was refused as no effect: %v", err)
	}
	if err := ValidatePartial(encode(t, work), "digest", "{}"); err == nil {
		t.Fatal("an apply that left only its work area was accepted as partial")
	}
	if err := ValidateWithdrawn(encode(t, Evidence{Request: "digest", Work: true}), "digest"); err == nil {
		t.Fatal("a removal that left the work area was accepted as withdrawn")
	}
	if err := ValidateWithdrawalUnfinished(encode(t, Evidence{Request: "digest", Work: true}), "digest"); err != nil {
		t.Fatalf("a removal that left the work area was not unfinished: %v", err)
	}
}

func TestAbsenceRequiresPositiveRemovalOfPublishedContent(t *testing.T) {
	gone := Evidence{Absent: true, Postcondition: true, Request: "digest"}
	if err := ValidateAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("removal evidence was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"not absent":        {Postcondition: true, Request: "digest"},
		"no postcondition":  {Absent: true, Request: "digest"},
		"image remains":     {Absent: true, Postcondition: true, Image: true, Request: "digest"},
		"tree remains":      {Absent: true, Postcondition: true, Request: "digest", Tree: true},
		"tree unmarked":     {Absent: true, Postcondition: true, Request: "digest", TreeContent: true},
		"tree staged":       {Absent: true, Postcondition: true, Request: "digest", TreeStaging: true},
		"work area remains": {Absent: true, Postcondition: true, Request: "digest", Work: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("incomplete removal evidence was accepted")
			}
		})
	}
}

// A partial installation is content this operation published on a guest that
// never installed, or its own marker with the completion not yet true. A guest
// holding another installation, and one powered on with none, are never
// converged: the first belongs to someone else and the second may be running
// the installer right now.
func TestPartialRequiresThisOperationsOwnUnfinishedWork(t *testing.T) {
	for name, evidence := range map[string]Evidence{
		"image published, guest never booted": {Image: true, Power: "Off", Request: "digest"},
		"tree published, guest never booted":  {Power: "Off", Request: "digest", Tree: true},
		"tree unmarked, guest never booted":   {Power: "Off", Request: "digest", TreeContent: true},
		"tree staged, guest never booted":     {Power: "Off", Request: "digest", TreeStaging: true},
		"installed, media still inserted":     {Marker: "{}", Media: "http://ip/os/m/install.iso", Power: "On", Image: true, Request: "digest"},
		"installed, content withdrawn":        {Marker: "{}", Power: "On", Request: "digest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), "digest", "{}"); err != nil {
				t.Fatalf("partial evidence was refused: %v", err)
			}
		})
	}
	for name, evidence := range map[string]Evidence{
		"another installation":  {Marker: "{\"other\":true}", Power: "On", Request: "digest"},
		"booted with no marker": {Image: true, Power: "On", Request: "digest"},
		"nothing published":     {Power: "Off", Request: "digest"},
		"already complete":      {Marker: "{}", Postcondition: true, Power: "On", Request: "digest"},
		"already absent":        {Absent: true, Request: "digest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), "digest", "{}"); err == nil {
				t.Fatal("evidence that proves no partial installation was accepted")
			}
		})
	}
}

// Evidence that names another request proves nothing about this one, whatever
// else it reports.
func TestEvidenceMustNameItsOwnRequest(t *testing.T) {
	evidence := encode(t, Evidence{Absent: true, Postcondition: true, Request: "other"})
	if err := ValidateAbsence(evidence, "digest"); err == nil {
		t.Fatal("evidence for another request was accepted")
	}
	if err := ValidateNoEffect(evidence, "digest"); err == nil {
		t.Fatal("evidence for another request was accepted")
	}
}

func TestEvidenceIsBoundedAndStrictlyShaped(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":         {},
		"unknown field": []byte(`{"request":"digest","invented":true}`),
		"trailing":      []byte(`{"request":"digest"}{}`),
		"not an object": []byte(`"digest"`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(data, "digest"); err == nil {
				t.Fatal("malformed evidence was accepted")
			}
		})
	}
	gone := encode(t, Evidence{Absent: true, Postcondition: true, Request: "digest"})
	installed := encode(t, Evidence{Address: "192.0.2.10", HostKey: "ssh-ed25519 AAAA", Postcondition: true, Request: "digest"})
	if _, _, err := HostKeyEvidence(installed); err != nil {
		t.Fatalf("a proved host key was refused: %v", err)
	}
	for _, closer := range []string{"}", "]"} {
		if err := ValidateAbsence([]byte(string(gone)+closer), "digest"); err == nil {
			t.Errorf("evidence followed by %s was accepted", closer)
		}
		if _, _, err := HostKeyEvidence([]byte(string(installed) + closer)); err == nil {
			t.Errorf("a host key followed by %s was accepted", closer)
		}
	}
	oversized := make([]byte, maxEvidenceBytes+1)
	for index := range oversized {
		oversized[index] = ' '
	}
	if err := ValidateAbsence(oversized, "digest"); err == nil {
		t.Fatal("unbounded evidence was accepted")
	}
}

// An installation whose machine cannot be logged in to is not finished. Both
// the apply and the observation that resolves it run this one rule, so a
// continuation cannot settle on weaker evidence than the apply demanded: the
// observation path proved the marker and the host key and never once proved
// the machine was usable, and that is what let an interrupted apply report a
// completed installation it had not verified.
func TestCompletionRequiresTheFleetAccountToAnswer(t *testing.T) {
	request := Request{Address: "198.51.100.11"}
	complete := Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: "{}",
		Postcondition: true, Power: "On", Reachable: true, Request: "digest",
	}
	if err := ValidatePresence(encode(t, complete), request, "digest", "{}"); err != nil {
		t.Fatalf("a reachable installation was refused: %v", err)
	}
	unreachable := complete
	unreachable.Reachable = false
	if err := ValidatePresence(encode(t, unreachable), request, "digest", "{}"); err == nil {
		t.Fatal("an installation whose machine never answered was accepted as complete")
	}
}

// A machine that holds the marker but has not answered yet is part way through
// rather than failed, so the verb retries instead of refusing: sshd finishes
// starting after the identity channel already answers.
func TestAMachineThatHoldsTheMarkerWithoutAnsweringIsPartial(t *testing.T) {
	evidence := Evidence{Marker: "{}", Power: "On", Reachable: false, Request: "digest"}
	if err := ValidatePartial(encode(t, evidence), "digest", "{}"); err != nil {
		t.Fatalf("an installed machine that has not answered yet was refused: %v", err)
	}
}

// A package tree extracted from another DVD than the one this operation froze
// is not this installation's tree, however complete it is.
func TestPresenceRequiresTheTreeTheOperationFroze(t *testing.T) {
	request := pinnedRequest(t, labCatalog())
	marker, err := MarkerFor(request, "digest")
	if err != nil {
		t.Fatal(err)
	}
	complete := Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: string(marker),
		Postcondition: true, Power: "On", Reachable: true, Request: "digest", Tree: true, TreeContent: true,
		TreeIdentity: dvdDigest,
	}
	if err := ValidatePresence(encode(t, complete), request, "digest", string(marker)); err != nil {
		t.Fatalf("the frozen tree was refused: %v", err)
	}
	for name, identity := range map[string]string{"another image": bootDigest, "no identity": ""} {
		t.Run(name, func(t *testing.T) {
			other := complete
			other.TreeIdentity = identity
			if err := ValidatePresence(encode(t, other), request, "digest", string(marker)); err == nil {
				t.Fatal("a tree extracted from another image was accepted")
			}
		})
	}
}

// A private installer image names the private URL in its Kickstart, so a
// completed delivered-key installation has withdrawn it with the key pair:
// its presence accepts no image and no private material, and refuses either
// still published. A public installation still needs its image in place.
func TestPresenceOfAPrivateInstallationRequiresItsImageWithdrawn(t *testing.T) {
	private := Request{Address: "198.51.100.41", Private: &Publication{Path: "/srv/public/private/os/metal-01"}}
	withdrawn := Evidence{
		Address: private.Address, HostKey: "ssh-ed25519 AAAAHOST", Marker: "{}",
		Postcondition: true, Power: "On", Reachable: true, Request: "digest",
	}
	if err := ValidatePresence(encode(t, withdrawn), private, "digest", "{}"); err != nil {
		t.Fatalf("a private installation that withdrew everything was refused: %v", diagnostics.Of(err))
	}
	for name, test := range map[string]struct {
		change  func(*Evidence)
		message string
	}{
		"its image":    {func(e *Evidence) { e.Image = true }, "the installation left its private installer image published"},
		"its key pair": {func(e *Evidence) { e.Private = true }, "the installation left material only its machine may read published"},
	} {
		t.Run(name, func(t *testing.T) {
			left := withdrawn
			test.change(&left)
			err := ValidatePresence(encode(t, left), private, "digest", "{}")
			expectRefusal(t, err, "lifecycle.state")
			if reported := diagnostics.Of(err)[0]; reported.Message != test.message {
				t.Fatalf("message = %q", reported.Message)
			}
		})
	}
	public := Request{Address: private.Address, Image: &Publication{Path: "/srv/public/os/rhel-01/install.iso"}}
	err := ValidatePresence(encode(t, withdrawn), public, "digest", "{}")
	expectRefusal(t, err, "lifecycle.state")
	if reported := diagnostics.Of(err)[0]; reported.Message != "the installation did not publish everything its boot needs" {
		t.Fatalf("message = %q", reported.Message)
	}
}
