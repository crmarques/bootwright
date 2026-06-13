package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/bundle"
)

func TestWorkflowReporterGroupsBundlePreparation(t *testing.T) {
	var out bytes.Buffer
	out.WriteString("Summary\n  [OK] host check: all 7 check(s) passed\n")

	reporter := newWorkflowReporter(&out)
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
	reporter := newWorkflowReporter(&out).WithPromptGap(&prompt)

	reporter.AnsibleStart("/usr/bin/ansible-playbook")

	if prompt.String() != "\n" {
		t.Fatalf("prompt gap = %q, want blank line", prompt.String())
	}
}
