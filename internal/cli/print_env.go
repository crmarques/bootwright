package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newPrintEnvCmd(stdout io.Writer) *cobra.Command {
	var sensitive bool
	cmd := &cobra.Command{
		Use:   "print-env",
		Short: "Print shell exports for the current context",
		Args:  cobra.NoArgs,
		Example: `  eval "$(bootwright print-env)"
  eval "$(bootwright print-env --sensitive)"`,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "allow printing secret material such as proxy credentials")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		state, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths)
		if err != nil {
			return failErr(1, err)
		}
		if proxy.EnvRequiresSecrets(state) && !sensitive {
			return failErr(1, errors.New("proxy credentials would be printed; rerun with --sensitive to export them"))
		}
		proxyEnv, err := proxy.ResolveEnvForContext(ctx.Name, state, ctx.SecretsDir)
		if err != nil {
			return failErr(1, err)
		}
		writeShellExport(stdout, bootwrightContextEnv, ctx.Name)
		writeProxyShellExports(stdout, proxyEnv)
		return nil
	}
	return cmd
}
