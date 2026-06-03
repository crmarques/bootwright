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
	ApplyTaskKindClusterInfra           = "clusterInfra"
	ApplyTaskKindClusterISO             = "clusterISO"
	ApplyTaskKindNodeBoot               = "nodeBoot"
	ApplyTaskKindInstallWait            = "installWait"
	ApplyTaskKindStorageCluster         = "storageCluster"
	ApplyTaskKindStorageAttachmentApply = "storageAttachmentApply"
	ApplyTaskKindClusterAddonApply      = "clusterAddonApply"
	ApplyTaskKindClusterAddonWait       = "clusterAddonWait"

	ApplyClusterKindContainer = "container"
	ApplyClusterKindStorage   = "storage"

	ApplyPhaseProvider         = "provider"
	ApplyPhaseInfraComponents  = "infra-components"
	ApplyPhaseClusterInfra     = "cluster-infra"
	ApplyPhaseStorageCluster   = "storage-cluster"
	ApplyPhaseContainerCluster = "container-cluster"
	ApplyPhaseAddons           = "addons"

	applyProviderPlaybook        = "bootwright.core.task_provider_services_apply"
	applyInfraComponentsPlaybook = "bootwright.core.task_infra_component_services_apply"
	applyClusterInfraPlaybook    = "bootwright.core.task_machine_infra_apply"
	applyCreateISOPlaybook       = "bootwright.core.task_container_cluster_create_agent_iso"
	applyBootMachinePlaybook     = "bootwright.core.task_container_cluster_boot_agent_machine"
	applyWaitInstallPlaybook     = "bootwright.core.task_container_cluster_wait_agent_install"
	applyStoragePlaybook         = "bootwright.core.task_storage_cluster_apply"
)

type ApplyTarget struct {
	Name       string
	PhaseNames []string
}

type ApplyTask struct {
	Entry             TaskLedgerEntry
	Playbook          string
	Limit             string
	Forks             int
	RedfishSlots      int
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
