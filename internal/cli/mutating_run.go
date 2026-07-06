package cli

import (
	"io"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

// This file holds the presentation and setup seam shared by the two mutating-run
// commands, apply and destroy. Both sequence the same shape: print a Prepare
// banner, load and plan, run the gates, then collect a become credential and open
// a workflow reporter before handing the plan to a converge executor. Keeping the
// shared steps here removes the copy-shaped duplication that had accumulated in
// the two largest command files and gives the mutating-run flow one seam to
// evolve. The gate sequencing and executor dispatch still live in each command
// (they differ), and the domain decisions each step makes live in converge.

// printMutatingRunPreamble opens the shared "Prepare → Load desired state" banner
// a text-output apply or destroy prints before loading desired state. Non-text
// output (JSON) prints nothing here.
func printMutatingRunPreamble(stdout io.Writer, output, label string) {
	if output != outputText {
		return
	}
	p := cliout.New(stdout)
	p.Command(label)
	p.Section("Prepare")
	p.List([]cliout.Item{{Label: "Load desired state"}})
}

// printPlanStep prints the shared "Plan <label>" step a text-output apply or
// destroy prints once desired state has loaded.
func printPlanStep(stdout io.Writer, output, label string) {
	if output == outputText {
		cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + label}})
	}
}

// prepareMutatingRunCredential sets up the become credential and workflow reporter
// a mutating apply or destroy needs just before it hands the plan to a converge
// executor. It prints the pre-prompt blank line when a become password will be
// asked for, collects the credential (skipped for a dry-run or a no-remote-work
// plan), builds the reporter, and wires its prompt gap. The returned cleanup must
// be deferred by the caller so a staged credential file is removed when the
// command returns; it is a no-op when no credential was collected.
func prepareMutatingRunCredential(stdin io.Reader, stdout, stderr io.Writer, plan converge.WorkflowPlan, dryRun bool) (becomeCredential, *workflowReporter, func(), error) {
	become := becomeCredential{}
	cleanup := func() {}
	if !dryRun && !plan.NoRemoteWork && willPromptForBecomePassword(plan.AskBecomePass) {
		cliout.NewContinuation(stderr).BlankLine()
	}
	if !dryRun && !plan.NoRemoteWork {
		credential, c, err := prepareBecomeCredential(stdin, stderr, plan.AskBecomePass, false, true)
		if err != nil {
			return becomeCredential{}, nil, func() {}, err
		}
		cleanup = c
		become = credential
	}
	reporter := newWorkflowReporter(stdout)
	if plan.AskBecomePass && become.PasswordFile == "" {
		reporter.WithPromptGap(stderr)
	}
	return become, reporter, cleanup, nil
}
