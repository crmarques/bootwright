package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
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
		scope, err := applyStageScope(stage)
		if err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		var storageNames []string
		if strings.TrimSpace(clusterScope) != "" {
			if _, storageNames, err = clusterRootNamesForTarget(state, clusterScope); err != nil {
				return failErr(1, err)
			}
		}
		state, err = scopeStateForApply(state, "all", clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		applyTarget := scope.applyTarget()
		applyTarget.StorageClusterNames = storageNames
		tasks, err := workflow.PlanApplyTasksChecked(applyTarget, state)
		if err != nil {
			return failErr(1, err)
		}
		report, err := workflow.StateCheck(tasks, cf.ctx.RunsDir)
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
	if len(report.Roots) == 0 {
		p.Status(cliout.StatusOK, "scope", "no selected resources to check")
		return
	}
	if report.InSync {
		p.Status(cliout.StatusOK, "state", "selected desired state matches the last recorded apply")
		return
	}
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
