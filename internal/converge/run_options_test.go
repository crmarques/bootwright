package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestRunOptionsForContextSeedsSharedFields(t *testing.T) {
	ctx := workspace.Context{
		Name:               "demo",
		RenderedDir:        "/r",
		RunsDir:            "/runs",
		SecretsDir:         "/secrets",
		ManagedServicesDir: "/msd",
		ProviderStateDir:   "/psd",
		OwnershipDir:       "/own",
	}
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}}
	opts := runOptionsForContext(ctx, "/clusters", "ansible-playbook", state)

	if opts.RenderedDir != "/r" || opts.RunsDir != "/runs" || opts.SecretsDir != "/secrets" ||
		opts.ManagedServicesDir != "/msd" || opts.ProviderStateDir != "/psd" || opts.OwnershipDir != "/own" {
		t.Fatalf("context dirs not seeded: %+v", opts)
	}
	if opts.ContextName != "demo" || opts.ClustersDir != "/clusters" || opts.Executable != "ansible-playbook" {
		t.Fatalf("name/clustersDir/executable not seeded: %+v", opts)
	}
	if len(opts.State.Environments) != 1 {
		t.Fatalf("state not seeded")
	}
	if opts.Playbook != "" || opts.DryRun || opts.Check || opts.AcquireRunLease ||
		opts.ArtifactsBaseName != "" || opts.OutputLogPath != "" || opts.StreamAnsible ||
		opts.UseControllingTTY || opts.ResolveInstaller || opts.AskBecomePass ||
		opts.BundleDir != "" || opts.Limit != "" || opts.Forks != 0 || opts.Label != "" {
		t.Fatalf("seeder set run-specific fields it should leave to callers: %+v", opts)
	}
}
