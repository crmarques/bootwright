package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type workflowReporter struct {
	printer       *output.Printer
	promptGap     *output.Printer
	section       string
	opened        bool
	showPromptGap bool
}

func newWorkflowReporter(stdout io.Writer, section string) *workflowReporter {
	return &workflowReporter{printer: output.NewContinuation(stdout), section: section}
}

func (r *workflowReporter) WithPromptGap(w io.Writer) *workflowReporter {
	r.promptGap = output.NewContinuation(w)
	r.showPromptGap = true
	return r
}

func (r *workflowReporter) BundleStart() {
	r.ensure()
	r.printer.List([]output.Item{{Label: "Prepare Ansible bundle", Detail: "check cache and extract embedded roles/playbooks if needed"}})
}

func (r *workflowReporter) BundleReady(result bundle.AnsibleBundleResult) {
	r.ensure()
	detail := "cache current at " + result.Dir
	if !result.Reused {
		detail = fmt.Sprintf("extracted %d file(s) to %s", result.Files, result.Dir)
	}
	r.printer.Status(output.StatusOK, "Ansible bundle", detail)
}

func (r *workflowReporter) RenderStart() {
	r.ensure()
	r.printer.List([]output.Item{{Label: "Render inputs", Detail: "effective state, inventory, vars, and installer placeholders"}})
}

func (r *workflowReporter) ResolveInstallerStart() {
	r.ensure()
	r.printer.List([]output.Item{{Label: "Resolve installer secrets", Detail: "write root-managed runtime installer files"}})
}

func (r *workflowReporter) DryRunCommand(label string, command []string) {
	r.ensure()
	r.printer.CommandLine("dry-run ansible command ["+label+"]", command)
}

func (r *workflowReporter) DryRunTasks(label string, tasks []workflow.TaskLedgerEntry, limits workflow.ConcurrencyLimits, groups []output.StepGroup) {
	r.ensure()
	steps := 0
	for _, group := range groups {
		steps += len(group.Steps)
	}
	r.printer.Status(output.StatusOK, label, fmt.Sprintf("%d step(s) covering %d planned task(s), parallelism %d", steps, len(tasks), limits.Parallelism))
	r.printer.Steps(groups)
}

func (r *workflowReporter) SkipNoHosts(label string, limit string) {
	r.ensure()
	r.printer.Status(output.StatusSkip, label, "no remote hosts match --limit "+limit)
}

func (r *workflowReporter) AnsibleStart(executable string) {
	r.ensure()
	r.printer.List([]output.Item{{Label: "Ansible", Detail: "starting " + executable}})
	if r.showPromptGap {
		r.promptGap.BlankLine()
	}
}

func (r *workflowReporter) ensure() {
	if r.opened {
		return
	}
	r.printer.Section(r.section)
	r.opened = true
}
