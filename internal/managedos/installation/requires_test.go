package installation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Which proofs a completion requires is derived from the request's own
// fields, frozen in its requires block for the role to read and judged by Go:
// a root declared by serial number is the only one whose WWN is proved.
func TestTheRequestFreezesTheProofsItsFieldsRequire(t *testing.T) {
	serial := &Hardware{SerialNumber: "ZC1A2B3C", SizeBytes: 480103981056}
	for name, test := range map[string]struct {
		request Request
		want    Requires
	}{
		"plain":      {Request{}, Requires{}},
		"fips":       {Request{InstallerArguments: []string{"ifname=enp1s0:52:54:00:00:00:01", fipsArgument}}, Requires{FIPS: true}},
		"fips=0":     {Request{InstallerArguments: []string{"fips=0"}}, Requires{}},
		"encrypting": {Request{DiskEncryption: &DiskEncryption{RecoveryPassphraseRef: "recovery"}}, Requires{RootEncryption: true}},
		"registering": {Request{Registration: &Registration{}, Update: &Update{Policy: "withinRelease", Release: "9.8"}},
			Requires{Registration: true, Update: true}},
		"serial":  {Request{Target: Target{Physical: true, Hardware: serial}}, Requires{RootWWN: true}},
		"wwn":     {Request{Target: Target{Physical: true, Hardware: &Hardware{SerialNumber: "ZC1A2B3C", RootDevice: "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"}}}, Requires{}},
		"volume":  {Request{Target: Target{Physical: true, Hardware: &Hardware{SerialNumber: "ZC1A2B3C", RootVolumeRef: &substrate.VolumeRef{}}}}, Requires{}},
		"virtual": {Request{Target: Target{Hardware: serial}}, Requires{}},
		"bare":    {Request{Target: Target{Physical: true}}, Requires{}},
	} {
		if got := test.request.requirements(); got != test.want {
			t.Errorf("%s: requires %+v, want %+v", name, got, test.want)
		}
	}
}

// The worker sessions' timeouts are Go's constants, frozen into the request
// budgets, and the run bounds are derived from the same constants: the values
// are the ones the role once chose for itself.
func TestTheSessionBudgetsAndTheRunBoundsShareTheirConstants(t *testing.T) {
	update := &Update{Policy: "withinRelease", Release: "9.8"}
	hosted, satellite := &Registration{}, &Registration{Satellite: &Satellite{Hostname: "satellite.example.test"}}
	selected := &Registration{Satellite: &Satellite{Hostname: "satellite.example.test", ContentView: "composite_rhel9"}}
	for name, test := range map[string]struct {
		request Request
		want    Sessions
	}{
		"none":                {Request{}, Sessions{}},
		"updating":            {Request{Update: update}, Sessions{Inspection: 120, Update: 1800, UpdateCompletion: 120}},
		"registering":         {Request{Registration: hosted}, Sessions{Inspection: 120, Registration: 630, Unregistration: 330}},
		"registering updates": {Request{Registration: hosted, Update: update}, Sessions{Inspection: 120, Registration: 750, Unregistration: 330, Update: 1800, UpdateCompletion: 120}},
		"satellite":           {Request{Registration: satellite}, Sessions{Inspection: 120, Registration: 690, Unregistration: 330}},
		"satellite updates":   {Request{Registration: satellite, Update: update}, Sessions{Inspection: 120, Registration: 810, Unregistration: 330, Update: 1800, UpdateCompletion: 120}},
		"content view":        {Request{Registration: selected}, Sessions{Inspection: 120, Registration: 750, Unregistration: 330}},
		"content view updates": {Request{Registration: selected, Update: update},
			Sessions{Inspection: 120, Registration: 870, Unregistration: 330, Update: 1800, UpdateCompletion: 120}},
	} {
		got := test.request.sessions()
		if got != test.want {
			t.Errorf("%s: sessions %+v, want %+v", name, got, test.want)
		}
		if time.Duration(max(got.Registration, got.Unregistration))*time.Second+registrationInspection > registrationBound {
			t.Errorf("%s: a %+v session outlasts the registration bound %s", name, got, registrationBound)
		}
		if time.Duration(got.Update+got.UpdateCompletion)*time.Second+updateRebootBound+updateGuestRead+updateMarkerRead > updateBound {
			t.Errorf("%s: a %+v session outlasts the update bound %s", name, got, updateBound)
		}
	}
	if registrationBound != 15*time.Minute || updateBound != 45*time.Minute {
		t.Fatalf("bounds %s, %s", registrationBound, updateBound)
	}
}

