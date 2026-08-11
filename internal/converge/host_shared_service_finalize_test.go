package converge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/workspace"
)

func workspaceContextFixture(name string) workspace.Context {
	return workspace.Context{Name: name, RenderedDir: "/rendered", RunsDir: "/runs", SecretsDir: "/secrets", ManagedServicesDir: "/managed", ProviderStateDir: "/provider", OwnershipDir: "/ownership"}
}

func TestHostSharedServiceFinalizeUsesExactManifestAndHostLimit(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	manifest, err := BuildHostSharedServiceManifest("matrix", "apply", []InfraComponentServiceRef{
		{Kind: "bmc-emulator", Name: "provider-bmc", Host: "provider.example.test", SelectionDigests: []string{digest}},
		{Kind: "artifacts", Name: "infra-artifacts", Host: "infra.example.test", SelectionDigests: []string{digest}},
	})
	if err != nil {
		t.Fatalf("BuildHostSharedServiceManifest: %v", err)
	}
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		t.Fatalf("ExtraVarPair: %v", err)
	}
	plan := WorkflowPlan{ExtraVarPairs: []string{`{"bootwright_mutating_invocation":"bootwright apply --context matrix"}`, pair}}
	opts, err := hostSharedServiceFinalizeOptions(workspaceContextFixture("matrix"), "/clusters", "ansible-playbook", "/bundle", "", plan, manifest, nil, nil, []string{"bootwright", "apply"})
	if err != nil {
		t.Fatalf("hostSharedServiceFinalizeOptions: %v", err)
	}
	if opts.Playbook != roles.PlaybookTaskHostSharedServiceFinalize {
		t.Fatalf("playbook = %q", opts.Playbook)
	}
	if opts.Limit != "infra.example.test:provider.example.test" {
		t.Fatalf("limit = %q", opts.Limit)
	}
	if len(opts.ExtraVarPairs) != 2 || opts.ExtraVarPairs[1] != pair {
		t.Fatalf("extra vars = %#v, want exact manifest", opts.ExtraVarPairs)
	}
	if strings.Join(opts.InvocationArgs, " ") != "bootwright apply" {
		t.Fatalf("invocation = %#v", opts.InvocationArgs)
	}
	if !opts.UseOwnershipRecordsSnapshot || !opts.ClassifyUnreachable || opts.PostRunFinalizer == nil || !opts.HostMutationLeaseFinalizer {
		t.Fatalf("finalizer options do not retain pre-run inventory and exact completion proof: %+v", opts)
	}
}

func TestHostSharedServiceFinalizeRefusesMissingOrDuplicateManifest(t *testing.T) {
	manifest, err := BuildHostSharedServiceManifest("matrix", "destroy", []InfraComponentServiceRef{{Kind: "proxy", Name: "infra-proxy", Host: "infra.example.test", SelectionDigests: []string{"sha256:" + strings.Repeat("a", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		t.Fatal(err)
	}
	for _, pairs := range [][]string{nil, {pair, pair}} {
		_, err := hostSharedServiceFinalizeOptions(workspaceContextFixture("matrix"), "/clusters", "ansible-playbook", "/bundle", "", WorkflowPlan{ExtraVarPairs: pairs}, manifest, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "exactly one unchanged selected-host manifest") {
			t.Fatalf("pairs %#v error = %v", pairs, err)
		}
	}
}

func TestHostSharedServiceFinalizeCapturesAnsibleOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
printf '%s\n' 'finalizer-stdout-line'
printf '%s\n' 'finalizer-stderr-line' >&2
mkdir -p "$BOOTWRIGHT_ANSIBLE_ARTIFACTS"
printf '%s\n' '{"host":"provider-host","status":"ok","completion":true}' '{"schemaVersion":1,"status":"terminal","processedHosts":["provider-host"],"hosts":{"provider-host":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":0,"completed":1}}}' > "$BOOTWRIGHT_ANSIBLE_ARTIFACTS/run-result.jsonl"
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	ctx := workspace.Context{
		Name:               "matrix",
		RenderedDir:        filepath.Join(root, "rendered"),
		RunsDir:            filepath.Join(root, "runs"),
		SecretsDir:         filepath.Join(root, "secrets"),
		ManagedServicesDir: filepath.Join(root, "managed-services"),
		ProviderStateDir:   filepath.Join(root, "provider-state"),
		OwnershipDir:       filepath.Join(root, "ownership"),
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	manifest, err := BuildHostSharedServiceManifest("matrix", "destroy", []InfraComponentServiceRef{{
		Kind:             string(ownership.KindBMCEmulator),
		Name:             "provider-a",
		Host:             "provider-host",
		SelectionDigests: []string{digest},
	}})
	if err != nil {
		t.Fatalf("BuildHostSharedServiceManifest: %v", err)
	}
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		t.Fatalf("ExtraVarPair: %v", err)
	}
	records := []ownership.ResourceRecord{{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       string(ownership.KindBMCEmulator),
		Name:       "provider-a",
		Owner:      ownership.Owner,
		Context:    "matrix",
		Host:       "provider-host",
		HostFacts:  map[string]string{"ansible_connection": "local"},
	}}
	lease, err := workflow.AcquireCommandRunLease(context.Background(), ctx.RunsDir, "destroy")
	if err != nil {
		t.Fatalf("AcquireCommandRunLease: %v", err)
	}
	defer func() { _ = lease.Close() }()
	var stdout, stderr bytes.Buffer
	if err := FinalizeHostSharedServiceOperations(context.Background(), &stdout, &stderr, ctx, filepath.Join(root, "clusters"), executable, filepath.Join(root, "bundle"), "", WorkflowPlan{ExtraVarPairs: []string{pair}}, manifest, records, nil, lease, []string{"bootwright", "destroy"}); err != nil {
		t.Fatalf("FinalizeHostSharedServiceOperations: %v", err)
	}
	terminal := stdout.String() + stderr.String()
	if strings.Contains(terminal, "finalizer-stdout-line") || strings.Contains(terminal, "finalizer-stderr-line") {
		t.Fatalf("host shared-service finalizer streamed Ansible output\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	logData, err := os.ReadFile(workflow.PreflightLogPath(ctx.RunsDir, hostSharedServiceFinalizeArtifactsBaseName))
	if err != nil {
		t.Fatalf("read finalizer Ansible log: %v", err)
	}
	for _, want := range []string{"finalizer-stdout-line", "finalizer-stderr-line"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("finalizer Ansible log missing %q:\n%s", want, logData)
		}
	}
}
