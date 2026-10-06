package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	stateencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/cli -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one JSON document with testdata/<name>.golden, which
// holds it indented for review. A terminated document is a stream's whole
// output, which the output contract ends with exactly one LF and no other. The
// document must then be compact: indenting compact JSON is lossless, so
// comparing the indented form byte for byte compares the bytes written.
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	if terminated {
		if bytes.Count(data, []byte("\n")) != 1 || !bytes.HasSuffix(data, []byte("\n")) {
			t.Fatalf("%s: a JSON document must be followed by exactly one LF and hold no other: %q", name, data)
		}
		data = data[:len(data)-1]
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatalf("%s: the output is not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), data) {
		t.Fatalf("%s: the JSON is not compact, so no indented golden can prove its bytes; %s",
			name, firstDifference(compact.String(), string(data)))
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, data, "", "  "); err != nil {
		t.Fatalf("%s: %v", name, err)
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

// matchesTextGolden compares text output with testdata/<name>.golden, which
// holds the same bytes. A golden is reviewed and kept as a text file, so the
// output must be one: nonempty, LF-terminated, with no trailing whitespace and
// no blank last line, any of which an editor or git diff --check would change.
func matchesTextGolden(t *testing.T, name string, text []byte) {
	t.Helper()
	switch {
	case len(text) == 0 || text[len(text)-1] != '\n':
		t.Fatalf("%s: the text does not end with its LF: %q", name, text)
	case bytes.HasSuffix(text, []byte("\n\n")):
		t.Fatalf("%s: the text ends with a blank line, which a golden cannot hold as itself", name)
	case bytes.Contains(text, []byte(" \n")) || bytes.Contains(text, []byte("\t\n")) || bytes.Contains(text, []byte("\r\n")):
		t.Fatalf("%s: a line ends with whitespace, which a golden cannot hold as itself", name)
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, text, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, text) {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), string(text)), firstDifference(string(want), string(text)))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as a JSON document.
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

// cliGolden is one Runner invocation over fixed application results. Standard
// output is compared with testdata/<golden>.golden, and a case without a golden
// must write nothing there. The exit status and standard error are asserted
// inline, beside the invocation they belong to.
type cliGolden struct {
	golden string
	args   string
	record func(*dispatchRecord)
	code   int
	stderr string
	// quoted holds standard output as its strconv.Quote form, for bytes a text
	// golden cannot hold as themselves.
	quoted bool
}

// path is the command path the invocation selects: its words before the first
// flag.
func (c cliGolden) path() string {
	var words []string
	for _, word := range strings.Fields(c.args) {
		if strings.HasPrefix(word, "-") {
			break
		}
		words = append(words, word)
	}
	return strings.Join(words, " ")
}

// json reports whether the invocation selects the JSON output mode.
func (c cliGolden) json() bool {
	words := strings.Fields(c.args)
	for i, word := range words {
		if word == "--output=json" || word == "--output" && i+1 < len(words) && words[i+1] == "json" {
			return true
		}
	}
	return false
}

// cliGoldens holds every case. Each record function builds its results afresh,
// because the Runner clears revealed material after every invocation.
func cliGoldens() []cliGolden {
	const (
		operationID = "op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a"
		revision    = "rev-3b8d0f5a9c1e4d7b2a6f8c0e1d3b5a7f"
		runID       = "run-9d2e4f6a8b0c1d3e5f7a9b1c3d5e7f9a"
		retiredKey  = "key-fixture-1"
		activeKey   = "key-fixture-2"
		logs        = "/var/lib/bootwright/contexts/lab/state/operations/" + operationID + "/logs"
	)
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	ready := func(id, observed string) prerequisites.Check {
		return prerequisites.Check{ID: id, Required: "qualified " + id, Observed: observed, Status: "ready"}
	}
	lab := contexts.Summary{Name: "lab", Mode: contexts.Ready, Current: true, Configured: true}
	secretContext := secretstore.Context{Name: "lab", Mode: "ready"}
	version := func(id string) *string { return &id }
	selection := secretstore.Selection{
		Type:       "local-keyring",
		Store:      secretstore.ComponentRef{ID: "local-v4", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
		KeyCustody: secretstore.ComponentRef{ID: "local-keyfile-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
	}
	deferred := diagnostic{
		Severity: "warning", Code: "api.deferred", Message: "CustomPlaybook is a reserved declaration and cannot execute.",
		Source: &diagnostics.SourceLocation{Path: "playbooks/reserved.yaml", Document: 1, Line: 5, Column: 1},
		Object: &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "CustomPlaybook", Name: "reserved"},
		Field:  "$.spec",
	}
	excluded := diagnostic{
		Severity: "warning", Code: "api.selection", Message: "cluster root is excluded by the Environment selection",
		Source: &diagnostics.SourceLocation{Path: "clusters/ocp-02/cluster.yaml", Document: 1, Line: 4, Column: 9},
		Object: &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "ContainerCluster", Name: "ocp-02"},
		Field:  "$.metadata.name", Remediation: "include the cluster in the matching Environment root selection",
	}
	blocks := func(last string) []lifecycle.BlockResult {
		return []lifecycle.BlockResult{
			{ID: "artifact-server-lab-artifacts", Description: "serve artifacts for lab-artifacts on controller", Stage: "infra-components", State: "done"},
			{ID: "substrate-host-lab-libvirt", Description: "realize the libvirt host of lab-libvirt on controller", Stage: "substrates", State: "done"},
			{ID: "machine-rhel-01", Description: "realize the virtual machine rhel-01 and its controller", Stage: "machines", State: last},
			{ID: "os-install-rhel-01", Description: "install the operating system of rhel-01", Stage: "machines", State: "pending"},
		}
	}
	operation := func(state string) *lifecycle.OperationResult {
		result := &lifecycle.OperationResult{
			Context: lifecycle.ContextIdentity{Name: "lab", Revision: revision}, Verb: "apply", Blocks: blocks(state),
			Logs: []string{"contexts/lab/state/operations/" + operationID + "/logs/operation.jsonl"}, LogLocation: logs,
			Receipt: lifecycle.Receipt{Operation: operationID, Verb: "apply", State: state, Next: "continue-apply"},
		}
		if state == "done" {
			result.Blocks[3].State, result.Receipt.Next = "done", "none"
		}
		return result
	}
	status := func() *lifecycle.StatusResult {
		// Each block reports the attempts its record counts: the failed
		// machine was retried once, and the pending one never started. The
		// failed proxy block reads its service failed, the cluster no block
		// names reads pending, and one binding covers the three Secrets.
		attempted := slices.Insert(blocks("failed"), 1, lifecycle.BlockResult{
			ID: "proxy-lab-proxy", Description: "proxy egress for lab-proxy on controller", Stage: "infra-components", State: "failed",
		})
		for index, attempts := range []int{1, 1, 1, 2, 0} {
			attempted[index].Attempts = attempts
		}
		return &lifecycle.StatusResult{
			Context:         lifecycle.ContextIdentity{Name: "lab", Revision: revision, Mode: "ready"},
			SetupChecks:     []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}, {ID: "execution-bundle", Status: "ready"}},
			Desired:         lifecycle.DesiredSummary{Revision: revision, Environment: "lab-rhel", Files: 14, Objects: 14},
			Clusters:        []lifecycle.ClusterSummary{{Name: "ocp-01", Kind: "ContainerCluster", Status: lifecycle.RealizationPending}},
			StorageClusters: []lifecycle.ClusterSummary{{Name: "ceph-01", Kind: "StorageCluster", Status: lifecycle.RealizationUnsupported}},
			Shared: []lifecycle.ServiceSummary{
				{Kind: "ArtifactServer", Name: "lab-artifacts", Machine: "controller", Status: lifecycle.RealizationDone},
				{Kind: "DNSServer", Name: "lab-dns", Machine: "controller", Status: lifecycle.RealizationUnsupported},
				{Kind: "Proxy", Name: "lab-proxy", Machine: "controller", Status: lifecycle.RealizationFailed},
			},
			Secrets: lifecycle.SecretSummary{Declared: 3, Bindings: 1},
			// The lost record refuses both the continuation and the removal,
			// so nothing is offered.
			NextSteps: []string{},
			Lifecycle: &lifecycle.LifecycleSummary{
				Operation: operationID, Verb: "apply", State: "failed", Next: "continue-apply", Blocks: attempted,
				Logs: []string{"contexts/lab/state/operations/" + operationID + "/logs/operation.jsonl"}, Executable: "1.4.0 (9f2c1ab)",
			},
			// The pending block reads so because its record was lost beside
			// the attempt that started it.
			Contradictions: []string{"os-install-rhel-01 (no block record, yet an attempt of it is recorded)"},
			LogLocation:    logs,
		}
	}
	// An apply whose machine attempt was lost and whose removal's
	// observation found a domain of the same name that this context does not
	// own: the block names why it is still unknown and what the operator does
	// about it, and both verbs observe it again.
	unresolved := func() *lifecycle.StatusResult {
		result := status()
		result.NextSteps, result.Contradictions = []string{"bootwright apply", "bootwright destroy"}, []string{}
		result.Lifecycle.State, result.Lifecycle.Next = "unknown", "resolve"
		result.Lifecycle.Blocks[3].State, result.Lifecycle.Blocks[3].Attempts = "unknown", 1
		result.Lifecycle.Blocks[3].Unresolved = &lifecycle.Unresolved{
			Reason: "domain bootwright-lab-rhel-01 on Machine hypervisor at 192.0.2.5 does not carry this context's ownership",
			Remedy: "remove or rename domain bootwright-lab-rhel-01 on Machine hypervisor at 192.0.2.5, which this context does not own",
		}
		return result
	}
	// A context no operation has touched: a binding the first apply
	// publishes, no cluster roots, and a status that offers the first
	// operation.
	idle := func() *lifecycle.StatusResult {
		return &lifecycle.StatusResult{
			Context:         lifecycle.ContextIdentity{Name: "lab", Revision: revision, Mode: "ready"},
			SetupChecks:     []lifecycle.SetupCheck{{ID: "controller-binding", Status: "pending"}, {ID: "execution-bundle", Status: "ready"}},
			Desired:         lifecycle.DesiredSummary{Revision: revision, Environment: "lab-rhel", Files: 14, Objects: 14},
			Clusters:        []lifecycle.ClusterSummary{},
			StorageClusters: []lifecycle.ClusterSummary{},
			Shared:          []lifecycle.ServiceSummary{{Kind: "ArtifactServer", Name: "lab-artifacts", Machine: "controller", Status: lifecycle.RealizationPending}},
			Secrets:         lifecycle.SecretSummary{Declared: 3},
			NextSteps:       []string{"bootwright plan", "bootwright apply"},
		}
	}
	// A context whose last removal completed and finalized: what it took
	// back and the cluster no block named read pending, it holds no binding,
	// and nothing is offered.
	destroyed := func() *lifecycle.StatusResult {
		removed := []lifecycle.BlockResult{
			{ID: "os-install-rhel-01", Description: "remove the installer media of rhel-01", Stage: "machines", State: "done", Attempts: 1},
			{ID: "machine-rhel-01", Description: "remove the virtual machine rhel-01 and its controller", Stage: "machines", State: "done", Attempts: 1},
			{ID: "substrate-host-lab-libvirt", Description: "remove the networks and virtual-media pool of lab-libvirt from controller", Stage: "substrates", State: "done", Attempts: 1},
			{ID: "artifact-server-lab-artifacts", Description: "remove the artifact server lab-artifacts from controller", Stage: "infra-components", State: "done", Attempts: 1},
		}
		return &lifecycle.StatusResult{
			Context:         lifecycle.ContextIdentity{Name: "lab", Revision: revision, Mode: "ready"},
			SetupChecks:     []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}, {ID: "execution-bundle", Status: "ready"}},
			Desired:         lifecycle.DesiredSummary{Revision: revision, Environment: "lab-rhel", Files: 14, Objects: 14},
			Clusters:        []lifecycle.ClusterSummary{{Name: "ocp-01", Kind: "ContainerCluster", Status: lifecycle.RealizationPending}},
			StorageClusters: []lifecycle.ClusterSummary{{Name: "ceph-01", Kind: "StorageCluster", Status: lifecycle.RealizationUnsupported}},
			Shared: []lifecycle.ServiceSummary{
				{Kind: "ArtifactServer", Name: "lab-artifacts", Machine: "controller", Status: lifecycle.RealizationPending},
				{Kind: "DNSServer", Name: "lab-dns", Machine: "controller", Status: lifecycle.RealizationUnsupported},
			},
			Secrets:   lifecycle.SecretSummary{Declared: 3},
			NextSteps: []string{},
			Lifecycle: &lifecycle.LifecycleSummary{
				Operation: operationID, Verb: "destroy", State: "done", Next: "none", Blocks: removed,
				Logs: []string{"contexts/lab/state/operations/" + operationID + "/logs/operation.jsonl"}, Executable: "1.4.0 (9f2c1ab)",
			},
			Contradictions: []string{},
			LogLocation:    logs,
		}
	}
	// A host whose controller setup never completed: the bundle reads
	// not-ready, the binding waits for the first apply, and setup is the only
	// step offered.
	unprepared := func() *lifecycle.StatusResult {
		result := idle()
		result.SetupChecks = []lifecycle.SetupCheck{{ID: "controller-binding", Status: "pending"}, {ID: "execution-bundle", Status: "not-ready"}}
		result.NextSteps = []string{"bootwright setup"}
		return result
	}
	validation := func() *compilation.Report {
		return &compilation.Report{
			Counts:                    compilation.Counts{FilesSeen: 15, ObjectsDecoded: 15},
			ExcludedContainerClusters: []string{"ocp-02"}, ExcludedStorageClusters: []string{},
			ExcludedResourceFiles: []string{"clusters/ocp-02/cluster.yaml"},
			Advisories:            []diagnostic{deferred}, Diagnostics: []diagnostic{deferred, excluded},
		}
	}
	invalid := &diagnostics.Failure{Diagnostics: []diagnostic{
		{
			Severity: "error", Code: "api.required", Message: "required field is absent",
			Source: &diagnostics.SourceLocation{Path: "environment.yaml", Document: 1, Line: 6, Column: 3},
			Object: &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Environment", Name: "lab-rhel"},
			Field:  "$.spec.domains.base",
		},
		{
			Severity: "error", Code: "api.field", Message: "field is not permitted by this schema",
			Source: &diagnostics.SourceLocation{Path: "infra/machines/rhel-01.yaml", Document: 1, Line: 12, Column: 5},
			Object: &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Machine", Name: "rhel-01"},
			Field:  "$.spec.network.adresses",
		},
	}}
	checked := func() *custody.CheckResult {
		return &custody.CheckResult{Context: secretContext, Secrets: []custody.CheckRow{
			{Name: "lab-bmc-credentials", Type: "usernamePassword", Source: "generated", Parts: []secrets.Part{secrets.UsernamePart, secrets.PasswordPart}, Status: "available", Version: version("ver-4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d"), Sequence: 1},
			{Name: "artifact-server-tls", Type: "tlsCertificate", Source: "generated", Parts: []secrets.Part{secrets.PrivateKeyPart, secrets.CertificatePart}, Status: "available", Version: version("ver-1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"), Sequence: 1},
			{Name: "bootwright-machine-key", Type: "sshKeyPair", Source: "generated", Parts: []secrets.Part{secrets.PublicKeyPart, secrets.PrivateKeyPart}, Status: "available", Version: version("ver-7e6d5c4b3a291807f6e5d4c3b2a19080"), Sequence: 2},
		}}
	}
	missing := func() *custody.CheckResult {
		return &custody.CheckResult{Context: secretContext, Secrets: []custody.CheckRow{
			{Name: "artifact-server-tls", Type: "tlsCertificate", Source: "generated", Parts: []secrets.Part{secrets.CertificatePart, secrets.PrivateKeyPart}, Status: "available", Version: version("ver-1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"), Sequence: 1},
			{Name: "registry-pull-secret", Type: "dockerConfigJson", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, Status: "missing"},
		}}
	}
	listed := func() *custody.ListResult {
		return &custody.ListResult{Context: secretContext, Secrets: []custody.ListRow{
			{Name: "registry-pull-secret", Type: "dockerConfigJson", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, State: "orphaned", BoundVersions: 1},
			{Name: "lab-bmc-credentials", Type: "usernamePassword", Source: "generated", Parts: []secrets.Part{secrets.UsernamePart, secrets.PasswordPart}, State: "stale", CurrentVersion: version("ver-4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d"), CurrentSequence: 2, BoundVersions: 2},
			{Name: "artifact-server-tls", Type: "tlsCertificate", Source: "generated", Parts: []secrets.Part{secrets.CertificatePart, secrets.PrivateKeyPart}, State: "current", CurrentVersion: version("ver-1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f"), CurrentSequence: 1, BoundVersions: 1},
		}}
	}
	encryptionStatus := func() *encryption.StatusResult {
		key := activeKey
		return &encryption.StatusResult{
			Context:     secretContext,
			Initialized: true,
			Implementation: &encryption.ImplementationStatus{
				Type: "local-keyring", State: "ready",
				Store:      encryption.ComponentStatus{ID: "local-v4", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
				KeyCustody: encryption.ComponentStatus{ID: "local-keyfile-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
			},
			ActiveKey: &key,
			Keys:      []secretstore.Key{{ID: activeKey, State: "active", Seals: 3}, {ID: retiredKey, State: "retired", Seals: 12}},
			Items:     encryption.ItemStatus{CurrentVersions: 3, BoundVersions: 3, MaterialParts: 6},
		}
	}
	stored := func() *media.ListResult {
		return &media.ListResult{Media: []media.MediaRow{
			{
				Name: "rhel-9.8-x86_64-boot.iso", Size: 1045430272, SHA256: "e8b0f3a61d9c2e47b5a803f6d1c94e27a0b6d3f81c5e9a24d7b0e3f6a19c5d82",
				Source: "file:///srv/images/rhel-9.8-x86_64-boot.iso", Added: "2026-09-20T08:15:00Z", Frozen: true, Verified: "ok",
			},
			{
				Name: "rhel-9.8-x86_64-dvd.iso", Size: 13123217408, SHA256: "4c1f8e2b9d7a6c5e3f0b1a2d4e6f8a0c2b4d6e8f0a1c3e5b7d9f1a3c5e7b9d0f",
				Source: "https://images.example.test/rhel-9.8-x86_64-dvd.iso", Added: "2026-09-21T10:02:41Z", Verified: "ok",
			},
		}}
	}
	// Rows arrive in name order, as inventory.Rows derives them.
	machines := func(powerRead bool) *inventory.ListResult {
		result := &inventory.ListResult{Context: "lab", PowerRead: powerRead, Machines: []inventory.MachineRow{
			{Name: "controller", Address: "controller.lab.example.test", IPs: []string{"192.0.2.1"}, OS: "provided", Clusters: []string{}, Lifecycle: "not-applied"},
			{Name: "rhel-01", Address: "rhel-01.lab.example.test", IPs: []string{"198.51.100.11"}, OS: "installed", Provider: "lab-libvirt", Clusters: []string{}, Lifecycle: "applied"},
			{Name: "sno-01", IPs: []string{}, OS: "provided", Clusters: []string{"sno"}, Lifecycle: "not-applied"},
		}}
		if powerRead {
			result.Machines[0].Power, result.Machines[1].Power = "unknown", "on"
		}
		return result
	}
	powered := func(verb, now, previous string, changed bool) *power.Result {
		return &power.Result{
			Context: "lab", Machine: "rhel-01", Verb: verb, Power: now, Previous: previous, Changed: changed,
			LogLocation: "/var/lib/bootwright/contexts/lab/state/runs/" + runID, Logs: []string{"contexts/lab/state/runs/" + runID + "/run.output"},
		}
	}
	// A run whose adapter refuses proves no power state: the service returns
	// only where its output was retained, beside the runner's own refusal
	// (internal/reconciliation/ansiblerunner/process_linux_amd64.go), which
	// points at the file the run's request names
	// (internal/reconciliation/lifecycle/runtime.go).
	refusedRun := func(r *dispatchRecord) {
		r.result.power = &power.Result{
			LogLocation: "/var/lib/bootwright/contexts/lab/state/runs/" + runID, Logs: []string{"contexts/lab/state/runs/" + runID + "/run.output"},
		}
		r.err = diagnostics.NewFailureWithRemediation("lifecycle.state", "the adapter operation did not complete", "", "read the adapter output retained in this run's run.output")
	}
	trusted := func() *enrollment.Report {
		return &enrollment.Report{Context: "lab", Pending: 2, Recorded: 2, Hosts: []enrollment.HostReport{
			{Machine: "bastion", Address: "192.0.2.5", Port: 22, Action: enrollment.ActionAdd, KeyType: "ssh-ed25519", Fingerprint: "SHA256:Xq3v9LmP2rT8wY5zB1nK4cH7dF0gJ6sA9eU2iO3pQ1M"},
			{Machine: "db-01", Address: "198.51.100.21", Port: 2222, Action: enrollment.ActionReplace, KeyType: "ssh-ed25519", Fingerprint: "SHA256:b7R2kN9xW4pL1vC8mT5qZ3hG6jD0fS2aY9uE4iK7oP1", PreviousFingerprint: "SHA256:M4tH8cQ1zV6nB3xR9wL2pK7dJ5fG0sA8eY1uT4iO6rN"},
			{Machine: "rhel-01", Action: enrollment.ActionSkip, Reason: "host key comes from its installation evidence"},
		}}
	}
	effective := func() *compilation.EffectiveResult {
		environment := api.NewObject(api.Environment, "lab-rhel", api.Value{}, api.MapValue(
			api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "base", Value: api.StringValue("lab.example.test")})},
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		))
		controller := api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
			api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(true)})},
		))
		return &compilation.EffectiveResult{Counts: compilation.Counts{FilesSeen: 2, ObjectsDecoded: 2}, Effective: api.NewCatalog([]api.Object{controller, environment})}
	}
	unauthorized := diagnostics.NewFailureWithRemediation("lifecycle.authorization",
		"this plan has data-loss consequences that are not authorized: os-install-rhel-01", "",
		"review the plan's impacts and repeat the command with --authorize data-loss")
	noInput := diagnostics.NewFailure("context.input", "context has no desired state; run context update --name lab --input-dir <dir>", "")
	return []cliGolden{
		// Controller reports: an unpresented dry-run plan, a presented setup
		// that retired bundles, and a context's readiness.
		{golden: "cli-setup-plan", args: "setup --dry-run", record: func(r *dispatchRecord) {
			r.result.controller = &prerequisites.Report{
				Platform: platform, Route: "direct", DryRun: true, Outcome: "planned",
				Checks: []prerequisites.Check{
					{ID: "execution-bundle", Required: "qualified Python and Ansible", Observed: "unverified", Status: "unverified"},
					{ID: "container-runtime", Required: "podman", Observed: "unverified", Status: "unverified"},
				},
				Actions:      []string{"Prepare the qualified execution bundle", "Install the baseline native packages"},
				Dependencies: []string{"qualified-source.tar.gz"},
			}
		}},
		{golden: "cli-setup-changed", args: "setup --yes --purge-old-bundles", record: func(r *dispatchRecord) {
			r.result.controller = &prerequisites.Report{
				Platform: platform, Route: "direct", Outcome: "changed", PlanPresented: true,
				Checks:         []prerequisites.Check{ready("execution-bundle", "qualified"), ready("container-runtime", "podman 5.6.1")},
				Actions:        []string{"Prepare the qualified execution bundle"},
				RetiredBundles: []string{"bundle-2026-08", "bundle-2026-09"},
			}
		}},
		// Each check carries the summary its constructor in
		// internal/controller/prerequisites/service.go gives a held check,
		// which readiness makes both required and observed: the platform, the
		// verified local identity, versions() of the execution bundle, the
		// installed podman version-release, toolVersionSummary of the target
		// tools, the libvirt client and the controller Machine's name.
		{golden: "cli-preflight-controller", args: "preflight controller --context lab", record: func(r *dispatchRecord) {
			held := func(id, summary, scope string) prerequisites.Check {
				return prerequisites.Check{ID: id, Required: summary, Observed: summary, Status: "ready", Scope: scope}
			}
			r.result.controller = &prerequisites.Report{
				ContextName: "lab", Machine: "controller", Platform: platform, Route: "direct", Outcome: "ready",
				Checks: []prerequisites.Check{
					held("host", "fedora 43/amd64", prerequisites.HostScope),
					held("installed-host", "verified local identity", prerequisites.HostScope),
					held("execution-bundle", "Python 3.14.7, Ansible 2.21.4", prerequisites.HostScope),
					held("container-runtime", "5.6.1-1.fc43", prerequisites.HostScope),
					held("target-tools", "openshift-clients 4.21.15, openshift-install 4.21.15", prerequisites.ContextScope),
					held("libvirt-client", "libvirt client", prerequisites.ContextScope),
					held("controller-binding", "controller", prerequisites.ContextScope),
				},
			}
		}},

		// Lifecycle: a staged preview, a preview whose apply only completes an
		// interrupted finalization, a completed apply, a settled destroy, a
		// settled apply that first completed an interrupted finalization, a
		// settled destroy that first released what an interrupted registration
		// left, an apply that ran and failed, and a refusal that registered
		// nothing.
		{golden: "cli-plan", args: "plan --stage infra-components,substrates", record: func(r *dispatchRecord) {
			r.result.lifecyclePlan = &lifecycle.PlanResult{
				Context: lifecycle.ContextIdentity{Name: "lab", Revision: revision}, Verb: "apply",
				Steps: []lifecycle.PlanStep{
					{ID: "artifact-server-lab-artifacts", Description: "serve artifacts for lab-artifacts on controller", Stage: "infra-components", Impacts: []string{"create-container-unit", "open-listener 192.0.2.1:8443"}, State: "pending", Selection: lifecycle.StepStart, Wave: 1},
					{ID: "substrate-host-lab-libvirt", Description: "realize the libvirt host of lab-libvirt on controller", Stage: "substrates", Impacts: []string{"create-libvirt-pool lab-libvirt", "create-libvirt-network lab"}, State: "pending", Selection: lifecycle.StepStart, After: []int{1}, Wave: 2},
					{ID: "machine-rhel-01", Description: "realize the virtual machine rhel-01 and its controller", Stage: "machines", Impacts: []string{"create-libvirt-domain rhel-01"}, State: "pending", Selection: lifecycle.StepNotSelected, After: []int{2}, Wave: 3},
					{ID: "os-install-rhel-01", Description: "install the operating system of rhel-01", Stage: "machines", Impacts: []string{"power-on rhel-01"}, State: "pending", Selection: lifecycle.StepWaiting, WaitsOn: "machine-rhel-01", After: []int{3}, Wave: 4},
				},
				Stages: []string{"infra-components", "substrates"}, Waves: 4, Widest: 1, Startable: 2, Deferred: 2,
				Receipt: lifecycle.Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"},
			}
		}},
		{golden: "cli-plan-finalization", args: "plan", record: func(r *dispatchRecord) {
			r.result.lifecyclePlan = &lifecycle.PlanResult{
				Context: lifecycle.ContextIdentity{Name: "lab", Revision: revision}, Verb: "apply",
				Steps: []lifecycle.PlanStep{
					{ID: "artifact-server-lab-artifacts", Description: "serve artifacts for lab-artifacts on controller", Stage: "infra-components", Impacts: []string{"create-container-unit"}, State: "done", Wave: 1},
					{ID: "substrate-host-lab-libvirt", Description: "realize the libvirt host of lab-libvirt on controller", Stage: "substrates", Impacts: []string{"create-libvirt-pool lab-libvirt"}, State: "done", After: []int{1}, Wave: 2},
				},
				Waves: 2, Widest: 1, Continuation: true, Finalizes: true,
				Receipt: lifecycle.Receipt{Operation: operationID, Verb: "plan", State: "preview", Next: "continue-apply"},
			}
		}},
		{golden: "cli-apply", args: "apply --yes", record: func(r *dispatchRecord) { r.result.lifecycleOperation = operation("done") }},
		{golden: "cli-destroy", args: "destroy --yes", record: func(r *dispatchRecord) {
			r.result.lifecycleOperation = &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab", Revision: revision}, Verb: "destroy", Settled: true,
				Receipt: lifecycle.Receipt{Operation: "none", Verb: "destroy", State: "done", Next: "none"},
			}
		}},
		{golden: "cli-apply-finalized", args: "apply --yes", record: func(r *dispatchRecord) {
			result := operation("done")
			result.Logs, result.LogLocation = nil, ""
			result.Settled, result.Recovered = true, lifecycle.RecoveredFinalization
			r.result.lifecycleOperation = result
		}},
		{golden: "cli-destroy-released", args: "destroy --yes", record: func(r *dispatchRecord) {
			r.result.lifecycleOperation = &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab", Revision: revision}, Verb: "destroy",
				Settled: true, Recovered: lifecycle.RecoveredRelease,
				Receipt: lifecycle.Receipt{Operation: "none", Verb: "destroy", State: "done", Next: "none"},
			}
		}},
		{
			golden: "cli-apply-failed", args: "apply --yes", code: 1,
			record: func(r *dispatchRecord) {
				r.result.lifecycleOperation = operation("failed")
				r.err = diagnostics.NewFailureWithRemediation("lifecycle.state", "the operation did not complete", "", "repeat the operation to continue it")
			},
			stderr: "[FAIL] lifecycle.state: the operation did not complete; next: repeat the operation to continue it\n",
		},
		{
			args: "apply --yes --authorize data-loss --stage machines", code: 1, record: func(r *dispatchRecord) { r.err = unauthorized },
			stderr: "[FAIL] lifecycle.authorization: this plan has data-loss consequences that are not authorized: os-install-rhel-01; next: review the plan's impacts and repeat the command with --authorize data-loss\n",
		},
		// Status: an apply that failed beside records that contradict it,
		// which offers no next step, and an idle context, whose empty row
		// sections are omitted from text and whose lifecycle is null.
		{golden: "cli-status", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = status() }},
		{golden: "cli-status-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = status() }},
		{golden: "cli-status-unresolved", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = unresolved() }},
		{golden: "cli-status-unresolved-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = unresolved() }},
		{golden: "cli-status-idle", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = idle() }},
		{golden: "cli-status-idle-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = idle() }},
		{golden: "cli-status-destroyed", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = destroyed() }},
		{golden: "cli-status-destroyed-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = destroyed() }},
		{golden: "cli-status-unprepared", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = unprepared() }},
		{golden: "cli-status-unprepared-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = unprepared() }},

		// Desired state: warnings reach standard error in text and the
		// envelope's diagnostics in JSON; a failed validate has no result.
		{
			golden: "cli-validate", args: "validate -f inputs", record: func(r *dispatchRecord) { r.report = validation() },
			stderr: "[WARN] api.selection clusters/ocp-02/cluster.yaml:4:9: cluster root is excluded by the Environment selection [ContainerCluster/ocp-02] ($.metadata.name); next: include the cluster in the matching Environment root selection\n" +
				"[WARN] api.deferred playbooks/reserved.yaml:5:1: CustomPlaybook is a reserved declaration and cannot execute. [CustomPlaybook/reserved] ($.spec)\n",
		},
		{golden: "cli-validate-json", args: "validate -f inputs --output json", record: func(r *dispatchRecord) { r.report = validation() }},
		{
			args: "validate -f inputs", code: 1, record: func(r *dispatchRecord) { r.report, r.err = validation(), invalid },
			stderr: "[FAIL] api.required environment.yaml:6:3: required field is absent [Environment/lab-rhel] ($.spec.domains.base)\n" +
				"[FAIL] api.field infra/machines/rhel-01.yaml:12:5: field is not permitted by this schema [Machine/rhel-01] ($.spec.network.adresses)\n",
		},
		{golden: "cli-validate-failed-json", args: "validate -f inputs --output json", code: 1, record: func(r *dispatchRecord) { r.report, r.err = validation(), invalid }},
		{golden: "cli-render-effective", args: "render effective", record: func(r *dispatchRecord) { r.result.effective = effective() }},
		{golden: "cli-render-effective-json", args: "render effective --output json", record: func(r *dispatchRecord) { r.result.effective = effective() }},

		// Contexts.
		{
			golden: "cli-context-init", args: "context init --name lab -f context.yaml --input-dir inputs",
			record: func(r *dispatchRecord) {
				r.result.admission = &contexts.AdmissionResult{Context: lab, Counts: compilation.Counts{FilesSeen: 14, ObjectsDecoded: 14}, FilesCopied: 14, InputChanged: true, Diagnostics: []diagnostic{deferred}}
			},
			stderr: "[WARN] api.deferred playbooks/reserved.yaml:5:1: CustomPlaybook is a reserved declaration and cannot execute. [CustomPlaybook/reserved] ($.spec)\n",
		},
		{golden: "cli-context-update", args: "context update --name lab --input-dir inputs --yes", record: func(r *dispatchRecord) {
			r.result.admission = &contexts.AdmissionResult{Context: lab, Counts: compilation.Counts{FilesSeen: 14, ObjectsDecoded: 14}}
		}},
		{golden: "cli-context-use", args: "context use --name lab", record: func(r *dispatchRecord) { r.result.use = &contexts.UseResult{Context: lab} }},
		{golden: "cli-context-list", args: "context list", record: func(r *dispatchRecord) {
			r.result.list = &contexts.ListResult{Contexts: []contexts.Summary{
				{Name: "retired", Mode: contexts.Deleting, Configured: true}, lab, {Name: "edge", Mode: contexts.Initializing},
			}}
		}},
		{golden: "cli-context-current", args: "context current", record: func(r *dispatchRecord) { r.result.current = &contexts.CurrentResult{Context: lab} }},
		{golden: "cli-context-current-short", args: "context current --short", record: func(r *dispatchRecord) { r.result.current = &contexts.CurrentResult{Context: lab} }},
		{
			golden: "cli-context-delete", args: "context delete --name retired --purge --allow-orphans --yes",
			record: func(r *dispatchRecord) {
				r.result.deletion = &contexts.DeleteResult{Name: "retired", Outcome: "deleted", OrphansAbandoned: true, ReleasedReservations: []string{"libvirt-domain:bootwright-lab-rhel-01", "socket:192.0.2.1:8000"}}
			},
			stderr: "[WARN] context.orphaned: the objects this context owned were abandoned and are no longer managed\n",
		},

		// Secrets. A negative check keeps its complete result on standard
		// output and exits 1; a refusal has no result at all.
		{golden: "cli-secret-set", args: "secret set --name registry-pull-secret --value-file pull-secret.json", record: func(r *dispatchRecord) {
			r.result.secretMutation = &custody.MutationResult{Context: secretContext, Name: "registry-pull-secret", Changed: 1, Parts: []secrets.Part{secrets.ValuePart}}
		}},
		{golden: "cli-secret-generate", args: "secret generate", record: func(r *dispatchRecord) {
			r.result.secretMutation = &custody.MutationResult{Context: secretContext, Changed: 2, Unchanged: 1, ChangedNames: []string{"artifact-server-tls", "bootwright-machine-key"}, UnchangedNames: []string{"lab-bmc-credentials"}}
		}},
		{golden: "cli-secret-generate-current", args: "secret generate", record: func(r *dispatchRecord) {
			r.result.secretMutation = &custody.MutationResult{Context: secretContext, Unchanged: 3, UnchangedNames: []string{"artifact-server-tls", "bootwright-machine-key", "lab-bmc-credentials"}}
		}},
		{golden: "cli-secret-delete", args: "secret delete --name registry-pull-secret --yes", record: func(r *dispatchRecord) {
			r.result.secretMutation = &custody.MutationResult{Context: secretContext, Name: "registry-pull-secret", Changed: 1}
		}},
		{golden: "cli-secret-delete-noop", args: "secret delete --name registry-pull-secret --yes", record: func(r *dispatchRecord) {
			r.result.secretMutation = &custody.MutationResult{Context: secretContext, Name: "registry-pull-secret", Unchanged: 1}
		}},
		{golden: "cli-secret-check", args: "secret check", record: func(r *dispatchRecord) { r.result.secretCheck = checked() }},
		{golden: "cli-secret-check-json", args: "secret check --output json", record: func(r *dispatchRecord) { r.result.secretCheck = checked() }},
		{
			golden: "cli-secret-check-negative", args: "secret check", code: 1,
			record: func(r *dispatchRecord) {
				r.result.secretCheck = missing()
				r.err = unstoredSecret
			},
			stderr: "[FAIL] secret.input: Secret registry-pull-secret has no stored material [Secret/registry-pull-secret]; next: bootwright secret set --context lab --name registry-pull-secret --value-file <path>\n",
		},
		{golden: "cli-secret-check-negative-json", args: "secret check --output json", code: 1, record: func(r *dispatchRecord) {
			r.result.secretCheck = missing()
			r.err = unstoredSecret
		}},
		{golden: "cli-secret-list", args: "secret list", record: func(r *dispatchRecord) { r.result.secretList = listed() }},
		{golden: "cli-secret-list-json", args: "secret list --output json", record: func(r *dispatchRecord) { r.result.secretList = listed() }},
		{
			args: "secret list --context lab", code: 1, record: func(r *dispatchRecord) { r.err = noInput },
			stderr: "[FAIL] context.input: context has no desired state; run context update --name lab --input-dir <dir>\n",
		},
		{golden: "cli-secret-list-refused-json", args: "secret list --context lab --output json", code: 1, record: func(r *dispatchRecord) { r.err = noInput }},
		// An explicit sensitive result is the requested bytes with no added
		// LF (specs/cli/output.md, streams), which a text golden cannot hold
		// as itself, so its golden holds the quoted form.
		{golden: "cli-secret-show", args: "secret show --name lab-bmc-credentials --part username", quoted: true, record: func(r *dispatchRecord) {
			r.result.secretReveal = &custody.RevealResult{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("admin"), secrets.PasswordPart: []byte("unused")}), Part: secrets.UsernamePart}
		}},
		// The administrator kubeconfig is an explicit sensitive result too.
		{golden: "cli-cluster-kubeconfig", args: "cluster kubeconfig --name sno", quoted: true, record: func(r *dispatchRecord) {
			r.result.kubeconfig = &containeraccess.KubeconfigResult{Context: "lab", Cluster: "sno", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(kubeconfigFixture)})}
		}},
		{
			args: "cluster kubeconfig --name ceph", code: 1, record: func(r *dispatchRecord) { r.err = kubeconfigNotApplicable },
			stderr: "[FAIL] cluster.not-applicable: bootwright cluster kubeconfig does not apply to ceph, a managed Ceph StorageCluster; it applies to OpenShift and OKD ContainerClusters only; next: bootwright render effective --context lab lists the clusters this context selects and their kinds\n",
		},
		{golden: "cli-secret-encryption-init", args: "secret encryption init", record: func(r *dispatchRecord) {
			r.result.encryptionMutation = &encryption.MutationResult{Context: secretContext, Implementation: selection, ActiveKey: retiredKey, Changed: true}
		}},
		{golden: "cli-secret-encryption-rotate", args: "secret encryption rotate --yes", record: func(r *dispatchRecord) {
			r.result.encryptionMutation = &encryption.MutationResult{Context: secretContext, Implementation: selection, ActiveKey: activeKey, Changed: true, RetiredKeys: []string{retiredKey}, ReencryptedVersions: 3, ReencryptedParts: 6}
		}},
		{golden: "cli-secret-encryption-status", args: "secret encryption status", record: func(r *dispatchRecord) { r.result.encryptionStatus = encryptionStatus() }},
		{golden: "cli-secret-encryption-status-cleanup", args: "secret encryption status", record: func(r *dispatchRecord) {
			r.result.encryptionStatus = encryptionStatus()
			r.result.encryptionStatus.Items.RetainedArtifacts, r.result.encryptionStatus.Items.CleanupRequired = 2, true
		}},
		{golden: "cli-secret-encryption-status-json", args: "secret encryption status --output json", record: func(r *dispatchRecord) { r.result.encryptionStatus = encryptionStatus() }},

		// Media.
		{golden: "cli-media-add", args: "media add --name rhel-9.8-x86_64-dvd.iso --from-file rhel-9.8-x86_64-dvd.iso", record: func(r *dispatchRecord) {
			r.result.mediaMutation = &media.MutationResult{Name: "rhel-9.8-x86_64-dvd.iso", Size: 13123217408, SHA256: "4c1f8e2b9d7a6c5e3f0b1a2d4e6f8a0c2b4d6e8f0a1c3e5b7d9f1a3c5e7b9d0f", Outcome: "stored"}
		}},
		{golden: "cli-media-delete", args: "media delete --name rhel-9.8-x86_64-dvd.iso --yes", record: func(r *dispatchRecord) {
			r.result.mediaMutation = &media.MutationResult{Name: "rhel-9.8-x86_64-dvd.iso", Outcome: "deleted"}
		}},
		{golden: "cli-media-list", args: "media list --checksums", record: func(r *dispatchRecord) { r.result.mediaList = stored() }},
		{golden: "cli-media-list-json", args: "media list --checksums --output json", record: func(r *dispatchRecord) { r.result.mediaList = stored() }},

		// Machines. A power verb's service also reports where its run's
		// output is kept, through a reporter outside this boundary, which a
		// JSON invocation silences: TestAJSONInvocationWritesNoProgress in
		// cmd/bootwright proves that wiring, and these goldens hold what the
		// Runner writes.
		{golden: "cli-machine-list", args: "machine list --power-status", record: func(r *dispatchRecord) { r.result.machines = machines(true) }},
		{golden: "cli-machine-list-json", args: "machine list --power-status --output json", record: func(r *dispatchRecord) { r.result.machines = machines(true) }},
		{golden: "cli-machine-list-silent", args: "machine list --silent", record: func(r *dispatchRecord) { r.result.machines = machines(false) }},
		{golden: "cli-machine-start", args: "machine start --name rhel-01", record: func(r *dispatchRecord) { r.result.power = powered("start", "on", "off", true) }},
		{golden: "cli-machine-start-json", args: "machine start --name rhel-01 --output json", record: func(r *dispatchRecord) { r.result.power = powered("start", "on", "off", true) }},
		{golden: "cli-machine-stop", args: "machine stop --name rhel-01 --yes", record: func(r *dispatchRecord) { r.result.power = powered("stop", "off", "off", false) }},
		{golden: "cli-machine-stop-json", args: "machine stop --name rhel-01 --yes --output json", record: func(r *dispatchRecord) { r.result.power = powered("stop", "off", "off", false) }},
		{golden: "cli-machine-restart", args: "machine restart --name rhel-01 --force --yes", record: func(r *dispatchRecord) { r.result.power = powered("restart", "on", "on", true) }},
		{golden: "cli-machine-restart-json", args: "machine restart --name rhel-01 --force --yes --output json", record: func(r *dispatchRecord) { r.result.power = powered("restart", "on", "on", true) }},
		// A refused run: text named the run's directory while it ran, so it
		// adds only the diagnostic; JSON reported no progress, so its failure
		// envelope names the retained file.
		{
			args: "machine stop --name rhel-01 --yes", code: 1, record: refusedRun,
			stderr: "[FAIL] lifecycle.state: the adapter operation did not complete; next: read the adapter output retained in this run's run.output\n",
		},
		{golden: "cli-machine-stop-refused-json", args: "machine stop --name rhel-01 --yes --output json", code: 1, record: refusedRun},
		{golden: "cli-machine-trust", args: "machine trust --replace db-01 --yes", record: func(r *dispatchRecord) { r.result.trust = trusted() }},
		{golden: "cli-machine-trust-json", args: "machine trust --replace db-01 --yes --output json", record: func(r *dispatchRecord) { r.result.trust = trusted() }},

		// Envelopes: a usage error, which never dispatches, and a command
		// whose use case this build does not provide.
		{
			args: "status --output yaml", code: 2,
			stderr: "[FAIL] cli.usage: --output has an unsupported value\nUsage: bootwright status [flags]\nRun 'bootwright help' for available commands.\n",
		},
		{golden: "cli-usage-json", args: "status --output json --verbose", code: 2},
		{
			args: "status", code: 1, record: func(r *dispatchRecord) { r.err = availability.ErrNotImplemented },
			stderr: "[FAIL] cli.not-implemented: bootwright status is not implemented\n",
		},
		{golden: "cli-not-implemented-json", args: "status --output json", code: 1, record: func(r *dispatchRecord) { r.err = availability.ErrNotImplemented }},
	}
}

