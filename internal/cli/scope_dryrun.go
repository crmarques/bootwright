package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workflow"
)

type scopeDryRunReport struct {
	Target           string            `json:"target"`
	Action           string            `json:"action"`
	DryRun           bool              `json:"dryRun"`
	PlanOnly         bool              `json:"planOnly"`
	ReadinessChecks  string            `json:"readinessChecks"`
	Phases           []string          `json:"phases"`
	StateDir         string            `json:"stateDir"`
	SecretsDir       string            `json:"secretsDir"`
	HostStateDir     string            `json:"hostStateDir"`
	BundleDir        string            `json:"bundleDir"`
	Playbook         string            `json:"playbook"`
	Limit            string            `json:"limit,omitempty"`
	Check            bool              `json:"check,omitempty"`
	ResolveInstaller bool              `json:"resolveInstaller"`
	Command          []string          `json:"command"`
	Render           scopeDryRunRender `json:"render"`
	ExtraVars        []string          `json:"extraVars,omitempty"`
	ApplyPlan        *scopeDryRunApply `json:"applyPlan,omitempty"`
}

type scopeDryRunRender struct {
	EffectiveStatePath string                         `json:"effectiveStatePath"`
	LockPath           string                         `json:"lockPath"`
	InventoryPath      string                         `json:"inventoryPath"`
	VarsPath           string                         `json:"varsPath"`
	ArtifactsDir       string                         `json:"artifactsDir"`
	InstallerAssets    []scopeDryRunInstallerArtifact `json:"installerAssets"`
}

type scopeDryRunInstallerArtifact struct {
	ClusterName       string `json:"clusterName"`
	Dir               string `json:"dir"`
	InstallConfigPath string `json:"installConfigPath"`
	AgentConfigPath   string `json:"agentConfigPath"`
}

type scopeDryRunApply struct {
	RunStatus string                     `json:"runStatus"`
	Limits    workflow.ConcurrencyLimits `json:"limits"`
	Tasks     []workflow.TaskLedgerEntry `json:"tasks"`
}

func runScopeDryRunJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope scopeSpec, action string, state v1alpha1.State, selected []Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, check bool, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []applyTask, forks int) error {
	ctx := cf.ctx
	bundleDir, err := resolveBundleDir(ctx.StateDir)
	if err != nil {
		return failErr(1, err)
	}
	runner := ansible.CommandRunner{Stdout: io.Discard, Stderr: io.Discard}
	runResult, err := workflow.Run(cmd.Context(), workflow.RunOptions{
		State:             state,
		StateDir:          ctx.StateDir,
		SecretsDir:        ctx.SecretsDir,
		HostStateDir:      flags.hostStateDir,
		Executable:        flags.executable,
		BundleDir:         bundleDir,
		Playbook:          playbook,
		Limit:             limit,
		Forks:             forks,
		ExtraVarPairs:     extraVarPairs,
		ArtifactsBaseName: artifactsBaseName,
		Check:             check,
		AskBecomePass:     askBecomePass,
		DryRun:            true,
		ResolveInstaller:  resolveInstaller,
		Label:             scope.name + " " + action,
	}, runner, nil)
	if err != nil {
		return failErr(1, err)
	}
	report := scopeDryRunReport{
		Target:           scope.name,
		Action:           action,
		DryRun:           true,
		PlanOnly:         true,
		ReadinessChecks:  "not run; run bootwright check " + scope.name,
		Phases:           selectedPhaseNames(selected),
		StateDir:         ctx.StateDir,
		SecretsDir:       ctx.SecretsDir,
		HostStateDir:     flags.hostStateDir,
		BundleDir:        bundleDir,
		Playbook:         playbook,
		Limit:            limit,
		Check:            check,
		ResolveInstaller: resolveInstaller,
		Command:          runResult.Command,
		ExtraVars:        extraVarPairs,
		Render: scopeDryRunRender{
			EffectiveStatePath: runResult.Render.EffectiveStatePath,
			LockPath:           runResult.Render.LockPath,
			InventoryPath:      runResult.Render.InventoryPath,
			VarsPath:           runResult.Render.VarsPath,
			ArtifactsDir:       runResult.Render.ArtifactsDir,
			InstallerAssets:    dryRunInstallerArtifacts(runResult),
		},
	}
	if action == "apply" {
		report.ApplyPlan = &scopeDryRunApply{
			RunStatus: string(workflow.RunStatusRunning),
			Limits:    limits,
			Tasks:     taskLedgerEntries(tasks),
		}
	}
	return output.JSON(stdout, report)
}

func selectedPhaseNames(selected []Phase) []string {
	names := make([]string, 0, len(selected))
	for _, phase := range selected {
		names = append(names, phase.Name)
	}
	return names
}

func dryRunInstallerArtifacts(runResult workflow.RunResult) []scopeDryRunInstallerArtifact {
	out := make([]scopeDryRunInstallerArtifact, 0, len(runResult.Render.InstallerAssets))
	for _, asset := range runResult.Render.InstallerAssets {
		out = append(out, scopeDryRunInstallerArtifact{
			ClusterName:       asset.ClusterName,
			Dir:               asset.Dir,
			InstallConfigPath: asset.InstallConfigPath,
			AgentConfigPath:   asset.AgentConfigPath,
		})
	}
	return out
}
