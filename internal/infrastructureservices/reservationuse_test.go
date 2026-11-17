package infrastructureservices

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestUseConflictsReportEachHeldKeyOnceAgainstAUse(t *testing.T) {
	held := []HostReservation{
		{Context: "alpha", Kind: "substrate-physical", Service: "node", Keys: []string{"bmc:bmc.example:443/abc", "bmc:bmc.example:443/abc"}},
		{Context: "lab", Kind: "substrate-physical", Service: "own", Keys: []string{"bmc:bmc.example:443/abc"}},
		{Context: "beta", Kind: "substrate-physical", Service: "shared", Keys: []string{"bmc:bmc.example:443/abc"}, Shared: true},
		{Context: "gamma", Kind: "substrate-physical", Service: "far", Keys: []string{"bmc:other.example:443/abc"}},
	}
	use := HostReservation{Context: "lab", Kind: "substrate-physical", Service: "node", Keys: []string{"bmc:bmc.example:443/abc"}}
	found := UseConflicts(held, use)
	if len(found) != 2 || found[0].Held.Context != "alpha" || found[1].Held.Context != "beta" || found[0].HeldKey != "bmc:bmc.example:443/abc" || found[0].WantedKey != use.Keys[0] {
		t.Fatalf("conflicts = %+v", found)
	}
	shared := use
	shared.Shared = true
	if found := UseConflicts(held[2:3], shared); len(found) != 0 {
		t.Errorf("two shared claims conflict: %+v", found)
	}
	if found := UseConflicts(held[2:3], use); len(found) != 1 || found[0].Held.Context != "beta" {
		t.Errorf("an exclusive use beside a shared holder = %+v", found)
	}
}

func TestUseDiagnosticNamesWhatTheUsedKeyClassMeans(t *testing.T) {
	for class, want := range map[string]string{
		"bmc":        "the management controller endpoint Machine/node is driven through",
		"bmc-media":  "the management controller's shared media and verification settings Machine/node uses",
		"bmc-system": "the system UUID Machine/node's pin records",
		"bmc-mac":    "a MAC address Machine/node declares",
		"socket":     "socket 0.0.0.0:53 that Machine/node uses",
	} {
		key := class + ":x"
		if class == "socket" {
			key = "socket:0.0.0.0:53"
		}
		conflict := Conflict{
			Held:      HostReservation{Context: "alpha", Kind: "substrate-physical", Service: "held", Keys: []string{key}},
			Wanted:    HostReservation{Context: "lab", Kind: "substrate-physical", Service: "node", Keys: []string{key}},
			HeldKey:   key,
			WantedKey: key,
		}
		got := UseDiagnostic(conflict)
		if got.Code != "controller.conflict" || got.Severity != "error" || !strings.Contains(got.Message, "context alpha holds "+want+", for Machine/held") ||
			!strings.HasSuffix(got.Message, "so this context may not drive that machine and nothing was sent to it") {
			t.Errorf("%s: message = %q", class, got.Message)
		}
		if !strings.HasPrefix(got.Remediation, "act on the machine through context alpha, or run bootwright destroy --context alpha first") {
			t.Errorf("%s: remediation = %q", class, got.Remediation)
		}
		if class == "bmc" && !strings.HasSuffix(got.Remediation, ", or change spec.substrate.baremetal.bmc.address on Machine/node") {
			t.Errorf("bmc remediation = %q", got.Remediation)
		}
		if got.Object == nil || got.Object.Kind != "Machine" || got.Object.Name != "node" {
			t.Errorf("%s: object = %+v", class, got.Object)
		}
	}
	unnamed := Conflict{
		Held:    HostReservation{Context: "alpha", Kind: "custom", Service: "h"},
		Wanted:  HostReservation{Context: "lab", Kind: "custom", Service: "w"},
		HeldKey: "path:/x", WantedKey: "path:/x",
	}
	if got := UseDiagnostic(unnamed); got.Object != nil || !strings.Contains(got.Message, "a path reservation that custom w uses") {
		t.Errorf("an unnamed claim kind = %+v", got)
	}
}

func TestClaimKindNamesObjectOnlyForKindsWithAnAPIObject(t *testing.T) {
	for kind, want := range map[string]bool{"proxy": true, "dns": true, "substrate-machine": true, "os-install": true, "cluster-media": false, "": false, "unknown": false} {
		if got := ClaimKindNamesObject(kind); got != want {
			t.Errorf("ClaimKindNamesObject(%q) = %t, want %t", kind, got, want)
		}
	}
}

func TestReleaseFailureNamesTheRemovalsAnotherContextStillUses(t *testing.T) {
	held := []HostReservation{
		{Context: "lab", Kind: "substrate-host", Service: "remote", Keys: []string{"bridge:virbr-lab"}},
		{Context: "lab", Kind: "substrate-host", Service: "other", Keys: []string{"bridge:virbr-two"}},
		{Context: "alpha", Kind: "substrate-host-use", Service: "near", Keys: []string{"bridge-use:virbr-lab", "bridge-use:virbr-two"}, Shared: true},
	}
	if err := ReleaseFailure(held, "lab", "", ""); err == nil {
		t.Fatal("no failure for two guarded bridges")
	} else if failure, ok := err.(interface{ Error() string }); !ok || failure.Error() == "" {
		t.Errorf("failure = %#v", err)
	}
	if err := ReleaseFailure(held, "lab", string(api.InfraProvider), "remote"); err == nil || strings.Contains(err.Error(), "virbr-two") {
		t.Errorf("the named object's removal = %v", err)
	}
	if err := ReleaseFailure(held, "lab", string(api.InfraProvider), "absent"); err != nil {
		t.Errorf("an unrelated removal = %v", err)
	}
	if err := ReleaseFailure(held, "beta", "", ""); err != nil {
		t.Errorf("a context holding nothing = %v", err)
	}
}

func TestUnqualifiedSubjectAndHasClassReadOneClassPrefix(t *testing.T) {
	if subject, found := UnqualifiedSubject("unit:bootwright-dns", UnitClass); !found || subject != "bootwright-dns" {
		t.Errorf("UnqualifiedSubject = %q, %t", subject, found)
	}
	if subject, found := UnqualifiedSubject("path:/srv", UnitClass); found || subject != "path:/srv" {
		t.Errorf("another class = %q, %t", subject, found)
	}
	if !HasClass("bridge:virbr-lab", BridgeClass) || HasClass("bridge-use:virbr-lab", BridgeClass) || HasClass("", BridgeClass) {
		t.Error("HasClass reads more or less than the class and its colon")
	}
}
