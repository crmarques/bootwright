package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
	"github.com/crmarques/bootwright/internal/storage/cephadopt"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newDiffCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		stage        string
		through      string
		clusterScope string
		output       string
		recorded     bool
		adopt        bool
		verbose      bool
	)
	executable := workspace.ResolveAnsiblePlaybook()
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare desired state against the live clusters and report the differences",
		Long: "Compares the selected desired state against the real state of the clusters and\n" +
			"prints the differences as a git-style diff (\"-\" desired, \"+\" real). For each\n" +
			"managed Ceph StorageCluster it discovers live state read-only on the seed\n" +
			"(hosts, services and placements, OSD device layout, CRUSH rules, pools and\n" +
			"replication, config, mgr modules, health) and diffs it field by field; for each\n" +
			"ContainerCluster it runs a shallow reachability/ClusterVersion check. It is\n" +
			"read-only: it runs only read commands and writes no records (use --adopt on a\n" +
			"future run to fold real state back into desired state).\n\n" +
			"--recorded skips all cluster contact and instead reports drift between desired\n" +
			"state and the last recorded apply (match/drift/foreign/missing), the fast\n" +
			"offline check for automation. Exit codes: 0 in sync, 3 out of sync (a live\n" +
			"difference, degraded, or never-applied), 1 load error, 2 usage error.",
		Args: cobra.NoArgs,
		Example: `  # Diff the whole context against the live clusters
  bootwright diff

  # Diff only selected cluster roots
  bootwright diff --clusters dc1-ocp,ceph-storage

  # Fast offline check: desired state vs the last recorded apply
  bootwright diff --recorded

  # Machine-readable diff
  bootwright diff --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("limit to a stage: %s (or sub-phase %s)", strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
	registerStageCompletion(cmd, converge.ApplyStageNames())
	cmd.Flags().StringVar(&through, "through", "", fmt.Sprintf("limit to all stages up to and including STAGE: %s (or sub-phase %s); cumulative, excludes --stage", strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
	registerFlagCompletion(cmd, "through", converge.ApplyStageNames())
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to check (default: all)")
	cmd.Flags().BoolVar(&recorded, "recorded", false, "skip cluster contact; report drift against the last recorded apply instead of live state")
	cmd.Flags().BoolVar(&adopt, "adopt", false, "fold the discovered live state back into desired-state YAML (snapshots the prior input to history first); implies live mode")
	addVerboseFlag(cmd, &verbose)
	addOutputFlag(cmd, &output)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		if stage != "" && through != "" {
			return failErr(2, errors.New("--stage and --through are mutually exclusive: --stage limits to exactly that phase, --through limits to every phase from the beginning up to and including it"))
		}
		if recorded && adopt {
			return failErr(2, errors.New("--adopt requires live discovery and cannot be combined with --recorded"))
		}
		scope, err := converge.ApplyStageScope(stage)
		if through != "" {
			scope, err = converge.ApplyThroughScope(through)
		}
		if err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		report, err := status.StateCheck(state, clusterScope, scope.Name, scope.ApplyTarget(), cf.ctx.RunsDir, cf.ctx.OwnershipDir, cf.ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		if recorded {
			if output == outputJSON {
				if err := cliout.JSON(stdout, report); err != nil {
					return failErr(1, err)
				}
			} else {
				printStateCheckReport(stdout, report)
			}
			if !report.InSync {
				return silentExit(3)
			}
			return nil
		}
		live := buildLiveDiff(c.Context(), cf, executable, state, report, verbose, false, stderr)
		if adopt {
			var probed []cephadopt.ProbedStorage
			for _, storage := range live.Storage {
				if storage.Probed {
					probed = append(probed, cephadopt.ProbedStorage{Cluster: storage.Cluster, Report: storage.Report})
				}
			}
			summary, err := cephadopt.Adopt(cf.ctx, state, probed)
			if err != nil {
				return failErr(1, fmt.Errorf("adopt live state into desired state: %w", err))
			}
			live.Adopt = &summary
		}
		if output == outputJSON {
			if err := cliout.JSON(stdout, live); err != nil {
				return failErr(1, err)
			}
		} else {
			printLiveDiff(stdout, live)
		}
		if !live.InSync {
			return silentExit(3)
		}
		return nil
	}
	return cmd
}

func printStateCheckReport(stdout io.Writer, report workflow.StateCheckReport) {
	p := cliout.New(stdout)
	p.Command("diff")
	p.Section("Desired vs recorded reality")
	switch {
	case len(report.Roots) == 0:
		p.Status(cliout.StatusOK, "scope", "no selected resources to check")
	case report.InSync:
		p.Status(cliout.StatusOK, "state", "selected desired state matches the last recorded apply")
	default:
		for _, root := range report.Roots {
			label := root.Kind + "/" + root.Name
			switch {
			case root.Absent:
				p.Status(cliout.StatusWarn, label, "absent (never applied)")
			case len(root.Resources) == 0:
				p.Status(cliout.StatusOK, label, "in sync")
			default:
				p.Status(cliout.StatusWarn, label, stateCheckRootSummary(root))
				for _, resource := range root.Resources {
					p.Status(stateCheckResourceStatus(resource.Classification), resource.Label, string(resource.Classification))
				}
			}
		}
	}
	printStateCheckOrphans(p, report.Undeclared)
	printStateCheckLoadWarnings(p, report.LoadWarnings)
}

func printStateCheckLoadWarnings(p *cliout.Printer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	p.Section("Skipped ownership records")
	for _, warning := range warnings {
		p.Status(cliout.StatusWarn, "skipped", warning)
	}
}

func stateCheckRootSummary(root workflow.StateCheckRoot) string {
	var drift, foreign, missing, other int
	for _, resource := range root.Resources {
		switch resource.Classification {
		case workflow.ConvergeSafetyDrift:
			drift++
		case workflow.ConvergeSafetyForeign:
			foreign++
		case workflow.ConvergeSafetyMissing:
			missing++
		default:
			other++
		}
	}
	var parts []string
	if drift > 0 {
		parts = append(parts, fmt.Sprintf("%d drifted", drift))
	}
	if foreign > 0 {
		parts = append(parts, fmt.Sprintf("%d foreign-owned", foreign))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d not yet applied", missing))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown", other))
	}
	return fmt.Sprintf("%d of %d resources out of sync: %s", len(root.Resources), root.Total, strings.Join(parts, ", "))
}

func printStateCheckOrphans(p *cliout.Printer, orphans []workflow.UndeclaredResource) {
	if len(orphans) == 0 {
		return
	}
	p.Section("Owned but no longer declared")
	for _, o := range orphans {
		detail := "owned by Bootwright but not in desired state"
		switch {
		case o.Cluster != "":
			detail += fmt.Sprintf(" (cluster %s)", o.Cluster)
		case o.Provider != "":
			detail += fmt.Sprintf(" (provider %s)", o.Provider)
		}
		if destroySweepReclaims(o.Kind) {
			detail += "; re-declare it, or " + destroyReclaimsOrphanHint
		} else {
			detail += "; " + destroyLeavesOrphanHint
		}
		p.Status(cliout.StatusWarn, o.Kind+"/"+o.Name, detail)
	}
}

func stateCheckResourceStatus(class workflow.ConvergeSafetyClassification) cliout.Status {
	switch class {
	case workflow.ConvergeSafetyMatch:
		return cliout.StatusOK
	case workflow.ConvergeSafetyDrift, workflow.ConvergeSafetyMissing:
		return cliout.StatusWarn
	default:
		return cliout.StatusFail
	}
}
