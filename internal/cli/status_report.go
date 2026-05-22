package cli

import (
	"io"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/workflow"
)

type statusReport struct {
	Context  statusContext   `json:"context"`
	Desired  statusDesired   `json:"desired"`
	Clusters []statusCluster `json:"clusters"`
	// Shared lists provider service capabilities that two or more clusters
	// reference. Each entry maps to one Ansible-converged host instance.
	Shared    []statusShared      `json:"shared"`
	NextSteps []string            `json:"nextSteps"`
	ApplyRun  *workflow.RunLedger `json:"applyRun,omitempty"`
}

type statusShared struct {
	Kind              string   `json:"kind"`
	ProviderName      string   `json:"providerName"`
	CapabilityName    string   `json:"capabilityName"`
	HostRef           string   `json:"hostRef,omitempty"`
	ConsumingClusters []string `json:"consumingClusters"`
}

type statusContext struct {
	Name         string `json:"name"`
	InputDir     string `json:"inputDir"`
	StateDir     string `json:"stateDir"`
	SecretsDir   string `json:"secretsDir"`
	HostStateDir string `json:"hostStateDir"`
}

type statusDesired struct {
	Source            string `json:"source"`
	Loaded            bool   `json:"loaded"`
	LoadError         string `json:"loadError,omitempty"`
	Environments      int    `json:"environments"`
	InfraProviders    int    `json:"infraProviders"`
	Hosts             int    `json:"hosts"`
	ClusterInfras     int    `json:"clusterInfras"`
	ContainerClusters int    `json:"containerClusters"`
}

type statusCluster struct {
	Name               string `json:"name"`
	InstallMode        string `json:"installMode,omitempty"`
	InstallMethod      string `json:"installMethod,omitempty"`
	InstallerFreshness string `json:"installerFreshness"`
	InstallerPath      string `json:"installerPath,omitempty"`
	EffectiveStatePath string `json:"effectiveStatePath,omitempty"`
	FreshnessDetail    string `json:"freshnessDetail,omitempty"`
}

func runStatusJSON(stdout io.Writer, cf *commonFlags, hostStateDir string) error {
	report, err := buildStatusReport(cf, hostStateDir)
	if err != nil {
		return failErr(1, err)
	}
	return output.JSON(stdout, report)
}

func buildStatusReport(cf *commonFlags, hostStateDir string) (statusReport, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return statusReport{}, err
	}
	state, loadErr := loadOptionalDesiredState(cf)
	stateLoaded := loadErr == nil && hasAnyState(state)

	report := statusReport{
		Context: statusContext{
			Name:         ctx.Name,
			InputDir:     ctx.InputDir,
			StateDir:     ctx.StateDir,
			SecretsDir:   ctx.SecretsDir,
			HostStateDir: hostStateDir,
		},
		Desired: statusDesired{
			Source: stateSource(cf),
			Loaded: stateLoaded,
		},
		Clusters:  []statusCluster{},
		Shared:    []statusShared{},
		NextSteps: nextStepHints(stateLoaded, state, ctx.StateDir),
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
		report.Clusters = buildStatusClusters(state, ctx.StateDir)
		report.Shared = buildStatusShared(state)
	}
	if ledger, ok, err := workflow.LoadRunLedger(ctx.StateDir); err == nil && ok {
		report.ApplyRun = &ledger
		report.NextSteps = ledgerNextSteps(ledger, report.NextSteps)
	} else if err != nil {
		report.NextSteps = append([]string{"inspect apply ledger: " + err.Error()}, report.NextSteps...)
	}
	return report, nil
}

func buildStatusShared(state v1alpha1.State) []statusShared {
	groups := render.SharedComponents(state)
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

func buildStatusClusters(state v1alpha1.State, stateDir string) []statusCluster {
	freshness := loadEffectiveStateFreshness(state, stateDir)
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
		installer := installerInstallConfigPath(stateDir, name)
		result := freshnessForInstaller(freshness, installer)
		entry := statusCluster{
			Name:               name,
			InstallMode:        v1alpha1.InstallMode(ocp),
			InstallMethod:      ocp.Spec.Install.Method,
			InstallerFreshness: result.State,
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
