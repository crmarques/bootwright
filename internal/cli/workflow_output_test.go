package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/bundle"
)

func TestPlanHidesMutationOnlyFlags(t *testing.T) {
	plan := newPlanCmd(nil, io.Discard, io.Discard)
	for _, name := range []string{"reclaim-devices", "ask-become-pass", "verbose"} {
		flag := plan.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("plan is missing flag %q", name)
		}
		if !flag.Hidden {
			t.Fatalf("plan --help must hide mutation/execution-only flag %q", name)
		}
	}
	for _, name := range []string{"mode", "authorize", "clusters", "cluster-install-parallelism"} {
		flag := plan.Flags().Lookup(name)
		if flag == nil || flag.Hidden {
			t.Fatalf("plan should keep %q visible", name)
		}
	}
	apply := newApplyCmd(nil, io.Discard, io.Discard)
	if flag := apply.Flags().Lookup("reclaim-devices"); flag == nil || flag.Hidden {
		t.Fatal("apply must keep --reclaim-devices visible")
	}
	destroy := newDestroyCmd(nil, io.Discard, io.Discard)
	if flag := destroy.Flags().Lookup("cluster-install-parallelism"); flag != nil {
		t.Fatal("destroy must not accept --cluster-install-parallelism because its graph has no install chain")
	}
}

func TestWorkflowReporterGroupsBundlePreparation(t *testing.T) {
	var out bytes.Buffer
	out.WriteString("Summary\n  [OK] host check: all 7 check(s) passed\n")

	reporter := newWorkflowReporter(&out, "Run")
	reporter.BundleStart()
	reporter.BundleReady(bundle.AnsibleBundleResult{
		Dir:   "/var/lib/bootwright/cache/ansible-bundles/version=dev",
		Files: 1425,
	})
	reporter.RenderStart()
	reporter.AnsibleStart("/usr/bin/ansible-playbook")

	got := out.String()
	for _, want := range []string{
		"\n\nRun\n",
		"  - Prepare Ansible bundle: check cache and extract embedded roles/playbooks if needed\n",
		"  [OK] Ansible bundle: extracted 1425 file(s) to /var/lib/bootwright/cache/ansible-bundles/version=dev\n",
		"  - Render inputs: effective state, inventory, vars, and installer placeholders\n",
		"  - Ansible: starting /usr/bin/ansible-playbook\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("workflow output missing %q:\n%s", want, got)
		}
	}
}

func TestWorkflowReporterAddsPromptGap(t *testing.T) {
	var out bytes.Buffer
	var prompt bytes.Buffer
	reporter := newWorkflowReporter(&out, "Run").WithPromptGap(&prompt)

	reporter.AnsibleStart("/usr/bin/ansible-playbook")

	if prompt.String() != "\n" {
		t.Fatalf("prompt gap = %q, want blank line", prompt.String())
	}
}
