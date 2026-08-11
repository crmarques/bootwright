package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestDiffOrphanRemedyScopedToSweepCoverage(t *testing.T) {
	var buf bytes.Buffer
	printStateCheckOrphans(cliout.New(&buf), []workflow.UndeclaredResource{
		{Kind: "libvirt-domain", Name: "vm-a"},
		{Kind: "kubevirt-machine", Name: "vm-b", Cluster: "hub1"},
	})
	out := buf.String()
	if !strings.Contains(out, "libvirt-domain/vm-a") || !strings.Contains(out, "a full-context destroy reclaims it") {
		t.Fatalf("sweep-reclaimable orphan should promise reclaim:\n%s", out)
	}
	if !strings.Contains(out, "kubevirt-machine/vm-b") || !strings.Contains(out, "does not reclaim this record") {
		t.Fatalf("non-sweep orphan must not be pointed at a full destroy that leaves it standing:\n%s", out)
	}
	if strings.Contains(out, "`bootwright destroy`") {
		t.Fatalf("the blanket destroy remedy overpromises for non-sweep kinds and must be gone:\n%s", out)
	}
}

func TestDiffRejectsUnknownStage(t *testing.T) {
	_, stderr, code := runCLI(t, "diff", "--stage", "bogus")
	if code != 2 {
		t.Fatalf("diff --stage bogus exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stderr, "--stage must be one of") {
		t.Fatalf("diff --stage bogus stderr = %q", stderr)
	}
}

func TestDiffAcceptsSubPhaseStages(t *testing.T) {
	setTestHomeAndRoot(t)
	for _, stage := range converge.SubPhaseStageNames() {
		_, stderr, _ := runCLI(t, "diff", "--stage", stage)
		if strings.Contains(stderr, "--stage must be one of") {
			t.Fatalf("diff --stage %s rejected a valid sub-phase: %q", stage, stderr)
		}
	}
}

func TestDiffRejectsOverride(t *testing.T) {
	stdout, stderr, code := runCLI(t, "diff", "--override")
	if code != 2 {
		t.Fatalf("diff --override exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "unknown flag: --override") {
		t.Fatalf("expected unknown-flag rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestDiffAdoptRejectsRecorded(t *testing.T) {
	_, stderr, code := runCLI(t, "diff", "--recorded", "--adopt")
	if code != 2 {
		t.Fatalf("diff --recorded --adopt exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stderr, "--adopt requires live discovery") {
		t.Fatalf("stderr missing the adopt/recorded conflict: %q", stderr)
	}
}

func TestDiffAdoptRefusesWhileAnotherMutatorHoldsTheContext(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	identityFile := filepath.Join(t.TempDir(), "operator identity")
	if err := os.WriteFile(identityFile, []byte("test identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := workflow.AcquireCommandRunLease(context.Background(), ctx.RunsDir, "destroy")
	if err != nil {
		t.Fatalf("AcquireCommandRunLease: %v", err)
	}
	defer guard.Close()
	stdout, stderr, code := runCLI(t,
		"diff", "--adopt", "--stage", "deps", "--through", "base", "--clusters", "dc1-ocp",
		"--output", "json", "--verbose", "--context", ctx.Name, "--ssh-id-file", identityFile,
		"--ssh-user", "operator", "--ssh-user-for-provisioned")
	out := stdout + stderr
	if code == 0 || !strings.Contains(out, "mutating run") || !strings.Contains(out, guard.RunID) {
		t.Fatalf("diff --adopt exit=%d output=%q, want active-mutator refusal", code, out)
	}
	wantRetry := retryCommand{args: []string{
		"bootwright", "diff", "--stage", "deps", "--through", "base", "--clusters", "dc1-ocp",
		"--adopt", "--output", "json", "--verbose", "--context", ctx.Name, "--ssh-id-file", identityFile,
		"--ssh-user", "operator", "--ssh-user-for-provisioned",
	}}.String()
	if !strings.Contains(out, "`"+wantRetry+"`") {
		t.Fatalf("active-mutator refusal did not preserve the exact diff retry %q:\n%s", wantRetry, out)
	}
	if entries, err := os.ReadDir(workspace.InputHistoryDir(ctx)); err == nil && len(entries) > 0 {
		t.Fatalf("diff --adopt snapshotted input despite the active mutation lease: %v", entries)
	}
}

func TestDiffLocalFlagSurfaceIsExplicit(t *testing.T) {
	cmd := newDiffCmd(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.InitDefaultHelpFlag()
	var got []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		got = append(got, flag.Name)
	})
	slices.Sort(got)
	want := []string{"adopt", "clusters", "help", "output", "recorded", "stage", "through", "verbose"}
	if !slices.Equal(got, want) {
		t.Fatalf("diff local flags = %v, want exact public surface %v", got, want)
	}
}

func TestDiffRetryBuilderRepresentsEveryAcceptedFlag(t *testing.T) {
	base := resolvedInvocation{
		verb:                  invocationDiff,
		contextName:           "matrix context",
		sshIdentityFile:       "/tmp/operator identity",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			selection: runSelection{stage: "deps", through: "base", clusters: "ceph-a,ceph-b"},
			output:    outputJSON,
			adopt:     true,
			verbose:   true,
		},
	}
	adopt := mustRetry(t, base, retryIntent{})
	want := []string{
		"bootwright", "diff", "--stage", "deps", "--through", "base", "--clusters", "ceph-a,ceph-b",
		"--adopt", "--output", "json", "--verbose", "--context", "matrix context",
		"--ssh-id-file", "/tmp/operator identity", "--ssh-user", "operator", "--ssh-ask-sudo-password",
		"--ssh-user-for-provisioned",
	}
	if got := adopt.Args(); !slices.Equal(got, want) {
		t.Fatalf("diff adopt retry args = %#v\nwant %#v", got, want)
	}
	assertRetryParses(t, adopt, func(cmd *cobra.Command) {
		if cmd.Flag("adopt").Value.String() != "true" {
			t.Fatal("parsed retry did not preserve --adopt")
		}
	})

	recordedInvocation := base
	recordedInvocation.flags.adopt = false
	recordedInvocation.flags.recorded = true
	recorded := mustRetry(t, recordedInvocation, retryIntent{})
	if !slices.Contains(recorded.Args(), "--recorded") || slices.Contains(recorded.Args(), "--adopt") {
		t.Fatalf("recorded diff retry = %v", recorded.Args())
	}
	preserved := map[string]bool{}
	for _, retry := range []retryCommand{adopt, recorded} {
		for _, name := range retryFlagNames(retry.Args()) {
			preserved[name] = true
		}
	}
	root := newRootCmd(&bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	diff, _, err := root.Find([]string{"diff"})
	if err != nil {
		t.Fatal(err)
	}
	visit := func(flag *pflag.Flag) {
		if flag.Name != "help" && !preserved[flag.Name] {
			t.Errorf("diff --%s has no representation in the exact retry builder", flag.Name)
		}
	}
	diff.Flags().VisitAll(visit)
	diff.InheritedFlags().VisitAll(visit)
}

func TestDiffRejectsUnknownOutput(t *testing.T) {
	stdout, stderr, code := runCLI(t, "diff", "--output", "yaml")
	if code != 2 {
		t.Fatalf("diff --output yaml exit = %d, want 2", code)
	}
	if !strings.Contains(stdout+stderr, "--output must be") {
		t.Fatalf("expected output-format rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}
