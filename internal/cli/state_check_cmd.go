package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func newStateCheckCmd(stdout io.Writer) *cobra.Command {
	var (
		stage        string
		clusterScope string
		output       string
		override     bool
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

  # Machine-readable drift report
  bootwright state-check --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&stage, "stage", "", "limit to a stage: infra|clusters")
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to check")
	cmd.Flags().StringVar(&output, "output", outputText, "output format: text or json")
	cmd.Flags().BoolVar(&override, "override", false, "rejected: state-check never mutates state or suppresses drift")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		if override {
			return failErr(2, errors.New("--override is not valid for state-check; it never mutates state or suppresses drift"))
		}
		scope, err := converge.ApplyStageScope(stage)
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
			return cliout.JSON(stdout, report)
		}
		printStateCheckReport(stdout, report)
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
				p.Status(cliout.StatusWarn, label, fmt.Sprintf("%d of %d resources drifted from desired state", len(root.Resources), root.Total))
				for _, resource := range root.Resources {
					p.Status(stateCheckResourceStatus(resource.Classification), resource.Label, string(resource.Classification))
				}
			}
		}
	}
	printStateCheckOrphans(p, report.Undeclared)
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
