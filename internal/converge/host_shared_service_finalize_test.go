package converge

import (
	"strings"
	"testing"

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
