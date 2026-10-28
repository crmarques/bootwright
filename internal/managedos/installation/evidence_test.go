package installation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
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
	bounded := make([]string, 16)
	for index := range bounded {
		bounded[index] = strings.Repeat("d", 200)
	}
	partial := Evidence{Marker: "{}", Power: "On", Request: "digest"}
	partial.NetworkDifferences = bounded
	if err := ValidatePartial(encode(t, partial), "digest", "{}"); err != nil {
		t.Fatalf("sixteen differences of 200 bytes were refused: %v", diagnostics.Of(err))
	}
	for name, differences := range map[string][]string{
		"seventeen differences": append(append([]string{}, bounded...), "d"),
		"a long difference":     {strings.Repeat("d", 201)},
		"a control character":   {"mtu \x07"},
	} {
		t.Run(name, func(t *testing.T) {
			unbounded := partial
			unbounded.NetworkDifferences = differences
			err := ValidatePartial(encode(t, unbounded), "digest", "{}")
			expectRefusal(t, err, "lifecycle.state")
			if message := diagnostics.Of(err)[0].Message; message != "the installation evidence reports unbounded network differences" {
				t.Fatalf("refusal = %q", message)
			}
			unbounded.Address, unbounded.HostKey = "192.0.2.10", "ssh-ed25519 AAAA"
			if _, _, err := HostKeyEvidence(encode(t, unbounded)); err == nil {
				t.Fatal("a host key with unbounded network differences was accepted")
			}
		})
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
		Network: true, NetworkReboot: true, NetworkDifferences: []string{},

		Update: UpdateEvidence{State: updateNone},
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
		TreeIdentity: dvdDigest, Network: true, NetworkReboot: true, NetworkDifferences: []string{},

		Update: UpdateEvidence{State: updateNone},
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
		Network: true, NetworkReboot: true, NetworkDifferences: []string{},

		Update: UpdateEvidence{State: updateNone},
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

func TestSharedTreeAloneIsNoMachineEffect(t *testing.T) {
	evidence := Evidence{Power: "Off", Request: "digest", Tree: true, TreeContent: true, TreeStaging: true}
	if err := ValidateNoEffect(encode(t, evidence), "digest"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePartial(encode(t, evidence), "digest", "{}"); err == nil {
		t.Fatal("shared tree counted as installation effect")
	}
	if err := ValidateWithdrawn(encode(t, evidence), "digest"); err != nil {
		t.Fatal(err)
	}
}

// A completed installation proves its declared network after a restart, with
// nothing differing; a network applied but never restarted, or one that
// drifted, is not finished.
func TestPresenceRequiresTheNetworkProvedAfterAReboot(t *testing.T) {
	request := Request{Address: "198.51.100.11"}
	complete := Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: "{}",
		Postcondition: true, Power: "On", Reachable: true, Request: "digest",
		Network: true, NetworkReboot: true, NetworkDifferences: []string{},

		Update: UpdateEvidence{State: updateNone},
	}
	if err := ValidatePresence(encode(t, complete), request, "digest", "{}"); err != nil {
		t.Fatalf("a converged installation was refused: %v", diagnostics.Of(err))
	}
	for name, change := range map[string]func(*Evidence){
		"network unproved":       func(e *Evidence) { e.Network = false },
		"never restarted":        func(e *Evidence) { e.NetworkReboot = false },
		"a declared value drift": func(e *Evidence) { e.NetworkDifferences = []string{"mtu of bond0 is 1500, declared 9000"} },
	} {
		t.Run(name, func(t *testing.T) {
			unproved := complete
			change(&unproved)
			err := ValidatePresence(encode(t, unproved), request, "digest", "{}")
			expectRefusal(t, err, "lifecycle.state")
			if message := diagnostics.Of(err)[0].Message; message != "the installed machine's network does not match its declaration after a reboot" {
				t.Fatalf("refusal = %q", message)
			}
		})
	}
}

// A network that failed to converge leaves the frozen marker on a running
// machine, so the apply is part way through and never had no effect.
func TestANetworkFailureIsPartialNeverNoEffect(t *testing.T) {
	request := Request{Address: "198.51.100.11"}
	failed := encode(t, Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Marker: "{}", Power: "On",
		Reachable: true, Request: "digest", NetworkDifferences: []string{},
	})
	if effect, _ := installationEffect(reconciliation.Apply, failed, request, "digest", "{}"); effect != reconciliation.EffectPartial {
		t.Fatalf("a failed network convergence reads as %s, want partial", effect)
	}
	if err := ValidateNoEffect(failed, "digest"); err == nil {
		t.Fatal("a failed network convergence was accepted as no effect")
	}
}

// The evidence alone of a delivered-key Machine the restart powered off shows
// nothing: no marker, nothing published and a powered-off controller read. Read
// alone it is no effect; the observation reads it partial once the apply's
// records hold the disk-boot intent
// (TestAnInstallationProvedBeforeItsPowerOffIsNeverReadAsNoEffect). A Machine
// whose image stays published reads partial in the same window.
func TestAPrivateMachinePoweredOffByTheRestartReadsNoEffectAlone(t *testing.T) {
	off := func(image bool) []byte {
		return encode(t, Evidence{Address: "198.51.100.11", Image: image, Power: "Off", Request: "digest", NetworkDifferences: []string{}})
	}
	private := Request{Address: "198.51.100.11", Private: &Publication{Path: "/var/lib/bootwright-services/x", URL: "https://x"}}
	if effect, _ := installationEffect(reconciliation.Apply, off(false), private, "digest", "{}"); effect != reconciliation.EffectNoEffect {
		t.Fatalf("a delivered-key machine powered off by the restart reads as %s, want no-effect", effect)
	}
	public := Request{Address: "198.51.100.11"}
	if effect, _ := installationEffect(reconciliation.Apply, off(true), public, "digest", "{}"); effect != reconciliation.EffectPartial {
		t.Fatalf("a machine whose image stays published reads as %s, want partial", effect)
	}
}

// A network that drifted after its restart is partial while the adapter has
// not claimed its postcondition, and unknown when it claims one it did not
// prove.
func TestADriftedNetworkIsPartial(t *testing.T) {
	request := Request{Address: "198.51.100.11"}
	drifted := Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Marker: "{}", Power: "On",
		Image: true, Reachable: true, Request: "digest", NetworkReboot: true,
		NetworkDifferences: []string{"mtu of bond0 is 1500, declared 9000"},
	}
	if effect, _ := installationEffect(reconciliation.Apply, encode(t, drifted), request, "digest", "{}"); effect != reconciliation.EffectPartial {
		t.Fatalf("a drifted network reads as %s, want partial", effect)
	}
	drifted.Postcondition = true
	if effect, _ := installationEffect(reconciliation.Apply, encode(t, drifted), request, "digest", "{}"); effect != reconciliation.EffectUnknown {
		t.Fatalf("a drifted network claimed complete reads as %s, want unknown", effect)
	}
}
