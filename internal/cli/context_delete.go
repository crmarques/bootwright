package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newContextDeleteCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var name string
	var purge bool
	var yes bool
	var force bool
	cmd := &cobra.Command{
		Use:     "delete --name <ctx-name>",
		Short:   "Delete a context",
		Args:    cobra.NoArgs,
		Example: `  bootwright context delete --name lab --purge --yes`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	registerContextNameCompletion(cmd, "name")
	cmd.Flags().BoolVar(&purge, "purge", false, "delete the context base directory (required: a context is stored in shared root state, and delete refuses without it)")
	cmd.Flags().BoolVar(&force, "force", false, "delete the context even while it still owns running resources or installed clusters, abandoning them — their infrastructure keeps running and their ownership/install records and install-captured credentials (kubeconfigs, kubeadmin password) are lost. Prefer 'bootwright destroy --context <name>' first")
	addYesFlag(cmd, &yes, "delete")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if !purge {
			return failf(1, "context %q is stored in shared root state; rerun with --purge to delete it", name)
		}
		if purge && shouldRunContextRootChild() {
			childArgs := []string{"context", "delete", "--name", name, "--purge"}
			if yes {
				childArgs = append(childArgs, "--yes")
			}
			if force {
				childArgs = append(childArgs, "--force")
			}
			code, err := runWithLocalRoot(cmd.Context(), childArgs, stdin, stdout, stderr, true)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		registry, store, err := workspace.LoadDefaultStore()
		if err != nil {
			return failErr(1, err)
		}
		present, err := workspace.ContextBaseDirPresent(name)
		if err != nil {
			return failErr(1, err)
		}
		if present {
			ctx, err := workspace.RequireExistingContext(name)
			if err != nil {
				return failErr(1, err)
			}
			detail, refusal := contextPurgeGuard(ctx, force)
			if refusal != nil {
				return failErr(1, refusal)
			}
			if !yes && !confirm(stdin, stdout, contextDeletePrompt(name, ctx.BaseDir, detail)) {
				return failErr(1, errors.New("context delete aborted"))
			}
			if err := workspace.SafePurgeBaseDir(ctx); err != nil {
				return failErr(1, err)
			}
		}
		clearedRegistry := false
		if strings.TrimSpace(store.Current) == name {
			store.Current = ""
			if err := workspace.Save(registry, store); err != nil {
				return failErr(1, err)
			}
			clearedRegistry = true
		}
		p := output.New(stdout)
		p.Command("context delete")
		switch {
		case present:
			p.Summary(output.StatusOK, "context "+name, "base directory removed")
		case clearedRegistry:
			p.Summary(output.StatusOK, "context "+name, "not in shared storage; cleared local registry selection")
		default:
			p.Summary(output.StatusOK, "context "+name, "not in shared storage; nothing to remove")
		}
		return nil
	}
	return cmd
}

func contextDeletePrompt(name, baseDir, detail string) string {
	return fmt.Sprintf("Delete %s and all files under %s? %s [y/N] (default: no): ", name, baseDir, detail)
}

func contextPurgeGuard(ctx workspace.Context, force bool) (string, error) {
	records, skipped, err := converge.LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
	if err != nil {
		return "", err
	}
	installed, err := workflow.RecordedProvisionedClusters(workspace.ControllerClustersDir(ctx.Name))
	if err != nil {
		return "", err
	}
	detail := fmt.Sprintf("this removes %d ownership record(s), %d installed-cluster record(s), and the secrets directory %s — kubeconfigs, kubeadmin passwords, and any install-captured credentials become unrecoverable", len(records), len(installed), ctx.SecretsDir)
	if force {
		return detail, nil
	}
	if len(skipped) > 0 {
		return detail, fmt.Errorf("%d ownership record(s) under %s could not be read, so bootwright cannot tell whether context %q still owns running resources; fix or remove the corrupted record file(s), run `bootwright destroy --context %s` first, or pass --force to purge regardless (abandoning whatever they own)", len(skipped), ctx.OwnershipDir, ctx.Name, ctx.Name)
	}
	if len(records) > 0 || len(installed) > 0 {
		return detail, fmt.Errorf("context %q still owns %d resource(s) and %d installed cluster(s) — their infrastructure keeps running and their records and install-captured credentials (kubeconfigs, kubeadmin password) would be permanently lost; run `bootwright destroy --context %s` first, or pass --force to delete the context and abandon them", ctx.Name, len(records), len(installed), ctx.Name)
	}
	return detail, nil
}
