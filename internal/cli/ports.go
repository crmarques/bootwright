package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/nativeartifacts"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace"
)

type Services struct {
	Contexts           ContextService
	AddOnCatalog       AddOnCatalogService
	Secrets            SecretService
	Encryption         EncryptionService
	Media              MediaService
	DesiredState       DesiredStateService
	Controller         ControllerService
	Environment        EnvironmentService
	ContainerPreflight ContainerPreflightService
	StoragePreflight   StoragePreflightService
	AddOnPreflight     AddOnPreflightService
	Lifecycle          LifecycleService
	Artifacts          ArtifactService
	Installer          InstallerService
	StorageArtifacts   StorageArtifactService
	MachineInventory   MachineInventoryService
	MachineAccess      MachineAccessService
	MachineTrust       MachineTrustService
	ClusterAccess      ClusterAccessService
}

type ContextService interface {
	Init(context.Context, workspace.InitRequest) error
	Update(context.Context, workspace.UpdateRequest) error
	Use(context.Context, workspace.UseRequest) error
	List(context.Context, workspace.ListRequest) error
	Current(context.Context, workspace.CurrentRequest) error
	Delete(context.Context, workspace.DeleteRequest) error
}

type AddOnCatalogService interface {
	List(context.Context, addons.ListRequest) error
	Add(context.Context, addons.AddRequest) error
	Delete(context.Context, addons.DeleteRequest) error
}

type SecretService interface {
	Set(context.Context, secrets.SetRequest) error
	Generate(context.Context, secrets.GenerateRequest) error
	Check(context.Context, secrets.CheckRequest) error
	List(context.Context, secrets.ListRequest) error
	Show(context.Context, secrets.ShowRequest) error
	Delete(context.Context, secrets.DeleteRequest) error
}

type EncryptionService interface {
	Init(context.Context, secrets.EncryptionInitRequest) error
	Status(context.Context, secrets.EncryptionStatusRequest) error
	Rotate(context.Context, secrets.EncryptionRotateRequest) error
}

type MediaService interface {
	Add(context.Context, managedos.AddMediaRequest) error
	List(context.Context, managedos.ListMediaRequest) error
	Delete(context.Context, managedos.DeleteMediaRequest) error
}

type DesiredStateService interface {
	Validate(context.Context, desiredstate.ValidateRequest) error
	RenderEffective(context.Context, desiredstate.EffectiveRequest) error
}

type ControllerService interface {
	Check(context.Context, controller.CheckRequest) error
	Setup(context.Context, controller.SetupRequest) error
}

type EnvironmentService interface {
	PreflightInfrastructure(context.Context, environment.InfrastructurePreflightRequest) error
	PreflightClusters(context.Context, environment.ClustersPreflightRequest) error
	PreflightAll(context.Context, environment.AllPreflightRequest) error
	ListClusters(context.Context, environment.ListClustersRequest) error
	ClusterInfo(context.Context, environment.ClusterInfoRequest) error
	ClusterRsh(context.Context, environment.ClusterRshRequest) error
	ClusterExec(context.Context, environment.ClusterExecRequest) error
}

type ContainerPreflightService interface {
	Check(context.Context, containercluster.PreflightRequest) error
}

type StoragePreflightService interface {
	Check(context.Context, storage.PreflightRequest) error
}

type AddOnPreflightService interface {
	Check(context.Context, addons.PreflightRequest) error
}

type LifecycleService interface {
	Plan(context.Context, reconciliation.PlanRequest) error
	Status(context.Context, reconciliation.StatusRequest) error
	Apply(context.Context, reconciliation.ApplyRequest) error
	Destroy(context.Context, reconciliation.DestroyRequest) error
}

type ArtifactService interface {
	Render(context.Context, nativeartifacts.RenderRequest) error
}

type InstallerService interface {
	Render(context.Context, containercluster.RenderInstallerRequest) error
}

type StorageArtifactService interface {
	Render(context.Context, storage.RenderArtifactsRequest) error
}

type MachineInventoryService interface {
	List(context.Context, machine.ListRequest) error
}

type MachineAccessService interface {
	Rsh(context.Context, machine.RshRequest) error
	Exec(context.Context, machine.ExecRequest) error
}

type MachineTrustService interface {
	Enroll(context.Context, trust.EnrollRequest) error
}

type ClusterAccessService interface {
	OC(context.Context, containercluster.OCRequest) error
	Kubectl(context.Context, containercluster.KubectlRequest) error
	Kubeconfig(context.Context, containercluster.KubeconfigRequest) error
}
