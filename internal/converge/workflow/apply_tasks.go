package workflow

import (
	"io"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/roles"
)

const (
	ApplyTaskKindProvider                 = "providerServices"
	ApplyTaskKindInfraComponentServices   = "infraComponentServices"
	ApplyTaskKindControllerNameResolution = "controllerNameResolution"
	ApplyTaskKindMachineInfraPrepare      = "machineInfraPrepare"
	ApplyTaskKindClusterInstall           = "clusterInstall"
	ApplyTaskKindMachineInfraFinalize     = "machineInfraFinalize"
	ApplyTaskKindManagedMachineOS         = "managedMachineOS"
	ApplyTaskKindMachineRegistration      = "machineRegistration"
	ApplyTaskKindMachineRepositories      = "machineRepositories"
	ApplyTaskKindStorageNodeAccess        = "storageNodeAccess"
	ApplyTaskKindStorageInfra             = "storageInfra"
	ApplyTaskKindClusterISO               = "clusterISO"
	ApplyTaskKindHostVirtctl              = "hostVirtctl"
	ApplyTaskKindNodeBoot                 = "nodeBoot"
	ApplyTaskKindBootstrapWait            = "bootstrapWait"
	ApplyTaskKindInstallWait              = "installWait"
	ApplyTaskKindStorageCluster           = "storageCluster"
	ApplyTaskKindClusterAddon             = "clusterAddon"
	ApplyTaskKindNodeConfigApply          = "nodeConfigApply"
	ApplyTaskKindPlaybook                 = "provisioningPlaybook"

	ApplyClusterKindContainer = "container"
	ApplyClusterKindStorage   = "storage"

	ApplyPhaseFabric   = "fabric"
	ApplyPhaseMachines = "machines"
	ApplyPhaseDeps     = "deps"
	ApplyPhaseBase     = "base"
	ApplyPhaseAddons   = "add-ons"

	applyProviderPlaybook            = roles.PlaybookTaskProviderServicesApply
	applyInfraComponentsPlaybook     = roles.PlaybookTaskInfraComponentServicesApply
	applyControllerNameResolution    = roles.PlaybookTaskControllerNameResolutionApply
	applyMachineInfraPrepare         = roles.PlaybookTaskMachineInfraPrepare
	applyClusterInstallPlaybook      = roles.PlaybookTaskMachineInfraApply
	applyMachineInfraFinalize        = roles.PlaybookTaskMachineInfraFinalize
	applyManagedMachineOSPlaybook    = roles.PlaybookTaskManagedMachineOSApply
	applyMachineRegistrationPlaybook = roles.PlaybookTaskMachineRegistrationApply
	applyMachineRepositoriesPlaybook = roles.PlaybookTaskMachineRepositoriesApply
	applyStorageNodeAccessPlaybook   = roles.PlaybookTaskStorageNodeAccessApply
	applyCreateISOPlaybook           = roles.PlaybookTaskContainerClusterCreateAgentISO
	applyHostVirtctlPlaybook         = roles.PlaybookTaskHostVirtctlProvision
	applyBootMachinePlaybook         = roles.PlaybookTaskContainerClusterBootAgentMachine
	applyWaitInstallPlaybook         = roles.PlaybookTaskContainerClusterWaitAgentInstall
	applyStoragePlaybook             = roles.PlaybookTaskStorageClusterApply
)

func ApplyTaskKinds() []string {
	return []string{
		ApplyTaskKindProvider,
		ApplyTaskKindInfraComponentServices,
		ApplyTaskKindControllerNameResolution,
		ApplyTaskKindMachineInfraPrepare,
		ApplyTaskKindClusterInstall,
		ApplyTaskKindMachineInfraFinalize,
		ApplyTaskKindManagedMachineOS,
		ApplyTaskKindMachineRegistration,
		ApplyTaskKindMachineRepositories,
		ApplyTaskKindStorageNodeAccess,
		ApplyTaskKindStorageInfra,
		ApplyTaskKindClusterISO,
		ApplyTaskKindHostVirtctl,
		ApplyTaskKindNodeBoot,
		ApplyTaskKindBootstrapWait,
		ApplyTaskKindInstallWait,
		ApplyTaskKindStorageCluster,
		ApplyTaskKindClusterAddon,
		ApplyTaskKindNodeConfigApply,
		ApplyTaskKindPlaybook,
	}
}

func ApplyTaskKindIsReconfigureOnly(kind string) bool {
	return overrideReconfigureOnlyKinds[kind]
}

type ApplyMode string

const (
	ApplyModeCreate    ApplyMode = "create"
	ApplyModeReconcile ApplyMode = "reconcile"
	ApplyModeRebuild   ApplyMode = "rebuild"
)

func ApplyModeNames() []string {
	return []string{string(ApplyModeCreate), string(ApplyModeReconcile), string(ApplyModeRebuild)}
}

func (m ApplyMode) InstallOverride() bool { return m == ApplyModeRebuild }

func (m ApplyMode) Valid() bool {
	switch m {
	case ApplyModeCreate, ApplyModeReconcile, ApplyModeRebuild:
		return true
	default:
		return false
	}
}

type ApplyTarget struct {
	Name                string
	PhaseNames          []string
	StorageClusterNames []string
	ClusterKind         string
	MachineProvision    map[string]bool
	FabricHosts         map[string]bool
	GitSourceRoots      map[string]string
}

type ApplyTaskExecutionClass string

const ApplyTaskExecutionLiveProof ApplyTaskExecutionClass = "liveProof"

func (t ApplyTarget) MachineScoped() bool { return len(t.MachineProvision) > 0 }

func (t ApplyTarget) MachineIncluded(machine string) bool {
	return !t.MachineScoped() || t.MachineProvision[machine]
}

func (t ApplyTarget) FabricHostIncluded(host string) bool {
	return t.FabricHosts == nil || t.FabricHosts[host]
}

type ApplyTask struct {
	Entry              TaskLedgerEntry
	Playbook           string
	Limit              string
	Forks              int
	RedfishSlots       int
	HostSlotKey        string
	HostSlotCount      int
	ExtraVarPairs      []string
	Tags               []string
	SkipTags           []string
	Timeout            time.Duration
	RolesPath          string
	CollectionsPath    string
	SkipWhenConverged  bool
	ExecutionClass     ApplyTaskExecutionClass
	FailureRemedy      remedy.Request
	State              v1alpha1.State
	DesiredHashState   *v1alpha1.State
	DesiredHashVars    any
	StructuralHashVars any
	Extension          *extensionplan.ExtensionPlan
	hashes             *applyTaskHashCache
}

type applyTaskResult struct {
	id            string
	skipped       bool
	skippedReason string
	failure       string
	err           error
}

type ApplyReporter interface {
	RunStart(ledger RunLedger)
	StageSnapshot(ledger RunLedger)
	RunSummary(ledger RunLedger)
	PromptGap()
}

type ApplyTaskRunnerFactory func(stdout io.Writer, stderr io.Writer) ansible.Runner

type PreparedApplyTaskGraph struct {
	RunID     string
	StartedAt time.Time
	Tasks     []ApplyTask
	Limits    ConcurrencyLimits
}