// Go judges every required proof against the requires block the request
// froze: a required proof absent refuses, present completes, and a proof the
// request does not require but the evidence reports refuses where it would
// name something the installation did not perform.
func TestGoJudgesEachRequiredProofAgainstTheFrozenRequirement(t *testing.T) {
	plain := Request{Address: "198.51.100.11"}
	complete := Evidence{
		Address: plain.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: "{}", Postcondition: true, Power: "On",
		Reachable: true, Request: "digest", Network: true, NetworkReboot: true, NetworkDifferences: []string{},
		Update: UpdateEvidence{State: updateNone},
	}
	updated := UpdateEvidence{Kernel: "5.14.0-611.5.1.el9_7.x86_64", Packages: 2, Pin: pinRelease, Rebooted: true, Release: "9.8", State: updateComplete, Transaction: 3}
	owned := RegistrationEvidence{Consumer: "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", State: registrationOwned}
	required := func(change func(*Request)) Request {
		request := plain
		change(&request)
		request.freezeDerived()
		return request
	}
	fips := required(func(r *Request) { r.InstallerArguments = []string{fipsArgument} })
	encrypting := required(func(r *Request) { r.DiskEncryption = &DiskEncryption{RecoveryPassphraseRef: "recovery"} })
	serial := required(func(r *Request) { r.Target = Target{Physical: true, Hardware: &Hardware{SerialNumber: "ZC1A2B3C"}} })
	registering := required(func(r *Request) { r.Registration = &Registration{} })
	updating := required(func(r *Request) {
		r.Registration, r.Update = &Registration{}, &Update{Policy: "withinRelease", Release: "9.8"}
	})
	for name, test := range map[string]struct {
		request  Request
		evidence func(*Evidence)
		accepted bool
	}{
		"fips absent":            {fips, func(*Evidence) {}, false},
		"fips present":           {fips, func(e *Evidence) { e.FIPS = true }, true},
		"fips unrequired":        {plain, func(e *Evidence) { e.FIPS = true }, true},
		"encryption absent":      {encrypting, func(*Evidence) {}, false},
		"encryption present":     {encrypting, func(e *Evidence) { e.RootEncryption = true }, true},
		"root wwn present":       {serial, func(e *Evidence) { e.RootWWN = "naa.5000c500a1b2c3d4" }, true},
		"root wwn none reported": {serial, func(*Evidence) {}, true},
		"root wwn unexpected":    {plain, func(e *Evidence) { e.RootWWN = "naa.5000c500a1b2c3d4" }, false},
		"registration absent":    {registering, func(*Evidence) {}, false},
		"registration pending": {registering, func(e *Evidence) {
			e.Registration = RegistrationEvidence{Consumer: owned.Consumer, State: registrationPending}
		}, false},
		"registration present":        {registering, func(e *Evidence) { e.Registration = owned }, true},
		"update absent":               {updating, func(e *Evidence) { e.Registration = owned }, false},
		"update present":              {updating, func(e *Evidence) { e.Registration, e.Update = owned, updated }, true},
		"update unexpected":           {plain, func(e *Evidence) { e.Update = updated }, false},
		"frozen fips without its arg": {required(func(r *Request) { r.Requires.FIPS = true }), func(*Evidence) {}, true},
	} {
		evidence := complete
		test.evidence(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePresence(data, test.request, "digest", "{}"); (err == nil) != test.accepted {
			t.Errorf("%s: accepted %t, want %t (%v)", name, err == nil, test.accepted, diagnostics.Of(err))
		}
	}
	frozen := plain
	frozen.Requires.FIPS = true
	data, _ := json.Marshal(complete)
	if err := ValidatePresence(data, frozen, "digest", "{}"); err == nil {
		t.Error("a request whose frozen block requires FIPS accepted evidence without it")
	}
}

// A request whose frozen requirements, session budgets or roots are not what
// its own fields derive is refused on decode, so the role never reads a value
// Go did not derive.
func TestADecodedRequestHoldsTheValuesItsFieldsDerive(t *testing.T) {
	request := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	for name, change := range map[string]func(*Request){
		"requires":       func(r *Request) { r.Requires.FIPS = true },
		"no requirement": func(r *Request) { r.Requires.Registration = false },
		"session":        func(r *Request) { r.Budgets.Sessions.Registration = 900 },
		"no session":     func(r *Request) { r.Budgets.Sessions = Sessions{} },
	} {
		changed := request
		change(&changed)
		canonical, err := changed.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeRequest(canonical); err == nil {
			t.Errorf("%s: a request disagreeing with its own derivation decoded", name)
		}
	}
	canonical, _ := request.Canonical()
	if _, err := DecodeRequest(canonical); err != nil {
		t.Fatalf("the derived request does not decode: %v", diagnostics.Of(err))
	}
	if !strings.HasPrefix(request.Image.Path, request.ServedRoot+"/") {
		t.Fatalf("the image %s is not beneath the frozen served root %s", request.Image.Path, request.ServedRoot)
	}
}

func TestATreeRequestHoldsItsArtifactServersRoots(t *testing.T) {
	_, request := treeFixture(t)
	for name, change := range map[string]func(*TreeRequest){
		"area":        func(r *TreeRequest) { r.Area = []string{"services", "dns"} },
		"server":      func(r *TreeRequest) { r.Server = strings.Replace(r.Server, "/"+testContext+"/", "/other/", 1) },
		"served root": func(r *TreeRequest) { r.ServedRoot = r.Server + "/private" },
		"publication": func(r *TreeRequest) { r.Publication.Path = "/srv/os/tree" },
	} {
		changed := request
		changed.Area = append([]string(nil), request.Area...)
		change(&changed)
		canonical, err := changed.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeTreeRequest(canonical); err == nil {
			t.Errorf("%s: a tree request naming other roots decoded", name)
		}
	}
}
