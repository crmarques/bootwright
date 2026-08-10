package status

import (
	"sort"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type Report struct {
	Context          Context             `json:"context"`
	Error            string              `json:"error,omitempty"`
	SetupChecks      []SetupCheck        `json:"setupChecks,omitempty"`
	Desired          Desired             `json:"desired"`
	Clusters         []Cluster           `json:"clusters"`
	StorageClusters  []StorageCluster    `json:"storageClusters"`
	Shared           []Shared            `json:"shared"`
	Secrets          []SecretEntry       `json:"secrets"`
	NextSteps        []string            `json:"nextSteps"`
	ApplyRun         *workflow.RunLedger `json:"applyRun,omitempty"`
	ApplyRunActivity *ApplyRunActivity   `json:"applyRunActivity,omitempty"`
}

type StorageCluster struct {
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	Management string `json:"management,omitempty"`
}

type SetupCheck struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

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
	Substrate          string      `json:"substrate,omitempty"`
	HostCluster        string      `json:"hostCluster,omitempty"`
	InstallerFreshness string      `json:"installerFreshness"`
	InstallerPath      string      `json:"installerPath,omitempty"`
	EffectiveStatePath string      `json:"effectiveStatePath,omitempty"`
	FreshnessDetail    string      `json:"freshnessDetail,omitempty"`
	Addons             []Extension `json:"addons,omitempty"`
}

type Extension struct {
	Name            string                            `json:"name"`
	Status          string                            `json:"status,omitempty"`
	Phase           string                            `json:"phase,omitempty"`
	DesiredHash     string                            `json:"desiredHash,omitempty"`
	UpdatedAt       string                            `json:"updatedAt,omitempty"`
	LastObserved    string                            `json:"lastObserved,omitempty"`
	CSVObservations []extensionrecords.CSVObservation `json:"csvObservations,omitempty"`
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
		sub := stateview.ContainerClusterSubstrate(state, ocp)
		entry := Cluster{
			Name:               name,
			InstallMode:        v1alpha1.InstallMode(ocp),
			InstallMethod:      ocp.Spec.Install.Method,
			Substrate:          sub.Provider,
			HostCluster:        sub.Host,
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

func BuildStorageClusters(state v1alpha1.State) []StorageCluster {
	out := make([]StorageCluster, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		management := cluster.Spec.Management
		if management == "" {
			management = v1alpha1.StorageClusterManagementManaged
		}
		out = append(out, StorageCluster{
			Name:       cluster.Metadata.Name,
			Type:       cluster.Spec.Type,
			Management: management,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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
				entry.LastObserved = record.LastObserved
				entry.CSVObservations = append([]extensionrecords.CSVObservation(nil), record.CSVObservations...)
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
	for _, accessor := range v1alpha1.AuthoredKindAccessors() {
		if len(accessor.Names(s)) > 0 {
			return true
		}
	}
	return false
}
