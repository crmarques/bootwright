package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

func registerFlagCompletion(cmd *cobra.Command, flag string, values []string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	})
}

func registerStageCompletion(cmd *cobra.Command, values []string) {
	registerFlagCompletion(cmd, "stage", values)
}

func printStageScopeNotices(stdout io.Writer, scope converge.Scope) {
	omitted, assumedPrior := converge.StageScopeOmissions(scope)
	if len(omitted) == 0 {
		return
	}
	c := cliout.NewContinuation(stdout)
	c.Status(cliout.StatusOK, "scope", "phases not in this plan: "+strings.Join(omitted, ", "))
	if len(assumedPrior) > 0 {
		c.Warning("scope", "assumes a prior apply completed: "+strings.Join(assumedPrior, ", "))
	}
}
