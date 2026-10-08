package installation

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/managedos/installation -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares canonical JSON bytes with testdata/<name>.golden,
// which holds them indented for review. Indenting is lossless, so comparing
// the indented form byte for byte compares the canonical bytes exactly.
func matchesGolden(t *testing.T, name string, canonical []byte) {
	t.Helper()
	var indented bytes.Buffer
	if err := json.Indent(&indented, canonical, "", "  "); err != nil {
		t.Fatalf("%s: the canonical bytes are not JSON: %v", name, err)
	}
	indented.WriteByte('\n')
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, indented.Bytes()) {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), indented.String()), firstDifference(string(want), indented.String()))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

// A physical installation that imports the server's certificate through a
// controller anchored by its own bundle publishes its installer image only
// beneath its private subtree, so it freezes no public image. It is pinned
// from a literal so every key it freezes is in a golden the role's argument
// specification is checked against, and the golden must decode as this
// build's request.
func TestAPhysicalLiteralRequestReadsBackFromItsGolden(t *testing.T) {
	request := Request{
		Address: "198.51.100.41/24", BootMedia: Media{Name: bootImageName, SHA256: bootDigest, Size: 1105199104}, Budgets: installationBudgets,
		FleetKeyRef: "bootwright-machine-key", HostKeyPath: HostKeyPath, Hostname: "metal-01.lab.example.test",
		Identity:   Identity{Block: BlockID("metal-01"), Context: testContext, Object: "metal-01", Profile: "rhel-9-8"},
		Kickstart:  "text\n",
		MarkerPath: MarkerPath,
		Placement:  machineref.Placement{Connection: machineref.ConnectionLocal, Machine: "controller"},
		Private:    &Publication{Path: "/srv/private/os/metal-01", URL: "https://artifacts.lab.example.test/private/os/metal-01"},
		Target: Target{
			Channel: substrate.ChannelDeliveredKey,
			Controller: Controller{
				CredentialsRef: "lab-bmc-credentials", Endpoint: "https://bmc-01.lab.example.test/redfish/v1/Systems/1",
				TLSVerify: true, TrustBundleRef: "lab-bmc-ca",
				VirtualMedia: VirtualMedia{RemoveCertificate: true, Trust: substrate.TrustImportCertificate},
			},
			Hardware:   &Hardware{Interfaces: []Interface{{MACAddress: "52:54:00:9a:1b:01", Name: "enp1s0"}}, RootDevice: "/dev/sda"},
			HostKeyRef: "metal-01-host-key", HostKeyType: "ed25519", Physical: true, Substrate: substrate.ArmBaremetal,
		},
		TLSCertificateRef: "lab-artifacts-tls",
		User:              installUser,
		Version:           requestVersion,
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatalf("freezing: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "request-physical-literal", canonical)
	golden, err := os.ReadFile(filepath.Join("testdata", "request-physical-literal.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, golden); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(compact.Bytes())
	if err != nil {
		t.Fatalf("decoding the golden: %v", diagnostics.Of(err))
	}
	if decoded.Target.Controller.TrustBundleRef != "lab-bmc-ca" || !slices.Contains(decoded.SecretReferences(), "lab-bmc-ca") {
		t.Fatalf("the golden lost its controller bundle: %+v", decoded.Target.Controller)
	}
}

// The frozen request is what a plan's digest covers and what the adapter
// reads, Kickstart included, so every byte of it is contract. Each request is
// compared with its golden and still decodes as the version this build writes.
// Each is frozen at the fixture's store records, as a plan freezes it, so its
// golden carries every image's recorded size and SHA-256. The checksummed case
// declares its boot media digest in the prefixed, uppercase form admission
// accepts, which matches the record and so freezes exactly what the plain case
// freezes; a hosted tree declares no digest. The site-services case
// resolves names and time through external services at their declared
// addresses. The SSH-placed case puts the artifact server on a host declaring
// every field a placement freezes. That shape is refused before registration,
// because the installer image is built on the controller, so no plan freezes
// it; it is pinned only for the request's SSH placement arm, which the role's
// argument specification still admits.
func TestTheFrozenRequestsMatchTheirGoldens(t *testing.T) {
	checksummed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
		text("checksum", "SHA256:"+strings.ToUpper(strings.Repeat("0123456789abcdef", 4))),
	))
	sshPlaced := labCatalog(servicesHost(), artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2")))
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason: "a managed-OS installation builds and publishes its installer image on the controller, and ArtifactServer/lab-artifacts is placed on Machine/services",
		Remediation: "place ArtifactServer/lab-artifacts on the controller Machine, or select a server placed there in " +
			"spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on MachineInstallProfile/rhel-9-8"}}
	if refused := Refusals(sshPlaced); !slices.Equal(refused, want) {
		t.Fatalf("the SSH-placed shape is refused by %+v, want only the off-controller image server", refused)
	}
	for name, catalog := range map[string]api.Catalog{
		"lab-rhel": labCatalog(), "lab-rhel-checksummed": labCatalog(checksummed),
		"lab-rhel-ssh-placed": sshPlaced, "lab-rhel-site-services": siteServicesCatalog(),
	} {
		t.Run(name, func(t *testing.T) {
			request := pinnedRequest(t, catalog)
			canonical, err := request.Canonical()
			if err != nil {
				t.Fatalf("freezing: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "request-"+name, canonical)
			if _, err := DecodeRequest(canonical); err != nil {
				t.Fatalf("decoding: %v", diagnostics.Of(err))
			}
		})
	}
}

// The protocol plugin publishes evidence through a channel that encodes with
// sorted keys and no spaces (plugins/module_utils/controller_channel.py), which
// Freeze proves is also how Go encodes it. Each golden is what
// managedos_install_protocol.py publishes for one run over the lab-rhel
// request, whose profile hosts a package tree, and the reading that run's
// resolution takes accepts exactly those bytes.
func TestEvidenceMatchesItsGoldens(t *testing.T) {
	request := pinnedRequest(t, labCatalog())
	digest := strings.Repeat("1", 64)
	marker, err := MarkerFor(request, digest)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		evidence Evidence
		validate func([]byte) error
	}{
		// presence() after an apply: the guest answers with the frozen marker
		// on its own key, runs with its media ejected, and the image and the
		// whole tree, extracted from the frozen DVD, are published.
		"completed": {
			Evidence{
				Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: string(marker),
				Postcondition: true, Power: "On", Reachable: true, Request: digest, Tree: true, TreeContent: true,
				TreeIdentity: dvdDigest,
			},
			func(data []byte) error { return ValidatePresence(data, request, digest, string(marker)) },
		},
		// absence() once every published path is gone.
		"removed": {
			Evidence{Absent: true, Postcondition: true, Request: digest},
			func(data []byte) error { return ValidateAbsence(data, digest) },
		},
		// An observed presence() for an apply stopped before it published
		// anything: the controller reports the machine off and nothing else
		// is found, which is positive no effect.
		"no-effect": {
			Evidence{Address: request.Address, Power: "Off", Request: digest},
			func(data []byte) error { return ValidateNoEffect(data, digest) },
		},
		// A removal's observation reads the published content alone, and a
		// removal stopped while it deleted the tree left the directory
		// without its .treeinfo: content the removal still takes back.
		"removal-partial-tree": {
			Evidence{Address: request.Address, Request: digest, TreeContent: true},
			func(data []byte) error { return ValidateWithdrawalUnfinished(data, digest) },
		},
		// A removal's observation after an apply was killed while it
		// extracted the tree: the staging copy beside the tree and the work
		// area are left, and the removal still takes both back.
		"removal-remnants": {
			Evidence{Address: request.Address, Request: digest, TreeStaging: true, Work: true},
			func(data []byte) error { return ValidateWithdrawalUnfinished(data, digest) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "installation evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "evidence-"+name, canonical)
			if err := test.validate(canonical); err != nil {
				t.Fatalf("the %s evidence was refused: %v", name, diagnostics.Of(err))
			}
		})
	}
}
