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
)

func newStateCheckCmd(stdout io.Writer) *cobra.Command {
	var (
		stage        string
		through      string
		clusterScope string
		output       string
	)
	cmd := &cobra.Command{
		Use:   "state-check",
		Short: "Report drift between selected desired state and the last recorded apply",
		Args:  cobra.NoArgs,
		Example: `  # Check the whole context for drift from the last apply
  bootwright state-check

  # Check only selected cluster roots
  bootwright state-check --clusters dc1-ocp,ceph-storage

  # Limit to the infrastructure stage
  bootwright state-check --stage infra

  # Check everything up to and including the clusters stage
  bootwright state-check --through clusters

  # Machine-readable drift report
  bootwright state-check --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("limit to a stage: %s (or sub-phase %s)", strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
	registerStageCompletion(cmd, converge.ApplyStageNames())
	cmd.Flags().StringVar(&through, "through", "", fmt.Sprintf("limit to all stages up to and including STAGE: %s (or sub-phase %s); cumulative, excludes --stage", strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
	registerFlagCompletion(cmd, "through", converge.ApplyStageNames())
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to check (default: all)")
	addOutputFlag(cmd, &output)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		if stage != "" && through != "" {
			return failErr(2, errors.New("--stage and --through are mutually exclusive: --stage limits to exactly that phase, --through limits to every phase from the beginning up to and including it"))
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
		report, err := status.StateCheck(state, clusterScope, scope.ApplyTarget(), cf.ctx.RunsDir, cf.ctx.OwnershipDir, cf.ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		if output == outputJSON {
			if err := cliout.JSON(stdout, report); err != nil {
				return failErr(1, err)
			}
		} else {
			printStateCheckReport(stdout, report)
		}
		if !report.InSync {
			// Exit non-zero so automation can gate on "no drift" without parsing the
			// report. 1 and 2 are already this command's load and usage codes, so
			// out-of-sync (drift, foreign, or never-applied) gets its own code (3);
			// the printed report stays the output.
			return silentExit(3)
		}
		return nil
	}
	return cmd
}

func printStateCheckReport(stdout io.Writer, report workflow.StateCheckReport) {
	p := cliout.New(stdout)
	p.Command("state-check")
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

// printStateCheckLoadWarnings surfaces ownership records that could not be read,
// decoded, or validated and were skipped on load, mirroring destroy's skipped-
// record warning. A skipped record's orphan never appears in the report above, so
// the operator is told which files to repair rather than letting an orphan vanish.
func printStateCheckLoadWarnings(p *cliout.Printer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	p.Section("Skipped ownership records")
	for _, warning := range warnings {
		p.Status(cliout.StatusWarn, "skipped", warning)
	}
}

// stateCheckRootSummary describes a present root's out-of-sync resources by
// classification, so a foreign-owned resource (resolve the foreign owner) is never
// lumped under "drifted" (re-apply) — the two have opposite remediations, and a
// never-applied resource is a third, distinct case.
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

// printStateCheckOrphans lists Bootwright-owned resources that are no longer declared
// in desired state (orphans). They are not drift and are never auto-removed; the
// remedy is to re-declare them or run a full `bootwright destroy` to reclaim them.
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
		p.Status(cliout.StatusWarn, o.Kind+"/"+o.Name, detail)
	}
	p.Status(cliout.StatusWarn, "remedy", "re-declare these objects, or run `bootwright destroy` to reclaim them")
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
