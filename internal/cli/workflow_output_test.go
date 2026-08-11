package cli

import (
	"bytes"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/spf13/pflag"
)

func TestPlanLocalFlagSurfaceIsExplicit(t *testing.T) {
	plan := newPlanCmd(nil, io.Discard, io.Discard)
	plan.InitDefaultHelpFlag()
	var got []string
	plan.Flags().VisitAll(func(flag *pflag.Flag) {
		got = append(got, flag.Name)
		if flag.Hidden {
			t.Errorf("plan flag %q is accepted but hidden", flag.Name)
		}
	})
	sort.Strings(got)
	want := []string{"authorize", "cluster-install-parallelism", "clusters", "help", "machines", "mode", "output", "reclaim-devices", "stage", "through"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan local flags = %v, want exact public surface %v", got, want)
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

func TestPlanRejectsApplyExecutionFlags(t *testing.T) {
	stdout, stderr, code := runCLI(t, "plan", "--dry-run=false")
	if code != 2 || !strings.Contains(stdout+stderr, "unknown flag: --dry-run") {
		t.Fatalf("plan --dry-run=false = exit %d, stdout=%q, stderr=%q; want unknown-flag exit 2", code, stdout, stderr)
	}
}

func TestApplyPlanHelpStatesReconcileContract(t *testing.T) {
	for _, verb := range []string{"apply", "plan"} {
		t.Run(verb, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, verb, "--help")
			if code != 0 {
				t.Fatalf("%s --help exited %d: %s", verb, code, stderr)
			}
			help := stdout + stderr
			for _, want := range []string{"converges drift that is reconcilable in place", "structural (destructive-identity) drift or foreign ownership"} {
				if !strings.Contains(help, want) {
					t.Fatalf("%s --help missing %q:\n%s", verb, want, help)
				}
			}
		})
	}
}

func TestPlanHelpDisclosesPersistentSSHSudoPrompt(t *testing.T) {
	stdout, stderr, code := runCLI(t, "plan", "--help")
	if code != 0 {
		t.Fatalf("plan --help exited %d: %s", code, stderr)
	}
	help := stdout + stderr
	for _, want := range []string{"the persistent --ssh-ask-sudo-password flag still prompts", "before the command"} {
		if !strings.Contains(help, want) {
			t.Fatalf("plan --help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "and never prompts") {
		t.Fatalf("plan --help still promises that no prompt can occur:\n%s", help)
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
