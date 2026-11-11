package lifecycle

import (
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

const installedKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func installedPlacement() machineref.Placement {
	return machineref.Placement{Address: "198.51.100.20", Connection: machineref.ConnectionInstalled, Machine: "mirror", Port: 22, PrivateKeyRef: "fleet-key", User: "bootwright"}
}

// An installed placement is lent the host key its installation proved, read
// from the original apply's records and, on the first apply, from what this
// attempt's dependencies proved; every other arm is left as it is, and an
// unbound reader or a refused proof lends nothing.
func TestAnInstalledPlacementIsPinnedToTheHostKeyItsInstallationProved(t *testing.T) {
	original, attempt := []BlockEvidence{{Object: "original"}}, []BlockEvidence{{Object: "attempt"}}
	var read []BlockEvidence
	proved := func(proofs []BlockEvidence, machine, address string) (trust.HostKey, error) {
		read = proofs
		if machine != "mirror" || address != "198.51.100.20" {
			t.Fatalf("the proof was read for %s at %s", machine, address)
		}
		return trust.ParseAuthorizedKey(installedKey)
	}
	for name, tc := range map[string]struct {
		execution Execution
		want      []BlockEvidence
	}{
		"a continuation":  {Execution{ApplyProved: original, Proved: attempt}, original},
		"the first apply": {Execution{Proved: attempt}, attempt},
	} {
		t.Run(name, func(t *testing.T) {
			bound := map[string]secrets.Material{"fleet-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("KEY")})}
			tc.execution.Material = bound
			execution, placement, err := PinInstalledHost(tc.execution, installedPlacement(), proved)
			if err != nil || len(read) != 1 || read[0].Object != tc.want[0].Object || placement.KnownHostsRef != InstalledHostKeyMaterial {
				t.Fatalf("pinned %+v from %+v (%v)", placement, read, err)
			}
			line, _ := execution.Material[InstalledHostKeyMaterial].Part(secrets.ValuePart)
			if string(line) != "198.51.100.20 "+installedKey+"\n" || len(bound) != 1 {
				t.Fatalf("lent known-hosts entry %q; the caller's material holds %d entries", line, len(bound))
			}
		})
	}
	ssh := installedPlacement()
	ssh.Connection, ssh.KnownHostsRef = machineref.ConnectionSSH, "host-key"
	if _, placement, err := PinInstalledHost(Execution{}, ssh, nil); err != nil || placement != ssh {
		t.Fatalf("an SSH placement was changed to %+v (%v)", placement, err)
	}
	if _, _, err := PinInstalledHost(Execution{}, installedPlacement(), nil); err == nil {
		t.Fatal("an installed placement was admitted with no proof reader")
	}
	refused := errors.New("refused")
	failing := func([]BlockEvidence, string, string) (trust.HostKey, error) { return trust.HostKey{}, refused }
	if execution, _, err := PinInstalledHost(Execution{}, installedPlacement(), failing); !errors.Is(err, refused) || execution.Material != nil {
		t.Fatalf("a refused proof lent %+v (%v)", execution.Material, err)
	}
}

// A claim on a Machine this context installs is qualified by its SSH address
// and port alone, because its host key exists only once it is installed.
func TestAnInstalledHostsClaimsAreQualifiedByItsAddressAlone(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue())})
	reservation := prerequisites.HostReservation{Context: "lab", Kind: "artifact-server", Service: "mirror", Keys: []string{"unit:bootwright-lab-artifacts-mirror"}}
	published, keyed, err := publishedSSHClaims(catalog, "controller", []SSHReservation{{Machine: "mirror", Address: "198.51.100.20", Port: 22, Installed: true, Reservation: reservation}})
	if err != nil || len(keyed) != 0 || len(published) != 1 ||
		!slices.Equal(published[0].Keys, prerequisites.QualifiedKeys(machineref.SSHReservationQualifier("198.51.100.20", 22), reservation.Keys)) {
		t.Fatalf("published %+v, keyed %+v (%v)", published, keyed, err)
	}
	if _, _, err := publishedSSHClaims(catalog, "controller", []SSHReservation{{Machine: "mirror", Address: "198.51.100.20", Port: 22, Reservation: reservation}}); err == nil {
		t.Fatal("an SSH claim with no bound host key was admitted")
	}
}