const kubeconfigFixture = "apiVersion: v1\nclusters:\n- cluster:\n    server: https://api.sno.lab.example:6443\n  name: sno\nkind: Config\n"

// unstoredSecret is the diagnostic secret check gives a declared contextStore
// Secret with no stored material.
var unstoredSecret = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
	Severity: "error", Code: "secret.input", Message: "Secret registry-pull-secret has no stored material",
	Object:      &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Secret", Name: "registry-pull-secret"},
	Remediation: "bootwright secret set --context lab --name registry-pull-secret --value-file <path>",
}}}

var kubeconfigNotApplicable = diagnostics.NewFailureWithRemediation("cluster.not-applicable",
	"bootwright cluster kubeconfig does not apply to ceph, a managed Ceph StorageCluster; it applies to OpenShift and OKD ContainerClusters only", "",
	"bootwright render effective --context lab lists the clusters this context selects and their kinds")

// An explicit sensitive result is the custody's bytes exactly: nothing is
// added, not even a final LF the material lacks, and standard error stays
// empty. A refusal writes nothing to standard output and exactly one
// diagnostic to standard error.
func TestClusterKubeconfigWritesExactlyItsBytesOrOneDiagnostic(t *testing.T) {
	run := func(result *containeraccess.KubeconfigResult, failure error) (int, string, string) {
		record := &dispatchRecord{err: failure}
		record.result.kubeconfig = result
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"cluster", "kubeconfig", "--name", "sno"})
		return code, out.String(), errOut.String()
	}
	for _, value := range []string{kubeconfigFixture, "apiVersion: v1\x00\r\nkind: Config"} {
		code, out, errOut := run(&containeraccess.KubeconfigResult{Context: "lab", Cluster: "sno", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})}, nil)
		if code != 0 || out != value || errOut != "" {
			t.Fatalf("a revealed kubeconfig exited %d with %q and standard error %q, want exactly %q", code, out, errOut, value)
		}
	}
	for _, failure := range []error{
		kubeconfigNotApplicable,
		diagnostics.NewFailureWithRemediation("access.target", "the context lab selects no ContainerCluster named sno", "", "name a ContainerCluster this context selects"),
		diagnostics.NewFailureWithRemediation("access.unavailable", "this context holds no administrator kubeconfig for ContainerCluster sno", "", "bootwright apply --context lab"),
		secretstore.Failure("store.implementation", "this context's secret store is local-keyring-v3, which this Bootwright build cannot open"),
	} {
		code, out, errOut := run(nil, failure)
		if code != 1 || out != "" || strings.Count(errOut, "[FAIL] ") != 1 || strings.Count(errOut, "\n") != 1 {
			t.Fatalf("a refused kubeconfig exited %d with %q and standard error %q, want exit 1, no output and one diagnostic", code, out, errOut)
		}
	}
	code, out, errOut := run(&containeraccess.KubeconfigResult{Context: "lab", Cluster: "sno"}, nil)
	if code != 1 || out != "" || strings.Count(errOut, "[FAIL] ") != 1 {
		t.Fatalf("an empty kubeconfig exited %d with %q and standard error %q", code, out, errOut)
	}
}

