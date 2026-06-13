package converge

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

type DryRunReport struct {
	Target             string               `json:"target"`
	Action             string               `json:"action"`
	DryRun             bool                 `json:"dryRun"`
	PlanOnly           bool                 `json:"planOnly"`
	ReadinessChecked   bool                 `json:"readinessChecked"`
	ReadinessChecks    string               `json:"readinessChecks"`
	Phases             []string             `json:"phases"`
	RenderedDir        string               `json:"renderedDir"`
	ClustersDir        string               `json:"clustersDir"`
	RunsDir            string               `json:"runsDir"`
	SecretsDir         string               `json:"secretsDir"`
	ManagedServicesDir string               `json:"managedServicesDir"`
	ProviderStateDir   string               `json:"providerStateDir"`
	ContextDir         string               `json:"contextDir"`
	BundleDir          string               `json:"bundleDir"`
	Playbook           string               `json:"playbook"`
	Limit              string               `json:"limit,omitempty"`
	Check              bool                 `json:"check,omitempty"`
	ResolveInstaller   bool                 `json:"resolveInstaller"`
	Command            []string             `json:"command"`
	Render             DryRunRender         `json:"render"`
	ExtraVars          []string             `json:"extraVars,omitempty"`
	ApplyPlan          *DryRunApply         `json:"applyPlan,omitempty"`
	DestroyPlan        *DryRunDestroyPlan   `json:"destroyPlan,omitempty"`
	DestroySafety      *DryRunDestroySafety `json:"destroySafety,omitempty"`
}

// DryRunDestroyPlan reports the ordered task chain a full (whole-context)
// destroy would run. Full destroy has no single playbook, so its dry-run report
// carries the chain here instead of a single Playbook/Command.
type DryRunDestroyPlan struct {
	Tasks []workflow.TaskLedgerEntry `json:"tasks"`
}

type DryRunRender struct {
	EffectiveStatePath string                    `json:"effectiveStatePath"`
	LockPath           string                    `json:"lockPath"`
	InventoryPath      string                    `json:"inventoryPath"`
	VarsPath           string                    `json:"varsPath"`
	ArtifactsDir       string                    `json:"artifactsDir"`
	InstallerAssets    []DryRunInstallerArtifact `json:"installerAssets"`
}

type DryRunInstallerArtifact struct {
	ClusterName       string `json:"clusterName"`
	Dir               string `json:"dir"`
	InstallConfigPath string `json:"installConfigPath"`
	AgentConfigPath   string `json:"agentConfigPath"`
}

type DryRunApply struct {
	RunStatus string                     `json:"runStatus"`
	Limits    workflow.ConcurrencyLimits `json:"limits"`
	Tasks     []workflow.TaskLedgerEntry `json:"tasks"`
	Addons    []DryRunAddonPlan          `json:"addons,omitempty"`
}

type DryRunAddonPlan struct {
	Cluster   string                          `json:"cluster"`
	Binding   string                          `json:"binding"`
	Addon     string                          `json:"addon"`
	Type      string                          `json:"type"`
	Resources []extensionplan.ResourceSummary `json:"resources"`
}

type DryRunDestroySafety struct {
	OverrideRequired bool     `json:"overrideRequired"`
	Override         bool     `json:"override"`
	Reasons          []string `json:"reasons"`
}

