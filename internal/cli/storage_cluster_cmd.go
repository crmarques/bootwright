package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newStorageClusterCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage-cluster <command>",
		Short: "Day-2 operations on a managed Ceph storage cluster",
	}
	cmd.AddCommand(newStorageClusterReplaceArbiterCmd(stdin, stdout, stderr))
	requireSubcommand(cmd)
	return cmd
}

type replaceArbiterFlags struct {
	clusterName  string
	machineName  string
	authorize    []string
	yes          bool
	dryRun       bool
	output       string
	verbose      bool
	askBecomePas bool
	executable   string
}

func newStorageClusterReplaceArbiterCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	flags := replaceArbiterFlags{}
	cmd := &cobra.Command{
		Use:   "replace-arbiter --name <cluster> --new-arbiter-machine <machine>",
		Short: "Move a Ceph stretch tiebreaker onto another machine",
		Long: `Move the stretch tiebreaker (arbiter) of a managed Ceph cluster from the machine
that holds it today onto another declared machine, running only the work that
change needs: prepare and install the replacement machine, install Ceph on it,
add its mon to the cluster, hand it the tiebreaker, then retire the mon and host
it replaced.

The replacement is authored, not discovered. Name a Machine that declares the
'ceph-arbiter' capability and belongs to no other cluster; --new-arbiter-machine
rewrites the context input so the StorageCluster's stretch tiebreaker names it
(the previous input is snapshotted to input-history first), and the run then
reconciles the live cluster onto that desired state. Author the change yourself
and drop the flag if you would rather edit the input by hand.

Every step is idempotent and the order never removes before it adds: the
replacement mon must be in the monmap, in quorum, and carrying its stretch CRUSH
location before the tiebreaker moves, so any failure before that point leaves the
original arbiter in place with the cluster's quorum intact, and re-running
resumes where it stopped. A run whose desired arbiter already answers as the
tiebreaker is a no-op.

The machine that was replaced keeps running with its OS intact; only its Ceph
membership is removed. Tear the machine down separately when you no longer want
it.`,
		Args: cobra.NoArgs,
		Example: `  # Move the arbiter of a stretch cluster onto a standby machine
  bootwright storage-cluster replace-arbiter --name ceph-prd-01 \
    --new-arbiter-machine ceph-arbiter-b --yes

  # Preview the whole plan, changing nothing
  bootwright storage-cluster replace-arbiter --name ceph-prd-01 \
    --new-arbiter-machine ceph-arbiter-b --dry-run

  # The arbiter host is gone for good (data centre lost, hardware scrapped)
  bootwright storage-cluster replace-arbiter --name ceph-prd-01 \
    --new-arbiter-machine ceph-arbiter-b --authorize unreachable-nodes --yes

  # Reconcile an arbiter already authored in the input
  bootwright storage-cluster replace-arbiter --name ceph-prd-01 --yes`,
	}
	cmd.Flags().StringVar(&flags.clusterName, "name", "", "StorageCluster name (required)")
	_ = cmd.MarkFlagRequired("name")
	registerClusterScopeCompletion(cmd, clusterKindStorage)
	cmd.Flags().StringVar(&flags.machineName, "new-arbiter-machine", "", "Machine to make the stretch tiebreaker; it must declare the "+v1alpha1.MachineCapabilityCephArbiter+" capability and be bound by no other cluster. Rewrites the context input to name it, then reconciles. Omit it to reconcile the arbiter the input already declares")
	registerArbiterMachineCompletion(cmd)
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, flagDryRunUsage)
	addOutputFlagDryRun(cmd, &flags.output)
	addYesFlag(cmd, &flags.yes, "replace-arbiter")
	addVerboseFlag(cmd, &flags.verbose)
	addAskBecomePassFlag(cmd, &flags.askBecomePas)
	addAuthorizeFlag(cmd, &flags.authorize, authorizeVerbReplaceArbiter)
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		flags.executable = workspace.ResolveAnsiblePlaybook()
		return runReplaceArbiter(c, stdin, stdout, stderr, cf, flags)
	}
	return cmd
}

func registerArbiterMachineCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("new-arbiter-machine", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		state, err := loadDesiredStateLocalOnly(addCommonFlags())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return arbiter.CandidateMachines(state), cobra.ShellCompDirectiveNoFileComp
	})
}

