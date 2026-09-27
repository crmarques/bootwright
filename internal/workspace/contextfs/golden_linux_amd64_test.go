//go:build linux && amd64

package contextfs

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/workspace/contextfs -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one compact JSON document with testdata/<name>.golden,
// which holds it indented for review. A terminated format ends in exactly one
// LF, which is stripped before the comparison; any other format ends in no
// whitespace at all. Indenting is lossless only for compact input, so the body
// must equal its json.Compact form before its indented golden can stand for
// the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		trimmed, found := bytes.CutSuffix(data, []byte("\n"))
		if !found || bytes.HasSuffix(trimmed, []byte("\n")) {
			t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data[max(0, len(data)-16):])
		}
		body = trimmed
	} else if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	matchesTextGolden(t, name, indented.Bytes())
}

// matchesTextGolden compares bytes that are not one compact JSON document, such
// as JSON Lines, YAML or indented JSON, with testdata/<name>.golden byte for
// byte. git diff --check refuses a line ending in a space or tab and a blank
// line at the end of a file, so a golden holding either could never be
// committed.
func matchesTextGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	text := string(data)
	if text == "\n" || strings.HasSuffix(text, "\n\n") {
		t.Fatalf("%s: the bytes end in a blank line, which git diff --check refuses", name)
	}
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
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

// The records in the specs/contexts.md table are compact canonical JSON with
// one final LF, and every reader refuses any other bytes, so each is compared
// with its golden as encodeRecord writes it and read back as the repository
// reads it: decodeRecord, which hands a registry to decodeRegistry, then the
// record's own validator.
func TestWorkspaceRecordsMatchTheirGoldens(t *testing.T) {
	const (
		input       = "/home/operator/platform"
		environment = "/home/operator/platform/environments/lab"
		revision    = "rev-0123456789abcdef0123456789abcdef"
	)
	populated := emptyRegistry()
	populated.Contexts = []contexts.Record{
		// An interrupted init holds no input and no directory yet.
		{Name: "edge", Mode: contexts.Initializing, SecretStoreType: "local-keyring"},
		{Name: "lab", EnvironmentDirectory: environment, Revision: revision, Mode: contexts.Ready,
			SecretStoreType: "local-keyring", DirectoryDevice: 2049, DirectoryInode: 131074},
		{Name: "retired", EnvironmentDirectory: environment, Revision: "rev-fedcba9876543210fedcba9876543210",
			Mode: contexts.Deleting, SecretStoreType: "local-keyring", DirectoryDevice: 2049, DirectoryInode: 131075},
	}
	controlled := cloneRegistry(populated)
	controlled.Controller = contexts.ControllerDescriptor{Version: 1, Mode: "ready", DirectoryDevice: 2049, DirectoryInode: 131072}
	for name, registry := range map[string]contexts.Registry{
		"registry-empty": emptyRegistry(), "registry-contexts": populated, "registry-controller": controlled,
	} {
		data, err := encodeRecord(registry, maxRegistry)
		if err != nil {
			t.Fatalf("%s: %v", name, diagnostics.Of(err))
		}
		matchesGolden(t, name, data, true)
		var read contexts.Registry
		if err := decodeRecord(data, maxRegistry, &read); err != nil {
			t.Fatalf("%s: decoding: %v", name, diagnostics.Of(err))
		}
		if err := validateRegistry(read); err != nil || !reflect.DeepEqual(read, registry) {
			t.Fatalf("%s read back as %+v (%v)", name, read, err)
		}
	}

	data, err := encodeRecord(reservation{Version: ReservationVersion, Name: "lab"}, maxRecord)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "reservation", data, true)
	var reserved reservation
	if err := decodeRecord(data, maxRecord, &reserved); err != nil || validateReservation(reserved, "lab") != nil {
		t.Fatalf("the reservation read back as %+v (%v)", reserved, err)
	}

	// Publish builds a manifest this way, then names the revision it reserved.
	frozen, _, err := prepareManifest("lab", environment, desiredstate.Sources{
		Roots: []string{input},
		Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile(environment+"/environment.yaml", []byte("kind: Environment\n")),
		},
		Markers: []desiredstate.SourceFile{
			desiredstate.NewSourceFile(input+"/add-ons/_store/example/.bootwright-addon", []byte("example\n")),
		},
	})
	if err != nil {
		t.Fatalf("preparing the manifest: %v", diagnostics.Of(err))
	}
	frozen.Revision = revision
	data, err = encodeRecord(frozen, maxManifest)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "manifest", data, true)
	var read manifest
	if err := decodeRecord(data, maxManifest, &read); err != nil {
		t.Fatalf("decoding the manifest: %v", diagnostics.Of(err))
	}
	if err := validateManifest(read, "lab", revision, environment); err != nil || !reflect.DeepEqual(read, frozen) {
		t.Fatalf("the manifest read back as %+v (%v)", read, err)
	}
}