// Every byte a command writes is contract (specs/cli/output.md), so each
// result arm, each output mode and each envelope is taken at the Runner, over
// fixed application results and the production effective-state encoders, and
// compared whole: standard output with its golden, and the exit status and
// standard error inline.
func TestCommandOutputMatchesItsGoldens(t *testing.T) {
	for _, test := range cliGoldens() {
		name := test.golden
		if name == "" {
			name = test.args
		}
		t.Run(name, func(t *testing.T) {
			record := &dispatchRecord{}
			if test.record != nil {
				test.record(record)
			}
			var out, errOut bytes.Buffer
			code := New(Config{
				Out: &out, ErrOut: &errOut, Services: dispatchSpies(record),
				EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON,
			}).Run(context.Background(), strings.Fields(test.args))
			if code != test.code {
				t.Errorf("exit status %d, want %d", code, test.code)
			}
			if errOut.String() != test.stderr {
				t.Errorf("standard error differs:\n got %q\nwant %q", errOut.String(), test.stderr)
			}
			switch {
			case test.golden == "":
				if out.Len() != 0 {
					t.Errorf("standard output %q, want none", out.String())
				}
			case test.quoted:
				matchesTextGolden(t, test.golden, []byte(strconv.Quote(out.String())+"\n"))
			case test.json():
				matchesGolden(t, test.golden, out.Bytes(), true)
			default:
				matchesTextGolden(t, test.golden, out.Bytes())
			}
		})
	}
}