func resolveArbiterCluster(state v1alpha1.State, name string) (v1alpha1.StorageCluster, error) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name != name {
			continue
		}
		if !v1alpha1.StorageClusterManaged(cluster) {
			return v1alpha1.StorageCluster{}, fmt.Errorf("StorageCluster/%s is external (spec.management), so Bootwright does not manage its mons and cannot move its arbiter", name)
		}
		if cluster.Spec.Ceph == nil {
			return v1alpha1.StorageCluster{}, fmt.Errorf("StorageCluster/%s declares no spec.ceph, so it has no stretch arbiter", name)
		}
		return cluster, nil
	}
	return v1alpha1.StorageCluster{}, fmt.Errorf("--name %q matches no StorageCluster in this context; `bootwright cluster list` prints the declared cluster roots", name)
}

func printArbiterPlan(stdout io.Writer, plan arbiter.Plan, promotion arbiter.Promotion, dryRun bool) {
	p := cliout.New(stdout)
	fields := []cliout.Field{
		{Key: "Cluster", Value: plan.Cluster},
		{Key: "Failure domain", Value: plan.FailureDomain},
		{Key: "Replaces", Value: arbiterRetiredSubject(plan)},
		{Key: "With", Value: "mon." + plan.DesiredMon + " on Machine/" + plan.DesiredMachine + " (" + plan.FailureDomain + "=" + plan.DesiredSite + ")"},
	}
	if !promotion.Empty() {
		fields = append(fields, cliout.Field{Key: "Input rewrite", Value: promotion.RelPath + ": tiebreaker " + promotion.FromNode + " -> " + promotion.ToNode})
	}
	if plan.Degraded != "" {
		fields = append(fields, cliout.Field{Key: "Quorum", Value: plan.Degraded})
	}
	if plan.SameSite() {
		fields = append(fields, cliout.Field{Key: "Site conflict", Value: "shares " + plan.DesiredSite + " with mon(s) " + strings.Join(plan.SameSiteMons, ", ")})
	}
	p.Fields(fields)
	p.Section("Order")
	p.Fields([]cliout.Field{
		{Key: "1", Value: "prepare and install Machine/" + plan.DesiredMachine + ", then Ceph on it (apply --through deps)"},
		{Key: "2", Value: "deploy mon." + plan.DesiredMon + " with its stretch location and wait for quorum"},
		{Key: "3", Value: "ceph mon set_new_tiebreaker " + plan.DesiredMon},
		{Key: "4", Value: arbiterRetirementStep(plan)},
	})
	if dryRun {
		cliout.NewContinuation(stdout).Warning("dry-run", "plan only; nothing was changed and the context input was not rewritten")
	}
}

func arbiterMonLocations(cluster v1alpha1.StorageCluster, plan arbiter.Plan, hosts []string) map[string][]string {
	domain := plan.FailureDomain
	out := map[string][]string{}
	for _, host := range hosts {
		if node, ok := topology.HostByName(cluster, host); ok && node.Site != "" {
			out[host] = []string{domain + "=" + node.Site}
			continue
		}
		if host == plan.LiveNode && plan.LiveSite != "" {
			out[host] = []string{domain + "=" + plan.LiveSite}
		}
	}
	return out
}

func arbiterRetiredSubject(plan arbiter.Plan) string {
	if plan.LiveMon == "" {
		return "host " + plan.LiveNode + ", which still runs a mon no topology node declares"
	}
	return "mon." + plan.LiveMon + " (" + plan.FailureDomain + "=" + emptyAccessValue(plan.LiveSite) + ")"
}

func arbiterRetirementStep(plan arbiter.Plan) string {
	if plan.LiveMon == "" {
		return "remove host " + plan.LiveNode + ", which no topology node declares"
	}
	return "retire mon." + plan.LiveMon + " and remove host " + plan.LiveNode
}

func replaceArbiterConfirmPrompt(plan arbiter.Plan) string {
	if plan.LiveMon == "" {
		return "Retire host " + plan.LiveNode + " of " + plan.Cluster + ", which an interrupted replacement left running a mon no topology node declares?"
	}
	return "Move the stretch tiebreaker of " + plan.Cluster + " from mon." + plan.LiveMon + " to mon." + plan.DesiredMon + "?"
}

func errArbiterAborted() error { return errors.New("replace-arbiter aborted") }
