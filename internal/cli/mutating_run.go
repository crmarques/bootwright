package cli

import (
	"io"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

func printMutatingRunPreamble(stdout io.Writer, output, label string) {
	if output != outputText {
		return
	}
	p := cliout.New(stdout)
	p.Command(label)
	p.Section("Prepare")
	p.List([]cliout.Item{{Label: "Load desired state"}})
}

func printPlanStep(stdout io.Writer, output, label string) {
	if output == outputText {
		cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + label}})
	}
}

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
