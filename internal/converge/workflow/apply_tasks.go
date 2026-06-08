package workflow

import (
	"io"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/converge/ansible"
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

	applyProviderPlaybook         = "bootwright.core.task_provider_services_apply"
	applyInfraComponentsPlaybook  = "bootwright.core.task_infra_component_services_apply"
	applyMachineInfraPrepare      = "bootwright.core.task_machine_infra_prepare"
	applyClusterInstallPlaybook   = "bootwright.core.task_machine_infra_apply"
	applyMachineInfraFinalize     = "bootwright.core.task_machine_infra_finalize"
	applyManagedMachineOSPlaybook = "bootwright.core.task_managed_machine_os_apply"
	applyCreateISOPlaybook        = "bootwright.core.task_container_cluster_create_agent_iso"
	applyBootMachinePlaybook      = "bootwright.core.task_container_cluster_boot_agent_machine"
	applyWaitInstallPlaybook      = "bootwright.core.task_container_cluster_wait_agent_install"
	applyStoragePlaybook          = "bootwright.core.task_storage_cluster_apply"
)

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
	Entry             TaskLedgerEntry
	Playbook          string
	Limit             string
	Forks             int
	RedfishSlots      int
	HostSlotKey       string
	HostSlotCount     int
	ExtraVarPairs     []string
	State             v1alpha1.State
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