// A command becomes available by a catalog edit alone, so the catalog decides
// what must be pinned: every available command needs a successful case with a
// golden, and one in JSON when it accepts --output. A golden no case writes is
// stale and fails rather than lingering.
func TestEveryAvailableCommandHasAGolden(t *testing.T) {
	// A session's standard output is the remote process's own bytes
	// (specs/cli/output.md, streams), so Bootwright writes nothing to pin.
	sessions := []string{"machine rsh", "machine exec"}
	covered, coveredJSON, written := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, test := range cliGoldens() {
		if test.golden == "" {
			continue
		}
		if written[test.golden] {
			t.Errorf("two cases write %s", test.golden)
		}
		written[test.golden] = true
		if test.code == 0 {
			covered[test.path()] = true
			coveredJSON[test.path()] = coveredJSON[test.path()] || test.json()
		}
	}
	implemented := map[string]bool{}
	for _, spec := range commandCatalog() {
		if !spec.implemented {
			continue
		}
		implemented[spec.path] = true
		if slices.Contains(sessions, spec.path) {
			continue
		}
		if !covered[spec.path] {
			t.Errorf("available command %q has no successful golden case", spec.path)
		}
		acceptsOutput := slices.ContainsFunc(spec.flags, func(flag flagSpec) bool { return flag.name == "output" })
		if acceptsOutput && !coveredJSON[spec.path] {
			t.Errorf("available command %q accepts --output but has no successful JSON golden case", spec.path)
		}
	}
	for _, path := range sessions {
		if !implemented[path] {
			t.Errorf("%q is exempt as a session but is no longer an available command", path)
		}
	}
	files, err := filepath.Glob(filepath.Join("testdata", "cli-*.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if name := strings.TrimSuffix(filepath.Base(file), ".golden"); !written[name] {
			t.Errorf("%s is written by no case; delete it with the case that wrote it", file)
		}
	}
}