// goldenResolution is syntheticResolution with the host libraries its private
// Python runs against and a native transaction that installs one root package
// and upgrades another, so the frozen definition also reaches the installed
// file and link shapes and the before-identity only a replacement carries. A
// resolved definition never carries tools or runtime files, so neither can.
func goldenResolution(t *testing.T) prerequisites.Definition {
	t.Helper()
	base := prerequisites.CloneDefinition(syntheticResolution(t))
	bootstrap := *base.Bootstrap
	bootstrap.Execution = prerequisites.ExecutionRequirement{
		PythonExecutable: bootstrap.Execution.PythonExecutable, Loader: "/usr/lib64/ld-linux-x86-64.so.2",
		LockPath: "/usr/lib/sysimage/rpm/.rpm.lock",
		Files:    []prerequisites.InstalledFile{{Path: "/usr/lib64/libc.so.6", SHA256: strings.Repeat("d", 64)}},
		Links:    []prerequisites.InstalledLink{{Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-15.so.1"}},
		Preload:  []string{"/usr/lib64/libgcc_s.so.1"},
	}
	bootstrap, err := prerequisites.CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatalf("canonicalizing the bootstrap: %v", diagnostics.Of(err))
	}
	native := *base.Native
	roots := map[string]prerequisites.NativeIdentity{}
	for _, root := range native.Roots {
		roots[root.Package.Name] = root.Package
	}
	before := roots["podman"]
	before.Version = "1.2.2"
	native.Actions = []prerequisites.NativeAction{
		{Kind: "install", After: roots["openssh-clients"], SourceID: "openssh-clients", Reason: "root"},
		{Kind: "upgrade", Before: &before, After: roots["podman"], SourceID: "podman", Reason: "root"},
	}
	native.AfterSHA256 = strings.Repeat("f", 64)
	if native, err = prerequisites.CanonicalNativePlan(native); err != nil {
		t.Fatalf("canonicalizing the native plan: %v", err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native)
	if err != nil {
		t.Fatalf("resolving: %v", diagnostics.Of(err))
	}
	return definition
}

// The controller record is private shared-host evidence proved canonical by
// re-encoding, so its golden holds a completed setup through a proxy with its
// resolved definition, a recorded before-state, a binding, retained sources
// and definitions, both bundle reservation shapes and both a shared and an
// exclusive host reservation. The receipt's context member is always empty and
// is pinned as it encodes today (backlog F10).
func TestControllerRecordMatchesItsGolden(t *testing.T) {
	definition := goldenResolution(t)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	value.Receipt.Definition = &definition
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Sources = slices.Clone(definition.Sources)
	value.Receipt.Egress = prerequisites.SetupEgress{
		HTTPProxy: "http://proxy.example.test:3128", HTTPSProxy: "http://proxy.example.test:3128",
		NoProxy: []string{"localhost", ".example.test"},
	}
	var err error
	if value.Receipt.PlanDigest, err = prerequisites.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	value = completeControllerState(value)
	value.Receipt.Actions[0].Preparation = []byte(`{"addedSources":["synthetic-runtime"],"inventorySHA256":"` + strings.Repeat("a", 64) + `"}`)
	hostDigest, err := value.Host.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	value.Bindings = []prerequisites.ControllerBinding{{Context: "lab", Machine: "controller", HostDigest: hostDigest}}
	value.Reservations = []prerequisites.HostReservation{
		{Context: "lab", Kind: "media", Service: "media", Keys: []string{"media:rhel-9.6-x86_64-boot.iso"}, Shared: true},
		{Context: "lab", Kind: "os-install", Service: "node-01", Keys: []string{"path:/srv/lab/node-01.iso", "path:/srv/lab/rhel"}},
	}
	if value, err = retainControllerSources(prerequisites.HostState{}, value); err != nil {
		t.Fatal(err)
	}
	bundles := []controllerBundleReservation{
		{ID: strings.Repeat("0", 63) + "1", Mode: "reserved"},
		{ID: strings.Repeat("c", 64), Mode: "sealed", DirectoryDevice: 2049, DirectoryInode: 131080},
	}
	data, err := encodeRecord(controllerRecord(value, bundles), maxControllerState)
	if err != nil {
		t.Fatalf("encoding: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "controller-record", data, true)
	read, readBundles, err := decodeControllerRecord(data)
	if err != nil {
		t.Fatalf("decoding: %v", diagnostics.Of(err))
	}
	if err := validateControllerState(read); err != nil {
		t.Fatalf("validating: %v", diagnostics.Of(err))
	}
	again, err := encodeRecord(controllerRecord(read, readBundles), maxControllerState)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("the controller record read back as different bytes (%v)", err)
	}
}
