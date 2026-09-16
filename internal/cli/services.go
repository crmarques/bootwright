package cli

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
	ContainerPreflight    ContainerPreflightService
	StoragePreflight      StoragePreflightService
	AddOnPreflight        AddOnPreflightService
	Lifecycle             LifecycleService
	Artifacts             ArtifactService
	Installer             InstallerService
	StorageArtifacts      StorageArtifactService
	MachineInventory      MachineInventoryService
	MachineAccess         MachineAccessService
	MachinePower          MachinePowerService
	MachineTrust          MachineTrustService
	ClusterAccess         ClusterAccessService
}
