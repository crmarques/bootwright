package agentinstall

import (
	"bytes"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A frozen request is read back exactly as it was written, because the digest
// a plan records identifies those bytes and nothing else.
func TestFrozenRequestsRoundTripExactly(t *testing.T) {
	media, install, _ := onlyRequests(t, singleNodeCatalog())
	mediaBytes, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decodedMedia, err := DecodeMediaRequest(mediaBytes)
	if err != nil {
		t.Fatalf("decoding the media request: %v", diagnostics.Of(err))
	}
	roundTripped, err := decodedMedia.Canonical()
	if err != nil || !bytes.Equal(roundTripped, mediaBytes) {
		t.Fatalf("media request did not round trip: %v", err)
	}
	installBytes, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decodedInstall, err := DecodeInstallRequest(installBytes)
	if err != nil {
		t.Fatalf("decoding the install request: %v", err)
	}
	roundTripped, err = decodedInstall.Canonical()
	if err != nil || !bytes.Equal(roundTripped, installBytes) {
		t.Fatalf("install request did not round trip: %v", err)
	}
}

// A request written by another version is not read as this one, because its
// shape is not the shape this build froze.
func TestAForeignRequestVersionRefuses(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	media.Version = "cluster-media-agent-v0"
	data, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMediaRequest(data); err == nil {
		t.Fatal("a request of another version was read as this one")
	} else if code := refusalCode(t, err); code != "lifecycle.state" {
		t.Fatalf("refusal = %s", code)
	}
}

// The media block claims the two host resources it writes into, so a second
// context refuses rather than publishing over them.
func TestMediaReservationsClaimTheImageTreeAndWorkArea(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	keys := media.ReservationKeys()
	for _, want := range []string{
		"path:/var/lib/bootwright-clusters/lab/sno",
		"path:/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public/private/clusters/sno",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("reservations = %v, missing %q", keys, want)
		}
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("reservations are unordered: %v", keys)
	}
}

// Every declaration an attempt needs bound is named before the operation
// registers, and nothing else is.
func TestSecretReferencesNameEveryBoundDeclaration(t *testing.T) {
	media, install, _ := onlyRequests(t, singleNodeCatalog())
	if got := media.SecretReferences(); !slices.Equal(got, []string{
		"artifact-server-tls", "openshift-pull-secret", "sno-cluster-admin-ssh-key",
	}) {
		t.Fatalf("media secrets = %v", got)
	}
	if got := install.SecretReferences(); !slices.Equal(got, []string{"lab-bmc-credentials"}) {
		t.Fatalf("install secrets = %v", got)
	}
}

// The machines an installation boots are named in the order it boots them, so
// the rendezvous host is reached first.
func TestTheInstallRequestNamesItsMachinesInBootOrder(t *testing.T) {
	_, install, _ := onlyRequests(t, compactCatalog())
	if got := install.Machines(); !slices.Equal(got, []string{"ocp-01", "ocp-02", "ocp-03"}) {
		t.Fatalf("machines = %v", got)
	}
	if install.Physical() {
		t.Fatal("a cluster of virtual machines reported operator-owned hardware")
	}
}
