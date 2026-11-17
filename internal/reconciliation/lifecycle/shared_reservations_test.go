package lifecycle

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func sharedMedia(keys ...string) prerequisites.HostReservation {
	return prerequisites.HostReservation{Context: testContextName, Kind: "media", Service: "media", Shared: true, Keys: keys}
}

func TestSharedReservationsUniteKeysWithoutChangingContributions(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		claims := []prerequisites.HostReservation{
			sharedMedia("media:boot.iso"),
			{Context: testContextName, Kind: "os-install", Service: "node", Keys: []string{"path:/srv/node"}},
			sharedMedia("media:dvd.iso", "media:boot.iso"),
		}
		if reverse {
			slices.Reverse(claims)
		}
		before := make([]prerequisites.HostReservation, len(claims))
		for i, claim := range claims {
			before[i] = claim
			before[i].Keys = slices.Clone(claim.Keys)
		}
		got, err := hostKeyedReservations(capabilityBinding{reservations: claims}, nil, nil)
		want := []prerequisites.HostReservation{sharedMedia("media:boot.iso", "media:dvd.iso"),
			{Context: testContextName, Kind: "os-install", Service: "node", Keys: []string{"path:/srv/node"}}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("shared claim composition = %+v (%v), want %+v", got, err, want)
		}
		got[0].Keys[0] = "changed"
		if !reflect.DeepEqual(claims, before) {
			t.Fatal("composition changed or retained a mutable alias of its inputs")
		}
	}
}

func TestSharedReservationsRefuseConflictingIdentityMetadataBeforeEffects(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"exclusive", "mixed", "object kind", "releasing"} {
		t.Run(field, func(t *testing.T) {
			claims := []prerequisites.HostReservation{sharedMedia("media:boot.iso"), sharedMedia("media:dvd.iso")}
			switch field {
			case "exclusive":
				claims[0].Shared, claims[1].Shared = false, false
			case "mixed":
				claims[1].Shared = false
			case "object kind":
				claims[1].ObjectKind = "Machine"
			case "releasing":
				claims[1].Releasing = true
			}
			h := newHarness(t, "alpha")
			h.capability.reservations = claims
			before := previewEffects(h)
			for range 2 {
				_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
				if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "lifecycle.state" {
					t.Fatalf("conflicting reservation metadata did not refuse preview: %+v", found)
				}
			}
			if after := previewEffects(h); !slices.Equal(before, after) {
				t.Fatalf("refused preview changed state: %v -> %v", before, after)
			}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil || len(h.capability.applies) != 0 || len(h.workspace.reservations) != 0 {
				t.Fatal("conflicting reservation metadata reached publication or effects")
			}
		})
	}
}

func TestPreparationAdvancesWithOneSharedMediaReservation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, "inventory")
	h.capability.reservations = []prerequisites.HostReservation{sharedMedia("media:boot.iso")}
	if _, err := h.service.Prepare(ctx, requestPreparation()); err != nil {
		t.Fatal(err)
	}
	h.capability.definitions = append(h.capability.definitions, definition("installation"))
	h.capability.reservations = append(h.capability.reservations, sharedMedia("media:dvd.iso", "media:boot.iso"))
	for range 2 {
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		if want := []prerequisites.HostReservation{sharedMedia("media:boot.iso", "media:dvd.iso")}; !reflect.DeepEqual(h.workspace.reservations, want) {
			t.Fatalf("preparation advancement did not retain exactly one complete claim: %+v", h.workspace.reservations)
		}
	}
	if !slices.Equal(h.capability.applies, []string{"inventory", "installation"}) {
		t.Fatalf("completed discovery ran again: %v", h.capability.applies)
	}
}

func TestSharedReservationsKeepEverySSHHostQualifierAfterComposition(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, nestedDefinitions())
	h.service.compiler = testCompiler{state: withControllerAddress()}
	h.capability.reservations = []prerequisites.HostReservation{sharedMedia("media:boot.iso"), sharedMedia("media:dvd.iso")}
	claim := prerequisites.HostReservation{Context: testContextName, Kind: "host-use", Service: "files", Shared: true, Keys: []string{"path:/srv/files"}}
	h.capability.sshClaims = []SSHReservation{sshClaim("services", claim), sshClaim("storage", claim)}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	want := []prerequisites.HostReservation{
		{Context: testContextName, Kind: "host-use", Service: "files", Shared: true, Keys: []string{
			"ssh/192.0.2.10:22/path:/srv/files", "ssh/192.0.2.20:22/path:/srv/files",
			pinnedQualifier("services") + "path:/srv/files", pinnedQualifier("storage") + "path:/srv/files"}},
		sharedMedia("media:boot.iso", "media:dvd.iso"),
	}
	slices.Sort(want[0].Keys)
	if !reflect.DeepEqual(h.workspace.reservations, want) {
		t.Fatalf("host qualification was lost or attached to another claim: %+v", h.workspace.reservations)
	}
}

func TestSharedReservationsPreserveContextAndMetadataAndBoundTheUnion(t *testing.T) {
	t.Parallel()
	claim := sharedMedia("media:boot.iso")
	claim.ObjectKind, claim.Releasing = "Machine", true
	other := claim
	other.Context = "other"
	got, err := hostKeyedReservations(capabilityBinding{reservations: []prerequisites.HostReservation{other, claim, claim}}, nil, nil)
	if err != nil || !reflect.DeepEqual(got, []prerequisites.HostReservation{claim, other}) {
		t.Fatalf("composition changed context or matching metadata: %+v (%v)", got, err)
	}
	claims := []prerequisites.HostReservation{sharedMedia(), sharedMedia()}
	for i := range prerequisites.MaxPublishedReservationKeys {
		claims[i%2].Keys = append(claims[i%2].Keys, fmt.Sprintf("media:image-%03d.iso", i))
	}
	if _, err := hostKeyedReservations(capabilityBinding{reservations: claims}, nil, nil); err != nil {
		t.Fatal("the exact key bound refused:", err)
	}
	claims = append(claims, sharedMedia("media:overflow.iso"))
	if _, err := hostKeyedReservations(capabilityBinding{reservations: claims}, nil, nil); err == nil {
		t.Fatal("composition exceeded the published key bound")
	}
	if _, err := hostKeyedReservations(capabilityBinding{reservations: []prerequisites.HostReservation{sharedMedia("media:boot.iso", "media:boot.iso")}}, nil, nil); err == nil {
		t.Fatal("composition hid duplicate keys within one contribution")
	}
}
