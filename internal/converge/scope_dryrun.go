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
	Playbook           string               `json:"playbook,omitempty"`
	Limit              string               `json:"limit,omitempty"`
	Check              bool                 `json:"check,omitempty"`
	ResolveInstaller   bool                 `json:"resolveInstaller"`
	Command            []string             `json:"command"`
	Render             DryRunRender         `json:"render"`
	ExtraVars          []string             `json:"extraVars,omitempty"`
	ApplyPlan          *DryRunApply         `json:"applyPlan,omitempty"`
	DestroyPlan        *DryRunDestroyPlan   `json:"destroyPlan,omitempty"`
	DestroySafety      *DryRunDestroySafety `json:"destroySafety,omitempty"`
	Refusals           []string             `json:"refusals,omitempty"`
	PurgeHistory       *DryRunPurgeHistory  `json:"purgeHistory,omitempty"`
}

type DryRunPurgeHistory struct {
	Scope string `json:"scope"`
}

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
	RunStatus   string                     `json:"runStatus"`
	Limits      workflow.ConcurrencyLimits `json:"limits"`
	Tasks       []workflow.TaskLedgerEntry `json:"tasks"`
	Addons      []DryRunAddonPlan          `json:"addons,omitempty"`
	Transitions *DryRunTransitions         `json:"transitions,omitempty"`
}

type DryRunTransitions struct {
	Objects []DryRunTransition `json:"objects"`
	Error   string             `json:"error,omitempty"`
}

type DryRunTransition struct {
	Label  string `json:"label"`
	Action string `json:"action"`
}

func BuildDryRunTransitions(tasks []workflow.ApplyTask, runsDir, contextName string, mode workflow.ApplyMode, reinstallClusters []string) *DryRunTransitions {
	objects, err := workflow.ClassifyApplyObjectsForMode(tasks, runsDir, contextName, mode)
	if err != nil {
		return &DryRunTransitions{Error: err.Error()}
	}
	reinstall := map[string]bool{}
	for _, name := range reinstallClusters {
		reinstall[workflow.ObjectKindContainerCluster+"/"+name] = true
	}
	out := &DryRunTransitions{Objects: []DryRunTransition{}}
	for _, tr := range workflow.ClassifyApplyTransitions(objects, mode) {
		action := tr.Action
		if reinstall[tr.Label] {
			action = workflow.ApplyTransitionRebuild
		}
		out.Objects = append(out.Objects, DryRunTransition{Label: tr.Label, Action: string(action)})
	}
	return out
}

type DryRunAddonPlan struct {
	Cluster   string                          `json:"cluster"`
	Binding   string                          `json:"binding"`
	Addon     string                          `json:"addon"`
	Type      string                          `json:"type"`
	Resources []extensionplan.ResourceSummary `json:"resources"`
}

type DryRunDestroySafety struct {
	AuthorizationRequired bool     `json:"authorizationRequired"`
	Authorized            bool     `json:"authorized"`
	Reasons               []string `json:"reasons"`
}

func BuildScopeDryRunReport(cmdCtx context.Context, ctx workspace.Context, executable string, bundleDir string, scope Scope, action string, state v1alpha1.State, selected []Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, check bool, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []workflow.ApplyTask, destroySafety *DryRunDestroySafety, transitions *DryRunTransitions, forks int) (DryRunReport, error) {
	clustersDir := workspace.ControllerClustersDir(ctx.Name)
	runner := ansible.CommandRunner{Stdout: io.Discard, Stderr: io.Discard}
	opts := runOptionsForContext(ctx, clustersDir, executable, state)
	opts.BundleDir = bundleDir
	opts.Playbook = playbook
	opts.Limit = limit
	opts.Forks = forks
	opts.ExtraVarPairs = extraVarPairs
	opts.ArtifactsBaseName = artifactsBaseName
	opts.Check = check
	opts.AskBecomePass = askBecomePass
	opts.DryRun = true
	opts.ResolveInstaller = resolveInstaller
	opts.Label = scope.Name + " " + action
	runResult, err := workflow.Run(cmdCtx, opts, runner, nil)
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
			RunStatus:   string(workflow.RunStatusRunning),
			Limits:      limits,
			Tasks:       workflow.TaskLedgerEntries(tasks),
			Addons:      dryRunExtensionPlans(tasks),
			Transitions: transitions,
		}
	}
	return report, nil
}

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
		if task.Entry.Kind != workflow.ApplyTaskKindClusterAddon || task.Extension == nil {
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
