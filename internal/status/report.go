// Package status derives observe-stage analysis — the status report data
// model, installer freshness, ledger summarization, next-step recommendations,
// and state-check classification — from desired state and on-disk context
// artifacts. It returns plain data; presentation lives with the callers.
package status

import (
	"sort"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/graph"
)

type Report struct {
	Context          Context             `json:"context"`
	Error            string              `json:"error,omitempty"`
	SetupChecks      []SetupCheck        `json:"setupChecks,omitempty"`
	Desired          Desired             `json:"desired"`
	Clusters         []Cluster           `json:"clusters"`
	Shared           []Shared            `json:"shared"`
	Secrets          []SecretEntry       `json:"secrets"`
	NextSteps        []string            `json:"nextSteps"`
	ApplyRun         *workflow.RunLedger `json:"applyRun,omitempty"`
	ApplyRunActivity *ApplyRunActivity   `json:"applyRunActivity,omitempty"`
}

// SetupCheck is the JSON shape of a context readiness check in the status
// report.
type SetupCheck struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// SecretEntry is the JSON shape of a declared secret's local material status.
type SecretEntry struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Paths   []string `json:"paths"`
	Present bool     `json:"present"`
	Detail  string   `json:"detail,omitempty"`
}

type Shared struct {
	Kind              string   `json:"kind"`
	ProviderName      string   `json:"providerName"`
	CapabilityName    string   `json:"capabilityName"`
	MachineRef        string   `json:"machineRef,omitempty"`
	ConsumingClusters []string `json:"consumingClusters"`
}

type Context struct {
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

type ApplyRunActivity struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type Desired struct {
	Source                 string `json:"source"`
	Loaded                 bool   `json:"loaded"`
	LoadError              string `json:"loadError,omitempty"`
	Environments           int    `json:"environments"`
	Machines               int    `json:"machines"`
	MachineImages          int    `json:"machineImages"`
	MachineInstallProfiles int    `json:"machineInstallProfiles"`
	InfraProviders         int    `json:"infraProviders"`
	ContainerClusters      int    `json:"containerClusters"`
	StorageClusters        int    `json:"storageClusters"`
	StoragePools           int    `json:"storagePools"`
	ClusterAddons          int    `json:"clusterAddons"`
	ClusterAddonProfiles   int    `json:"clusterAddonProfiles"`
	ClusterAddonBindings   int    `json:"clusterAddonBindings"`
}

type Cluster struct {
	Name               string      `json:"name"`
	InstallMode        string      `json:"installMode,omitempty"`
	InstallMethod      string      `json:"installMethod,omitempty"`
	InstallerFreshness string      `json:"installerFreshness"`
	InstallerPath      string      `json:"installerPath,omitempty"`
	EffectiveStatePath string      `json:"effectiveStatePath,omitempty"`
	FreshnessDetail    string      `json:"freshnessDetail,omitempty"`
	Addons             []Extension `json:"addons,omitempty"`
}

type Extension struct {
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	Phase       string `json:"phase,omitempty"`
	DesiredHash string `json:"desiredHash,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

func BuildShared(state v1alpha1.State) []Shared {
	groups := stategraph.ResolveMachineServices(state).SharedServices()
	out := make([]Shared, 0, len(groups))
	for _, g := range groups {
		out = append(out, Shared{
			Kind:              g.Kind,
			ProviderName:      g.ProviderName,
			CapabilityName:    g.CapabilityName,
			MachineRef:        g.MachineRef,
			ConsumingClusters: g.ConsumingClusters,
		})
	}
	return out
}

func BuildClusters(state v1alpha1.State, renderedDir, clustersDir string) []Cluster {
	freshness := LoadEffectiveStateFreshness(state, renderedDir)
	extensionStatus := BuildAddons(state, clustersDir)
	names := make([]string, 0, len(state.ContainerClusters))
	byName := map[string]v1alpha1.ContainerCluster{}
	for _, c := range state.ContainerClusters {
		names = append(names, c.Metadata.Name)
		byName[c.Metadata.Name] = c
	}
	sort.Strings(names)
	out := make([]Cluster, 0, len(names))
	for _, name := range names {
		ocp := byName[name]
		installer := InstallerInstallConfigPath(clustersDir, name)
		result := FreshnessForInstaller(freshness, installer)
		entry := Cluster{
			Name:               name,
			InstallMode:        v1alpha1.InstallMode(ocp),
			InstallMethod:      ocp.Spec.Install.Method,
			InstallerFreshness: result.State,
			Addons:             extensionStatus[name],
		}
		if result.State != InstallerFreshnessMissing {
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

func BuildAddons(state v1alpha1.State, clustersDir string) map[string][]Extension {
	out := map[string][]Extension{}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return out
	}
	for _, plan := range plans {
		for _, extension := range plan.Addons {
			entry := Extension{Name: extension.Name}
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

func HasAnyState(s v1alpha1.State) bool {
	return len(s.Environments)+len(s.Machines)+len(s.MachineImages)+len(s.MachineInstallProfiles)+len(s.InfraProviders)+len(s.ContainerClusters)+len(s.StorageClusters)+len(s.StoragePlacementPolicies)+len(s.StoragePools)+len(s.StorageFilesystems)+len(s.StorageObjectGateways)+len(s.StorageExports)+len(s.ClusterAddons)+len(s.ClusterAddonProfiles)+len(s.ClusterAddonBindings) > 0
}
