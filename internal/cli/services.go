package cli

import "io"

type Services struct {
	Contexts              ContextService
	AddOnCatalog          AddOnCatalogService
	Secrets               SecretService
	Encryption            EncryptionService
	Media                 MediaService
	DesiredState          DesiredStateService
	Controller            ControllerService
	EnvironmentPreflight  EnvironmentPreflightService
	EnvironmentInspection EnvironmentInspectionService
	EnvironmentAccess     EnvironmentAccessService
	Lifecycle             LifecycleService
	Artifacts             ArtifactService
	Installer             InstallerService
	StorageArtifacts      StorageArtifactService
	MachineInventory      MachineInventoryService
	MachineAccess         MachineAccessService
	MachineCopy           MachineCopyService
	MachinePower          MachinePowerService
	MachineTrust          MachineTrustService
	ClusterAccess         ClusterAccessService
	Comparison            ComparisonService
	Logs                  LogsService
	// logOutput is the invocation's stdout, where a followed log streams.
	logOutput io.Writer
}