// BuildScopeDryRunReport runs the workflow in dry-run mode and assembles the
// machine-readable plan report. The CLI serializes it; the bundle directory
// arrives resolved from the CLI because the bundle version marker is CLI-owned.
func BuildScopeDryRunReport(cmdCtx context.Context, ctx workspace.Context, executable string, bundleDir string, scope Scope, action string, state v1alpha1.State, selected []Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, check bool, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []workflow.ApplyTask, destroySafety *DryRunDestroySafety, forks int) (DryRunReport, error) {
	clustersDir := workspace.ControllerClustersDir(ctx.Name)
	runner := ansible.CommandRunner{Stdout: io.Discard, Stderr: io.Discard}
	runResult, err := workflow.Run(cmdCtx, workflow.RunOptions{
		State:              state,
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		ContextName:        ctx.Name,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		OwnershipDir:       ctx.OwnershipDir,
		Executable:         executable,
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
		Label:              scope.Name + " " + action,
	}, runner, nil)
	if err != nil {
		return DryRunReport{}, err
	}
	report := DryRunReport{
		Target:             scope.Name,
		Action:             action,
		DryRun:             true,
		PlanOnly:           true,
		ReadinessChecked:   false,
		ReadinessChecks:    "not run; run bootwright preflight " + scope.Name,
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
		Render: DryRunRender{
			EffectiveStatePath: runResult.Render.EffectiveStatePath,
			LockPath:           runResult.Render.LockPath,
			InventoryPath:      runResult.Render.InventoryPath,
			VarsPath:           runResult.Render.VarsPath,
			ArtifactsDir:       runResult.Render.ArtifactsDir,
			InstallerAssets:    dryRunInstallerArtifacts(runResult),
		},
	}
	if action == "apply" || action == "plan" {
		report.ApplyPlan = &DryRunApply{
			RunStatus: string(workflow.RunStatusRunning),
			Limits:    limits,
			Tasks:     workflow.TaskLedgerEntries(tasks),
			Addons:    dryRunExtensionPlans(tasks),
		}
	}
	return report, nil
}

// BuildFullDestroyDryRunReport assembles the machine-readable plan for a
// whole-context destroy. Unlike the scoped destroy report it has no single
// playbook/command: it renders once for the artifact paths and reports the
// ordered destroy task chain (clusters then infra) the teardown would run.
func BuildFullDestroyDryRunReport(ctx workspace.Context, bundleDir string, scope Scope, state v1alpha1.State, selected []Phase, tasks []workflow.ApplyTask, extraVarPairs []string, destroySafety *DryRunDestroySafety) (DryRunReport, error) {
	clustersDir := workspace.ControllerClustersDir(ctx.Name)
	result, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, state)
	if err != nil {
		return DryRunReport{}, err
	}
	return DryRunReport{
		Target:             scope.Name,
		Action:             "destroy",
		DryRun:             true,
		PlanOnly:           true,
		ReadinessChecked:   false,
		ReadinessChecks:    "not run; run bootwright preflight",
		Phases:             selectedPhaseNames(selected),
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		ContextDir:         ctx.BaseDir,
		BundleDir:          bundleDir,
		ExtraVars:          extraVarPairs,
		DestroySafety:      destroySafety,
		Render: DryRunRender{
			EffectiveStatePath: result.EffectiveStatePath,
			LockPath:           result.LockPath,
			InventoryPath:      result.InventoryPath,
			VarsPath:           result.VarsPath,
			ArtifactsDir:       result.ArtifactsDir,
			InstallerAssets:    []DryRunInstallerArtifact{},
		},
		DestroyPlan: &DryRunDestroyPlan{Tasks: workflow.TaskLedgerEntries(tasks)},
	}, nil
}

func dryRunExtensionPlans(tasks []workflow.ApplyTask) []DryRunAddonPlan {
	var out []DryRunAddonPlan
	for _, task := range tasks {
		if task.Entry.Kind != workflow.ApplyTaskKindClusterAddonApply || task.Extension == nil {
			continue
		}
		out = append(out, DryRunAddonPlan{
			Cluster:   task.Extension.Cluster,
			Binding:   task.Extension.Binding,
			Addon:     task.Extension.Name,
			Type:      task.Extension.Extension.Spec.Type,
			Resources: extensionplan.ResourceSummaries(task.Extension.Extension),
		})
	}
	return out
}

func dryRunInstallerArtifacts(runResult workflow.RunResult) []DryRunInstallerArtifact {
	out := make([]DryRunInstallerArtifact, 0, len(runResult.Render.InstallerAssets))
	for _, asset := range runResult.Render.InstallerAssets {
		out = append(out, DryRunInstallerArtifact{
			ClusterName:       asset.ClusterName,
			Dir:               asset.Dir,
			InstallConfigPath: asset.InstallConfigPath,
			AgentConfigPath:   asset.AgentConfigPath,
		})
	}
	return out
}
