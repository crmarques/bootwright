package cli

import (
	"io"
	"sort"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/extensions/records"
	"github.com/crmarques/bootwright/internal/state/graph"
)

type statusReport struct {
	Context  statusContext   `json:"context"`
	Desired  statusDesired   `json:"desired"`
	Clusters []statusCluster `json:"clusters"`
	// Shared lists infra component services that two or more clusters
	// reference. Each entry maps to one Ansible-converged host instance.
	Shared           []statusShared          `json:"shared"`
	Secrets          []secretListEntry       `json:"secrets"`
	NextSteps        []string                `json:"nextSteps"`
	ApplyRun         *workflow.RunLedger     `json:"applyRun,omitempty"`
	ApplyRunActivity *statusApplyRunActivity `json:"applyRunActivity,omitempty"`
}

type statusShared struct {
	Kind              string   `json:"kind"`
	ProviderName      string   `json:"providerName"`
	CapabilityName    string   `json:"capabilityName"`
	HostRef           string   `json:"hostRef,omitempty"`
	ConsumingClusters []string `json:"consumingClusters"`
}

type statusContext struct {
	Name               string `json:"name"`
	ContextDir         string `json:"contextDir"`
	InputDir           string `json:"inputDir"`
	RenderedDir        string `json:"renderedDir"`
	ClustersDir        string `json:"clustersDir"`
	RunsDir            string `json:"runsDir"`
	ManagedServicesDir string `json:"managedServicesDir"`
	ProviderStateDir   string `json:"providerStateDir"`
	SecretsDir         string `json:"secretsDir"`
}

type statusApplyRunActivity struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type statusDesired struct {
	Source                   string `json:"source"`
	Loaded                   bool   `json:"loaded"`
	LoadError                string `json:"loadError,omitempty"`
	Environments             int    `json:"environments"`
	InfraProviders           int    `json:"infraProviders"`
	Hosts                    int    `json:"hosts"`
	ClusterInfras            int    `json:"clusterInfras"`
	ContainerClusters        int    `json:"containerClusters"`
	ClusterExtensions        int    `json:"clusterExtensions"`
	ClusterExtensionSets     int    `json:"clusterExtensionSets"`
	ClusterExtensionBindings int    `json:"clusterExtensionBindings"`
}

type statusCluster struct {
	Name               string            `json:"name"`
	InstallMode        string            `json:"installMode,omitempty"`
	InstallMethod      string            `json:"installMethod,omitempty"`
	InstallerFreshness string            `json:"installerFreshness"`
	InstallerPath      string            `json:"installerPath,omitempty"`
	EffectiveStatePath string            `json:"effectiveStatePath,omitempty"`
	FreshnessDetail    string            `json:"freshnessDetail,omitempty"`
	Extensions         []statusExtension `json:"extensions,omitempty"`
}

