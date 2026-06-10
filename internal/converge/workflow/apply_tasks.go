package workflow

import (
	"io"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/roles"
)

const (
	ApplyTaskKindProvider               = "providerServices"
	ApplyTaskKindInfraComponentServices = "infraComponentServices"
	ApplyTaskKindMachineInfraPrepare    = "machineInfraPrepare"
	ApplyTaskKindClusterInstall         = "clusterInstall"
	ApplyTaskKindMachineInfraFinalize   = "machineInfraFinalize"
	ApplyTaskKindManagedMachineOS       = "managedMachineOS"
	ApplyTaskKindStorageInfra           = "storageInfra"
	ApplyTaskKindClusterISO             = "clusterISO"
	ApplyTaskKindNodeBoot               = "nodeBoot"
	ApplyTaskKindInstallWait            = "installWait"
	ApplyTaskKindStorageCluster         = "storageCluster"
	ApplyTaskKindStorageAttachmentApply = "storageAttachmentApply"
	ApplyTaskKindClusterAddonApply      = "clusterAddonApply"
	ApplyTaskKindClusterAddonWait       = "clusterAddonWait"

	ApplyClusterKindContainer = "container"
	ApplyClusterKindStorage   = "storage"

	// Phase gates. Two families — infra (fabric, machines) and clusters
	// (deps, base, addons) — each an ordered set of CLI-selectable sub-phases.
	// These are coarse gates over the task graph; true ordering is the
	// capability/explicit-dependency DAG. deps and base are shared by storage
	// and container clusters; the planner filters tasks by cluster kind.
	ApplyPhaseFabric   = "fabric"   // provider hosts + BMC + shared services
	ApplyPhaseMachines = "machines" // substrate prepare/instantiate/OS/finalize
	ApplyPhaseDeps     = "deps"     // per-cluster install prereqs: cephadm / agent ISO
	ApplyPhaseBase     = "base"     // bring control planes up: ceph bootstrap / boot+wait
	ApplyPhaseAddons   = "addons"   // post-install: addons + storage attachment

	applyProviderPlaybook         = roles.PlaybookTaskProviderServicesApply
	applyInfraComponentsPlaybook  = roles.PlaybookTaskInfraComponentServicesApply
	applyMachineInfraPrepare      = roles.PlaybookTaskMachineInfraPrepare
	applyClusterInstallPlaybook   = roles.PlaybookTaskMachineInfraApply
	applyMachineInfraFinalize     = roles.PlaybookTaskMachineInfraFinalize
	applyManagedMachineOSPlaybook = roles.PlaybookTaskManagedMachineOSApply
	applyCreateISOPlaybook        = roles.PlaybookTaskContainerClusterCreateAgentISO
	applyBootMachinePlaybook      = roles.PlaybookTaskContainerClusterBootAgentMachine
	applyWaitInstallPlaybook      = roles.PlaybookTaskContainerClusterWaitAgentInstall
	applyStoragePlaybook          = roles.PlaybookTaskStorageClusterApply
)

// ApplyMode is the explicit safety mode an apply runs under. The CLI chooses it
// (`apply` = continue, the default; `apply --expect-new` = create; `apply
// --override`); it drives both the Go object preflight and the per-role Ansible
// gate via the bootwright_apply_mode extra-var.
//
//   - create:   greenfield assert (--expect-new). Refuse if any selected object already exists.
//   - continue: safe reconcile (the default). Create what is missing, skip what matches, fail on drift.
//   - override: break-glass. Rebuild drifted/owned objects; never touch foreign ones.
type ApplyMode string

const (
	ApplyModeCreate   ApplyMode = "create"
	ApplyModeContinue ApplyMode = "continue"
	ApplyModeOverride ApplyMode = "override"
)

// InstallOverride reports whether the mode authorizes Bootwright-owned destructive
// rebuilds — the semantics the legacy bootwright_install_override boolean carried.
func (m ApplyMode) InstallOverride() bool { return m == ApplyModeOverride }

// Valid reports whether m is one of the known modes.
func (m ApplyMode) Valid() bool {
	switch m {
	case ApplyModeCreate, ApplyModeContinue, ApplyModeOverride:
		return true
	default:
		return false
	}
}

type ApplyTarget struct {
	Name                string
	PhaseNames          []string
	StorageClusterNames []string
	// ClusterKind restricts which cluster-kind's deps/base/addons work is
	// planned: "" plans both, ApplyClusterKindStorage plans only storage
	// clusters, ApplyClusterKindContainer plans only container clusters. The
	// single-kind scopes (storage-cluster, container-cluster) set it so the
	// shared deps/base gates do not pull in the other kind.
	ClusterKind string
}

type ApplyTask struct {
	Entry         TaskLedgerEntry
	Playbook      string
	Limit         string
	Forks         int
	RedfishSlots  int
	HostSlotKey   string
	HostSlotCount int
	ExtraVarPairs []string
	State         v1alpha1.State
	// DesiredHashVars, when set, is the desired-state input ApplyTaskDesiredHash
	// hashes instead of the full State. Fabric (provider/infra-component) tasks
	// set it to the host's rendered fabric vars so state-check does not report
	// drift when an unrelated part of the fleet changes. Other tasks leave it nil
	// and continue to hash State.
	DesiredHashVars   any
	Extension         *extensionplan.ExtensionPlan
	StorageAttachment *StorageAttachmentPlan
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
