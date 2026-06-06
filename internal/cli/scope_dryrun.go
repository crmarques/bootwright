package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type scopeDryRunReport struct {
	Target             string                    `json:"target"`
	Action             string                    `json:"action"`
	DryRun             bool                      `json:"dryRun"`
	PlanOnly           bool                      `json:"planOnly"`
	ReadinessChecked   bool                      `json:"readinessChecked"`
	ReadinessChecks    string                    `json:"readinessChecks"`
	Phases             []string                  `json:"phases"`
	RenderedDir        string                    `json:"renderedDir"`
	ClustersDir        string                    `json:"clustersDir"`
	RunsDir            string                    `json:"runsDir"`
	SecretsDir         string                    `json:"secretsDir"`
	ManagedServicesDir string                    `json:"managedServicesDir"`
	ProviderStateDir   string                    `json:"providerStateDir"`
	ContextDir         string                    `json:"contextDir"`
	BundleDir          string                    `json:"bundleDir"`
	Playbook           string                    `json:"playbook"`
	Limit              string                    `json:"limit,omitempty"`
	Check              bool                      `json:"check,omitempty"`
	ResolveInstaller   bool                      `json:"resolveInstaller"`
	Command            []string                  `json:"command"`
	Render             scopeDryRunRender         `json:"render"`
	ExtraVars          []string                  `json:"extraVars,omitempty"`
	ApplyPlan          *scopeDryRunApply         `json:"applyPlan,omitempty"`
	DestroySafety      *scopeDryRunDestroySafety `json:"destroySafety,omitempty"`
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
	Addons    []scopeDryRunAddonPlan     `json:"addons,omitempty"`
}

type scopeDryRunAddonPlan struct {
	Cluster   string                          `json:"cluster"`
	Binding   string                          `json:"binding"`
	Addon     string                          `json:"addon"`
	Type      string                          `json:"type"`
	Resources []extensionplan.ResourceSummary `json:"resources"`
}

type scopeDryRunDestroySafety struct {
	OverrideRequired bool     `json:"overrideRequired"`
	Override         bool     `json:"override"`
	Reasons          []string `json:"reasons"`
}

func runScopeDryRunJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope scopeSpec, action string, state v1alpha1.State, selected []Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, check bool, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []workflow.ApplyTask, destroySafety *scopeDryRunDestroySafety, forks int) error {
	ctx := cf.ctx
	clustersDir := controllerClustersDir(ctx.Name)
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return failErr(1, err)
	}
	runner := ansible.CommandRunner{Stdout: io.Discard, Stderr: io.Discard}
	runResult, err := workflow.Run(cmd.Context(), workflow.RunOptions{
		State:              state,
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		ContextName:        ctx.Name,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		OwnershipDir:       ctx.OwnershipDir,
		Executable:         flags.executable,
		BundleDir:          bundleDir,
		Playbook:           playbook,
		Limit:              limit,
		Forks:              forks,
		ExtraVarPairs:      extraVarPairs,
		ArtifactsBaseName:  artifactsBaseName,
		Check:              check,
		AskBecomePass:      askBecomePass,
		DryRun:             true,
		ResolveInstaller:   resolveInstaller,
		Label:              scope.name + " " + action,
	}, runner, nil)
	if err != nil {
		return failErr(1, err)
	}
	report := scopeDryRunReport{
		Target:             scope.name,
		Action:             action,
		DryRun:             true,
		PlanOnly:           true,
		ReadinessChecked:   false,
		ReadinessChecks:    "not run; run bootwright check " + scope.name,
		Phases:             selectedPhaseNames(selected),
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		ContextDir:         ctx.BaseDir,
		BundleDir:          bundleDir,
		Playbook:           playbook,
		Limit:              limit,
		Check:              check,
		ResolveInstaller:   resolveInstaller,
		Command:            runResult.Command,
		ExtraVars:          extraVarPairs,
		DestroySafety:      destroySafety,
		Render: scopeDryRunRender{
			EffectiveStatePath: runResult.Render.EffectiveStatePath,
			LockPath:           runResult.Render.LockPath,
			InventoryPath:      runResult.Render.InventoryPath,
			VarsPath:           runResult.Render.VarsPath,
			ArtifactsDir:       runResult.Render.ArtifactsDir,
			InstallerAssets:    dryRunInstallerArtifacts(runResult),
		},
	}
	if action == "apply" || action == "plan" {
		report.ApplyPlan = &scopeDryRunApply{
			RunStatus: string(workflow.RunStatusRunning),
			Limits:    limits,
			Tasks:     workflow.TaskLedgerEntries(tasks),
			Addons:    dryRunExtensionPlans(tasks),
		}
	}
	return output.JSON(stdout, report)
}

func dryRunExtensionPlans(tasks []workflow.ApplyTask) []scopeDryRunAddonPlan {
	var out []scopeDryRunAddonPlan
	for _, task := range tasks {
		if task.Entry.Kind != workflow.ApplyTaskKindClusterAddonApply || task.Extension == nil {
			continue
		}
		out = append(out, scopeDryRunAddonPlan{
			Cluster:   task.Extension.Cluster,
			Binding:   task.Extension.Binding,
			Addon:     task.Extension.Name,
			Type:      task.Extension.Extension.Spec.Type,
			Resources: extensionplan.ResourceSummaries(task.Extension.Extension),
		})
	}
	return out
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
