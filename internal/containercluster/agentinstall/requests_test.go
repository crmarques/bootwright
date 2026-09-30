package agentinstall

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
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

// An install request frozen by the build before physical nodes froze their
// hardware is not read as this one, so its apply, and even its destroy, run
// only on the build that registered it.
func TestAnInstallRequestOfThePreviousVersionRefuses(t *testing.T) {
	_, install, _ := onlyRequests(t, singleNodeCatalog())
	install.Version = "cluster-install-agent-v2"
	data, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeInstallRequest(data); err == nil {
		t.Fatal("an install request of the previous version was read as this one")
	} else if code := refusalCode(t, err); code != "lifecycle.state" {
		t.Fatalf("refusal = %s", code)
	}
}

// Planning places both blocks on the controller arm alone: connection local
// and a Machine name, nothing else. A frozen request placed anywhere else, or
// carrying any other placement field, refuses through its apply, its destroy,
// both observations and its removal planning before an adapter runs, so no
// run crosses the adapter with a placement's identity or host key.
func TestAFrozenPlacementOffTheControllerRefusesEveryVerb(t *testing.T) {
	for name, placement := range map[string]machine.Placement{
		"an SSH placement": {
			Address: "192.0.2.2", Connection: machine.ConnectionSSH, KnownHostsRef: "hv-01-host-key",
			Machine: "hv-01", Port: 22, PrivateKeyRef: "hv-01-key", User: "root",
		},
		"a local placement with SSH access": {
			Connection: machine.ConnectionLocal, KnownHostsRef: "hv-01-host-key", Machine: "controller", PrivateKeyRef: "hv-01-key",
		},
		"a local placement with an address": {Address: "192.0.2.2", Connection: machine.ConnectionLocal, Machine: "controller"},
		"a local placement with a port":     {Connection: machine.ConnectionLocal, Machine: "controller", Port: 22},
		"a local placement with a user":     {Connection: machine.ConnectionLocal, Machine: "controller", User: "root"},
		"a local placement that escalates":  {Connection: machine.ConnectionLocal, Machine: "controller", SudoPasswordRef: "controller-sudo"},
		"a local placement naming no host":  {Connection: machine.ConnectionLocal},
		"no connection":                     {Machine: "controller"},
	} {
		t.Run(name, func(t *testing.T) {
			media, mediaRequest := mediaExecution(t, testDigest)
			mediaRequest.Placement = placement
			media.Block.Request = frozen(t, mediaRequest)
			install, installRequest := installExecution(t, singleNodeCatalog(), testDigest)
			installRequest.Placement = placement
			install.Block.Request = frozen(t, installRequest)
			runner := &fakeRunner{}
			mediaCapability, installCapability := NewMedia(runner), NewInstall(runner)
			ctx := context.Background()
			for verb, attempt := range map[string]func() error{
				"media apply":           func() error { _, err := mediaCapability.Apply(ctx, media); return err },
				"media destroy":         func() error { _, err := mediaCapability.Destroy(ctx, media); return err },
				"media observation":     func() error { _, err := mediaCapability.Observe(ctx, media); return err },
				"media removal reading": func() error { _, err := mediaCapability.ObserveRemoval(ctx, media); return err },
				"media removal":         func() error { _, err := mediaCapability.Removal(ctx, media.Block); return err },
				"install apply":         func() error { _, err := installCapability.Apply(ctx, install); return err },
				"install destroy":       func() error { _, err := installCapability.Destroy(ctx, install); return err },
				"install observation":   func() error { _, err := installCapability.Observe(ctx, install); return err },
				"install removal reading": func() error {
					_, err := installCapability.ObserveRemoval(ctx, install)
					return err
				},
				"install removal": func() error { _, err := installCapability.Removal(ctx, install.Block); return err },
			} {
				subject, _, _ := strings.Cut(verb, " ")
				reported := diagnostics.Of(attempt())
				if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
					reported[0].Message != "the frozen cluster "+subject+" request is placed off the controller" {
					t.Fatalf("%s: refusal = %#v", verb, reported)
				}
			}
			if len(runner.requests) != 0 {
				t.Fatalf("a request placed off the controller reached the adapter: %+v", runner.requests)
			}
		})
	}
}

func frozen(t *testing.T, request interface{ Canonical() ([]byte, error) }) []byte {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return canonical
}

// A media request frozen by the build before its install configuration named
// the additional trust bundles is not read as this one, so its apply, and even
// its destroy, run only on the build that registered it.
func TestAMediaRequestOfThePreviousVersionRefuses(t *testing.T) {
	media, _, _ := onlyRequests(t, externalCatalog())
	media.Version = "cluster-media-agent-v4"
	data, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMediaRequest(data); err == nil {
		t.Fatal("a media request of the previous version was read as this one")
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
