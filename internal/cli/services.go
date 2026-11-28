package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/machine/inventory"
)

type Services struct {
	Policy                PolicyService
	Contexts              ContextService
	AddOnCatalog          AddOnCatalogService
	Secrets               SecretService
	Encryption            EncryptionService
	Media                 MediaService
	ArtifactCatalog       ArtifactCatalogService
	CatalogPurge          CatalogPurgeService
	DesiredState          DesiredStateService
	Controller            ControllerService
	EnvironmentPreflight  EnvironmentPreflightService
	EnvironmentInspection EnvironmentInspectionService
	AdminCredentials      AdminCredentialsService
	SignOn                SignOnService
	EnvironmentAccess     EnvironmentAccessService
	Lifecycle             LifecycleService
	Artifacts             ArtifactService
	Installer             InstallerService
	StorageArtifacts      StorageArtifactService
	MachineInventory      MachineInventoryService
	MachineAccess         MachineAccessService
	MachineCopy           MachineCopyService
	DiskCheck             func(context.Context, inventory.DiskCheckRequest) (*inventory.DiskCheckReport, error)
	MachinePower          MachinePowerService
	MachineDiscovery      MachineDiscoveryService
	MachineEnrollment     MachineEnrollmentService
	MachineTrust          MachineTrustService
	ClusterAccess         ClusterAccessService
	Comparison            ComparisonService
	Logs                  LogsService
	// logOutput is the invocation's stdout, where a followed log streams.
	logOutput io.Writer
}
