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

func commaSeparatedCompletion(toComplete string, all []string) ([]string, cobra.ShellCompDirective) {
	prefix := ""
	chosen := map[string]struct{}{}
	if i := strings.LastIndex(toComplete, ","); i >= 0 {
		prefix = toComplete[:i+1]
		for _, part := range strings.Split(toComplete[:i], ",") {
			if part = strings.TrimSpace(part); part != "" {
				chosen[part] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(all))
	for _, name := range all {
		if _, taken := chosen[name]; taken {
			continue
		}
		out = append(out, prefix+name)
	}
	return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
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
