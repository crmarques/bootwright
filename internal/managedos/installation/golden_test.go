package installation

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

// The frozen request is what a plan's digest covers and what the adapter
// reads, Kickstart included, so every byte of it is contract. Each request is
// compared with its golden and still decodes as the version this build writes.
// The checksummed case declares its boot media digest in the prefixed,
// uppercase form admission accepts, so its golden pins the canonical form the
// request freezes; a hosted tree declares no digest. The SSH-placed case puts
// the artifact server on a host declaring every SSH access field, so its golden
// pins each key of the placement's SSH arm.
func TestTheFrozenRequestsMatchTheirGoldens(t *testing.T) {
	checksummed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
		text("checksum", "SHA256:"+strings.ToUpper(strings.Repeat("0123456789abcdef", 4))),
	))
	sshPlaced := artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2"))
	for name, catalog := range map[string]api.Catalog{
		"lab-rhel": labCatalog(), "lab-rhel-checksummed": labCatalog(checksummed),
		"lab-rhel-ssh-placed": labCatalog(servicesHost(), sshPlaced),
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := onlyRequest(t, catalog)
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
