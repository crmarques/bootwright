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
	cliout.New(stdout).Command(label)
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
	section := "Run"
	if dryRun {
		section = "Plan"
	}
	reporter := newWorkflowReporter(stdout, section)
	if plan.AskBecomePass && become.PasswordFile == "" {
		reporter.WithPromptGap(stderr)
	}
	return become, reporter, cleanup, nil
}