type statusExtension struct {
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	Phase       string `json:"phase,omitempty"`
	DesiredHash string `json:"desiredHash,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

func runStatusJSON(stdout io.Writer, cf *commonFlags) error {
	report, err := buildStatusReport(cf)
	if err != nil {
		return failErr(1, err)
	}
	return output.JSON(stdout, report)
}

func buildStatusReport(cf *commonFlags) (statusReport, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return statusReport{}, err
	}
	state, loadErr := loadOptionalDesiredState(cf)
	stateLoaded := loadErr == nil && hasAnyState(state)

	report := statusReport{
		Context: statusContext{
			Name:               ctx.Name,
			ContextDir:         ctx.BaseDir,
			InputDir:           ctx.InputDir,
			RenderedDir:        ctx.RenderedDir,
			ClustersDir:        ctx.ClustersDir,
			RunsDir:            ctx.RunsDir,
			ManagedServicesDir: ctx.ManagedServicesDir,
			ProviderStateDir:   ctx.ProviderStateDir,
			SecretsDir:         ctx.SecretsDir,
		},
		Desired: statusDesired{
			Source: stateSource(cf),
			Loaded: stateLoaded,
		},
		Clusters:  []statusCluster{},
		Shared:    []statusShared{},
		NextSteps: nextStepHints(stateLoaded, state, ctx.RenderedDir, ctx.ClustersDir, ctx.SecretsDir),
	}
	if loadErr != nil {
		report.Desired.LoadError = loadErr.Error()
	}
	if stateLoaded {
		report.Desired.Environments = len(state.Environments)
		report.Desired.InfraProviders = len(state.InfraProviders)
		report.Desired.Hosts = len(state.Hosts)
		report.Desired.ClusterInfras = len(state.ClusterInfras)
		report.Desired.ContainerClusters = len(state.ContainerClusters)
		report.Desired.ClusterExtensions = len(state.ClusterExtensions)
		report.Desired.ClusterExtensionSets = len(state.ClusterExtensionSets)
		report.Desired.ClusterExtensionBindings = len(state.ClusterExtensionBindings)
		report.Clusters = buildStatusClusters(state, ctx.RenderedDir, ctx.ClustersDir)
		report.Shared = buildStatusShared(state)
		report.Secrets, _ = declaredSecretEntries(ctx.SecretsDir, state)
	}
	if ledger, ok, err := workflow.LoadRunLedger(ctx.RunsDir); err == nil && ok {
		report.ApplyRun = &ledger
		activity, _ := workflow.AssessRunActivity(ctx.RunsDir, ledger, time.Now())
		report.ApplyRunActivity = &statusApplyRunActivity{State: string(activity.State), Detail: activity.Detail}
		report.NextSteps = ledgerNextSteps(ledger, activity, report.NextSteps)
	} else if err != nil {
		report.NextSteps = append([]string{"inspect apply ledger: " + err.Error()}, report.NextSteps...)
	}
	return report, nil
}

func buildStatusShared(state v1alpha1.State) []statusShared {
	groups := stategraph.ResolveProviderServices(state).SharedServices()
	out := make([]statusShared, 0, len(groups))
	for _, g := range groups {
		out = append(out, statusShared{
			Kind:              g.Kind,
			ProviderName:      g.ProviderName,
			CapabilityName:    g.CapabilityName,
			HostRef:           g.HostRef,
			ConsumingClusters: g.ConsumingClusters,
		})
	}
	return out
}

func buildStatusClusters(state v1alpha1.State, renderedDir, clustersDir string) []statusCluster {
	freshness := loadEffectiveStateFreshness(state, renderedDir)
	extensionStatus := buildStatusExtensions(state, clustersDir)
	names := make([]string, 0, len(state.ContainerClusters))
	byName := map[string]v1alpha1.ContainerCluster{}
	for _, c := range state.ContainerClusters {
		names = append(names, c.Metadata.Name)
		byName[c.Metadata.Name] = c
	}
	sort.Strings(names)
	out := make([]statusCluster, 0, len(names))
	for _, name := range names {
		ocp := byName[name]
		installer := installerInstallConfigPath(clustersDir, name)
		result := freshnessForInstaller(freshness, installer)
		entry := statusCluster{
			Name:               name,
			InstallMode:        v1alpha1.InstallMode(ocp),
			InstallMethod:      ocp.Spec.Install.Method,
			InstallerFreshness: result.State,
			Extensions:         extensionStatus[name],
		}
		if result.State != installerFreshnessMissing {
			entry.InstallerPath = installer
			entry.FreshnessDetail = result.Error
			if result.Path != installer {
				entry.EffectiveStatePath = result.Path
			}
		}
		out = append(out, entry)
	}
	return out
}

func buildStatusExtensions(state v1alpha1.State, clustersDir string) map[string][]statusExtension {
	out := map[string][]statusExtension{}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return out
	}
	for _, plan := range plans {
		for _, extension := range plan.Extensions {
			entry := statusExtension{Name: extension.Name}
			record, found, err := extensionrecords.LoadRecord(clustersDir, plan.Cluster, extension.Name)
			if err == nil && found {
				entry.Status = string(record.Status)
				entry.Phase = string(record.Phase)
				entry.DesiredHash = record.DesiredHash
				if !record.UpdatedAt.IsZero() {
					entry.UpdatedAt = record.UpdatedAt.Format(time.RFC3339)
				}
			}
			out[plan.Cluster] = append(out[plan.Cluster], entry)
		}
	}
	return out
}
