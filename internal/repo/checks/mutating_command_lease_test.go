package repocheck

import (
	"strings"
	"testing"
)

func TestDesiredStateMutatorsLeaseBeforeReadingOrWriting(t *testing.T) {
	cases := []struct {
		file          string
		acquire       string
		after         []string
		forbiddenLoad string
	}{
		{"internal/cli/scope_apply_cmd.go", `AcquireCommandRunLease(c.Context(), ctx.RunsDir, "apply")`, []string{"loadDesiredState(cf)", "RemovePlaybookRecordsForClusters"}, "cf.resolve()"},
		{"internal/cli/scope_destroy_cmd.go", `AcquireCommandRunLease(c.Context(), ctx.RunsDir, "destroy")`, []string{"loadDesiredStateTolerant(cf)", "SnapshotMutatingRunInput"}, "resolveTolerantInput"},
		{"internal/cli/context_update.go", `AcquireCommandRunLease(cmd.Context(), ctx.RunsDir, "context-update")`, []string{"workspace.ReplaceInput(ctx"}, ""},
		{"internal/cli/diff_cmd.go", `AcquireCommandRunLease(c.Context(), ctx.RunsDir, "diff-adopt")`, []string{"loadDesiredState(cf)", "cephadopt.Adopt(cf.ctx"}, ""},
		{"internal/cli/storage_cluster_replace_arbiter.go", `AcquireCommandRunLease(c.Context(), ctx.RunsDir, "replace-arbiter")`, []string{"loadDesiredState(cf)", "arbiter.Apply(ctx", "prepareArbiterMachine(runContext", "RunArbiterReplacement(runContext"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			source := readRepoFile(t, tc.file)
			acquire := strings.Index(source, tc.acquire)
			if acquire < 0 {
				t.Fatalf("%s does not acquire its registered command-wide mutation lease", tc.file)
			}
			for _, marker := range tc.after {
				position := strings.Index(source, marker)
				if position < 0 {
					t.Fatalf("%s no longer contains guarded operation %q; update the lease guard with the implementation", tc.file, marker)
				}
				if position < acquire {
					t.Fatalf("%s performs %q before acquiring its command-wide mutation lease", tc.file, marker)
				}
			}
			if tc.forbiddenLoad != "" && strings.Contains(source, tc.forbiddenLoad) {
				t.Fatalf("%s performs hidden desired-state locality loading through %q before its command lease; resolve only context identity/readiness before the lease and classify locality from the exact state loaded after it", tc.file, tc.forbiddenLoad)
			}
		})
	}
}

func TestDesiredStateLoadersClassifyLocalityFromTheReturnedState(t *testing.T) {
	source := readRepoFile(t, "internal/cli/state_input.go")
	for _, loader := range []string{"LoadNormalizeValidateInputFiles(ctx.InputPaths)", "LoadNormalizeValidateTolerant(ctx.InputPaths)"} {
		load := strings.Index(source, loader)
		if load < 0 {
			t.Fatalf("desired-state loader no longer contains %q; update the exact-state locality guard", loader)
		}
		if !strings.Contains(source[load:], "enforceControllerLocality(") {
			t.Fatalf("desired state loaded by %q is returned without locality classification over that exact state", loader)
		}
	}
}

func TestArbiterSubrunsConsumeTheCommandLease(t *testing.T) {
	cliSource := readRepoFile(t, "internal/cli/storage_cluster_replace_arbiter.go")
	for _, want := range []string{"runOpts.RunLease = runLease", "RunLease:           runLease"} {
		if !strings.Contains(cliSource, want) {
			t.Fatalf("replace-arbiter subrun does not consume its outer lease: missing %q", want)
		}
	}
	runSource := readRepoFile(t, "internal/converge/storage_replace_arbiter.go")
	for _, want := range []string{"runOpts.AcquireRunLease = opts.RunLease == nil", "runOpts.RunLease = opts.RunLease"} {
		if !strings.Contains(runSource, want) {
			t.Fatalf("arbiter Ansible run can open a lease gap or deadlock on the held lease: missing %q", want)
		}
	}
}
