package agentinstall

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/containercluster/agentinstall -run Golden -update
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

// The frozen requests are what a plan's digest covers and what the adapter
// reads, so every byte of them is contract. Each fixture's pair is compared
// with its goldens and still decodes as the version this build writes.
func TestFrozenRequestsMatchTheirGoldens(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{
		"lab-sno": singleNodeCatalog(), "compact": compactCatalog(), "external": externalCatalog(),
		"hints": hintsCatalog(),
	} {
		t.Run(name, func(t *testing.T) {
			media, install, _ := onlyRequests(t, catalog)
			mediaBytes, err := media.Canonical()
			if err != nil {
				t.Fatalf("freezing the media request: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "media-request-"+name, mediaBytes)
			if _, err := DecodeMediaRequest(mediaBytes); err != nil {
				t.Fatalf("decoding the media request: %v", diagnostics.Of(err))
			}
			installBytes, err := install.Canonical()
			if err != nil {
				t.Fatalf("freezing the install request: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "install-request-"+name, installBytes)
			if _, err := DecodeInstallRequest(installBytes); err != nil {
				t.Fatalf("decoding the install request: %v", diagnostics.Of(err))
			}
		})
	}
}

// Selection refuses a physical cluster, so the nodes its install request would
// freeze are compared beneath that refusal, derived as
// TestPhysicalNodesAreFrozenAsOperatorOwnedHardware derives them.
func TestFrozenPhysicalNodesMatchTheirGolden(t *testing.T) {
	catalog := physicalCatalog()
	declared, _ := catalog.Find(api.ContainerCluster, "metal")
	nodes, err := nodeProjections(catalog, declared, testContext, "controller", &Requirements{})
	if err != nil {
		t.Fatalf("projecting: %v", diagnostics.Of(err))
	}
	canonical, err := reconciliation.Freeze(struct {
		Nodes []Node `json:"nodes"`
	}{frozenNodes(nodes)}, "cluster install")
	if err != nil {
		t.Fatalf("freezing the nodes: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "install-nodes-physical", canonical)
}

// The protocol plugins publish evidence through a channel that encodes with
// sorted keys and no spaces (plugins/module_utils/controller_channel.py), which
// Freeze proves is also how Go encodes it. Each golden is what the plugin
// publishes for one observation of the lab-sno cluster, and the validator of
// that state accepts exactly those bytes.
func TestMediaEvidenceMatchesItsGoldens(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	// Each case names the containercluster_media_protocol.py call it mirrors.
	for name, test := range map[string]struct {
		evidence MediaEvidence
		validate func([]byte) error
	}{
		// presence() after a build: the image and work area exist, recorded
		// against this request by the declared release's installer.
		"completed": {
			MediaEvidence{Image: true, Inputs: testDigest, Installer: "4.21.15", Postcondition: true, Request: testDigest, Work: true},
			func(data []byte) error { return ValidateMediaPresence(data, media, testDigest) },
		},
		// absence() once both are gone.
		"removed": {
			MediaEvidence{Absent: true, Postcondition: true, Request: testDigest},
			func(data []byte) error { return ValidateMediaAbsence(data, testDigest) },
		},
		// An observed presence() finding only the work area.
		"partial": {
			MediaEvidence{Request: testDigest, Work: true},
			func(data []byte) error { return ValidateMediaPartial(data, testDigest) },
		},
		// An observed presence() finding nothing.
		"no-effect": {
			MediaEvidence{Request: testDigest},
			func(data []byte) error { return ValidateMediaNoEffect(data, testDigest) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "cluster media evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "media-evidence-"+name, canonical)
			if err := test.validate(canonical); err != nil {
				t.Fatalf("the %s evidence was refused: %v", name, diagnostics.Of(err))
			}
		})
	}
}

// Each install evidence golden is what containercluster_install_protocol.py
// publishes from the state its role resolves for the lab-sno cluster: missing
// lists node names, media, ownMedia and powered list Machine names, and every
// list is present even when empty.
func TestInstallEvidenceMatchesItsGoldens(t *testing.T) {
	_, install, _ := onlyRequests(t, singleNodeCatalog())
	for name, test := range map[string]struct {
		evidence InstallEvidence
		validate func([]byte) error
	}{
		// The cluster answers through this build's anchor, whole, reporting its
		// installation completed at the declared release, with its media ejected.
		"completed": {
			InstallEvidence{
				Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity, Media: []string{}, Missing: []string{},
				OwnMedia: []string{}, Postcondition: true, Powered: []string{"sno-01"}, Release: "4.21.15",
				Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallPresence(data, install, testDigest) },
		},
		// The same completion by an apply whose wait found the installer's own
		// kubeconfig cut short and put the kept copy back in its place first.
		"completed-restored": {
			InstallEvidence{
				Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity, Media: []string{}, Missing: []string{},
				OwnMedia: []string{}, Postcondition: true, Powered: []string{"sno-01"}, Release: "4.21.15",
				Request: testDigest, Restored: true,
			},
			func(data []byte) error { return ValidateInstallPresence(data, install, testDigest) },
		},
		// A removal ejects the media and retains the cluster, which still answers.
		"removed": {
			InstallEvidence{
				Absent: true, Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity,
				Media: []string{}, Missing: []string{}, OwnMedia: []string{}, Postcondition: true,
				Powered: []string{"sno-01"}, Release: "4.21.15", Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallAbsence(data, testDigest) },
		},
		// This build's cluster answers, at the declared release and whole, while
		// it is still installing and its node still presents the media: an
		// apply interrupted during the installation wait.
		"partial": {
			InstallEvidence{
				Cluster: anchorIdentity, Identity: anchorIdentity, Media: []string{"sno-01"}, Missing: []string{},
				OwnMedia: []string{"sno-01"}, Powered: []string{"sno-01"}, Release: "4.21.15", Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallPartial(data, testDigest) },
		},
		// Nothing answers yet, and the node runs presenting the image this
		// cluster published: an apply interrupted during boot or the bootstrap
		// wait, before the API answers. The node is not yet in the cluster.
		"partial-booted": {
			InstallEvidence{
				Identity: anchorIdentity, Media: []string{"sno-01"}, Missing: []string{"master-0"},
				OwnMedia: []string{"sno-01"}, Powered: []string{"sno-01"}, Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallPartial(data, testDigest) },
		},
		// A removal's observation reads each node's media and none of the
		// cluster, so nothing answers and every declared node reads missing,
		// while the node still presents an image this cluster did not publish:
		// a removal part way through, which ejecting again converges (D27).
		"release-partial-foreign": {
			InstallEvidence{
				Identity: anchorIdentity, Media: []string{"sno-01"}, Missing: []string{"master-0"},
				OwnMedia: []string{}, Powered: []string{"sno-01"}, Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallReleasePartial(data, install, testDigest) },
		},
		// The image was built, so its anchor is known, but nothing answers and
		// no node runs: every declared node is missing.
		"no-effect": {
			InstallEvidence{
				Identity: anchorIdentity, Media: []string{}, Missing: []string{"master-0"},
				OwnMedia: []string{}, Powered: []string{}, Request: testDigest,
			},
			func(data []byte) error { return ValidateInstallNoEffect(data, testDigest) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "cluster install evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "install-evidence-"+name, canonical)
			if err := test.validate(canonical); err != nil {
				t.Fatalf("the %s evidence was refused: %v", name, diagnostics.Of(err))
			}
		})
	}
}
