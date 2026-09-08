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
	"github.com/spf13/pflag"
)

type dispatchRecord struct {
	calls   int
	path    string
	ctx     context.Context
	request any
	err     error
}

func (r *dispatchRecord) called(ctx context.Context, path string, request any) error {
	r.calls++
	r.ctx, r.path, r.request = ctx, path, request
	return r.err
}

type contextsSpy struct{ record *dispatchRecord }

func (s contextsSpy) Init(ctx context.Context, request workspace.InitRequest) error {
	return s.record.called(ctx, "context init", request)
}

func (s contextsSpy) Update(ctx context.Context, request workspace.UpdateRequest) error {
	return s.record.called(ctx, "context update", request)
}

func (s contextsSpy) Use(ctx context.Context, request workspace.UseRequest) error {
	return s.record.called(ctx, "context use", request)
}

func (s contextsSpy) List(ctx context.Context, request workspace.ListRequest) error {
	return s.record.called(ctx, "context list", request)
}

func (s contextsSpy) Current(ctx context.Context, request workspace.CurrentRequest) error {
	return s.record.called(ctx, "context current", request)
}

func (s contextsSpy) Delete(ctx context.Context, request workspace.DeleteRequest) error {
	return s.record.called(ctx, "context delete", request)
}

type addOnCatalogSpy struct{ record *dispatchRecord }

func (s addOnCatalogSpy) List(ctx context.Context, request addons.ListRequest) error {
	return s.record.called(ctx, "add-ons list", request)
}

func (s addOnCatalogSpy) Add(ctx context.Context, request addons.AddRequest) error {
	return s.record.called(ctx, "add-ons add", request)
}

func (s addOnCatalogSpy) Delete(ctx context.Context, request addons.DeleteRequest) error {
	return s.record.called(ctx, "add-ons delete", request)
}

type secretsSpy struct{ record *dispatchRecord }

func (s secretsSpy) Set(ctx context.Context, request secrets.SetRequest) error {
	return s.record.called(ctx, "secret set", request)
}

func (s secretsSpy) Generate(ctx context.Context, request secrets.GenerateRequest) error {
	return s.record.called(ctx, "secret generate", request)
}

func (s secretsSpy) Check(ctx context.Context, request secrets.CheckRequest) error {
	return s.record.called(ctx, "secret check", request)
}

func (s secretsSpy) List(ctx context.Context, request secrets.ListRequest) error {
	return s.record.called(ctx, "secret list", request)
}

func (s secretsSpy) Show(ctx context.Context, request secrets.ShowRequest) error {
	return s.record.called(ctx, "secret show", request)
}

func (s secretsSpy) Delete(ctx context.Context, request secrets.DeleteRequest) error {
	return s.record.called(ctx, "secret delete", request)
}

type encryptionSpy struct{ record *dispatchRecord }

func (s encryptionSpy) Init(ctx context.Context, request secrets.EncryptionInitRequest) error {
	return s.record.called(ctx, "secret encryption init", request)
}

func (s encryptionSpy) Status(ctx context.Context, request secrets.EncryptionStatusRequest) error {
	return s.record.called(ctx, "secret encryption status", request)
}

func (s encryptionSpy) Rotate(ctx context.Context, request secrets.EncryptionRotateRequest) error {
	return s.record.called(ctx, "secret encryption rotate", request)
}

type mediaSpy struct{ record *dispatchRecord }

func (s mediaSpy) Add(ctx context.Context, request managedos.AddMediaRequest) error {
	return s.record.called(ctx, "media add", request)
}

func (s mediaSpy) List(ctx context.Context, request managedos.ListMediaRequest) error {
	return s.record.called(ctx, "media list", request)
}

func (s mediaSpy) Delete(ctx context.Context, request managedos.DeleteMediaRequest) error {
	return s.record.called(ctx, "media delete", request)
}

type desiredStateSpy struct{ record *dispatchRecord }

func (s desiredStateSpy) Validate(ctx context.Context, request desiredstate.ValidateRequest) error {
	return s.record.called(ctx, "validate", request)
}

func (s desiredStateSpy) RenderEffective(ctx context.Context, request desiredstate.EffectiveRequest) error {
	return s.record.called(ctx, "render effective", request)
}

type controllerSpy struct{ record *dispatchRecord }

func (s controllerSpy) Check(ctx context.Context, request controller.CheckRequest) error {
	return s.record.called(ctx, "preflight bastion", request)
}

func (s controllerSpy) Setup(ctx context.Context, request controller.SetupRequest) error {
	return s.record.called(ctx, "bastion setup", request)
}

type environmentSpy struct{ record *dispatchRecord }

func (s environmentSpy) PreflightInfrastructure(ctx context.Context, request environment.InfrastructurePreflightRequest) error {
	return s.record.called(ctx, "preflight infra", request)
}

func (s environmentSpy) PreflightClusters(ctx context.Context, request environment.ClustersPreflightRequest) error {
	return s.record.called(ctx, "preflight clusters", request)
}

func (s environmentSpy) PreflightAll(ctx context.Context, request environment.AllPreflightRequest) error {
	return s.record.called(ctx, "preflight all", request)
}

func (s environmentSpy) ListClusters(ctx context.Context, request environment.ListClustersRequest) error {
	return s.record.called(ctx, "cluster list", request)
}

func (s environmentSpy) ClusterInfo(ctx context.Context, request environment.ClusterInfoRequest) error {
	return s.record.called(ctx, "cluster info", request)
}

func (s environmentSpy) ClusterRsh(ctx context.Context, request environment.ClusterRshRequest) error {
	return s.record.called(ctx, "cluster rsh", request)
}

func (s environmentSpy) ClusterExec(ctx context.Context, request environment.ClusterExecRequest) error {
	return s.record.called(ctx, "cluster exec", request)
}

type containerPreflightSpy struct{ record *dispatchRecord }

func (s containerPreflightSpy) Check(ctx context.Context, request containercluster.PreflightRequest) error {
	return s.record.called(ctx, "preflight container-cluster", request)
}

type storagePreflightSpy struct{ record *dispatchRecord }

func (s storagePreflightSpy) Check(ctx context.Context, request storage.PreflightRequest) error {
	return s.record.called(ctx, "preflight storage-cluster", request)
}

type addOnPreflightSpy struct{ record *dispatchRecord }

func (s addOnPreflightSpy) Check(ctx context.Context, request addons.PreflightRequest) error {
	return s.record.called(ctx, "preflight add-ons", request)
}

type lifecycleSpy struct{ record *dispatchRecord }

func (s lifecycleSpy) Plan(ctx context.Context, request reconciliation.PlanRequest) error {
	return s.record.called(ctx, "plan", request)
}

func (s lifecycleSpy) Status(ctx context.Context, request reconciliation.StatusRequest) error {
	return s.record.called(ctx, "status", request)
}

func (s lifecycleSpy) Apply(ctx context.Context, request reconciliation.ApplyRequest) error {
	return s.record.called(ctx, "apply", request)
}

func (s lifecycleSpy) Destroy(ctx context.Context, request reconciliation.DestroyRequest) error {
	return s.record.called(ctx, "destroy", request)
}

type artifactsSpy struct{ record *dispatchRecord }

func (s artifactsSpy) Render(ctx context.Context, request nativeartifacts.RenderRequest) error {
	return s.record.called(ctx, "render", request)
}

type installerSpy struct{ record *dispatchRecord }

func (s installerSpy) Render(ctx context.Context, request containercluster.RenderInstallerRequest) error {
	return s.record.called(ctx, "render installer", request)
}

type storageArtifactsSpy struct{ record *dispatchRecord }

func (s storageArtifactsSpy) Render(ctx context.Context, request storage.RenderArtifactsRequest) error {
	return s.record.called(ctx, "render storage", request)
}

type machineInventorySpy struct{ record *dispatchRecord }

func (s machineInventorySpy) List(ctx context.Context, request machine.ListRequest) error {
	return s.record.called(ctx, "machine list", request)
}

type machineAccessSpy struct{ record *dispatchRecord }

func (s machineAccessSpy) Rsh(ctx context.Context, request machine.RshRequest) error {
	return s.record.called(ctx, "machine rsh", request)
}

func (s machineAccessSpy) Exec(ctx context.Context, request machine.ExecRequest) error {
	return s.record.called(ctx, "machine exec", request)
}

type machineTrustSpy struct{ record *dispatchRecord }

func (s machineTrustSpy) Enroll(ctx context.Context, request trust.EnrollRequest) error {
	return s.record.called(ctx, "machine trust", request)
}

type clusterAccessSpy struct{ record *dispatchRecord }

func (s clusterAccessSpy) OC(ctx context.Context, request containercluster.OCRequest) error {
	return s.record.called(ctx, "cluster oc", request)
}

func (s clusterAccessSpy) Kubectl(ctx context.Context, request containercluster.KubectlRequest) error {
	return s.record.called(ctx, "cluster kubectl", request)
}

func (s clusterAccessSpy) Kubeconfig(ctx context.Context, request containercluster.KubeconfigRequest) error {
	return s.record.called(ctx, "cluster kubeconfig", request)
}

func dispatchSpies(record *dispatchRecord) Services {
	return Services{
		Contexts:           contextsSpy{record},
		AddOnCatalog:       addOnCatalogSpy{record},
		Secrets:            secretsSpy{record},
		Encryption:         encryptionSpy{record},
		Media:              mediaSpy{record},
		DesiredState:       desiredStateSpy{record},
		Controller:         controllerSpy{record},
		Environment:        environmentSpy{record},
		ContainerPreflight: containerPreflightSpy{record},
		StoragePreflight:   storagePreflightSpy{record},
		AddOnPreflight:     addOnPreflightSpy{record},
		Lifecycle:          lifecycleSpy{record},
		Artifacts:          artifactsSpy{record},
		Installer:          installerSpy{record},
		StorageArtifacts:   storageArtifactsSpy{record},
		MachineInventory:   machineInventorySpy{record},
		MachineAccess:      machineAccessSpy{record},
		MachineTrust:       machineTrustSpy{record},
		ClusterAccess:      clusterAccessSpy{record},
	}
}

func dispatchFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	for name, value := range map[string]string{
		"context": "example", "name": "demo", "ssh-id-file": "key.pem",
		"ssh-user": "operator", "clusters": "first,second", "machines": "node-a,node-b",
		"replace": "node-b", "node": "node-a", "version": "", "part": "private",
		"from-file": "", "from-url": "https://example.invalid/image.iso",
		"sha256":      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"pull-secret": "", "tls-cert": "", "tls-key": "", "raw-file": "secret.bin",
		"username": "operator", "input-dir": "inputs", "output-dir": "artifacts",
	} {
		flags.String(name, value, "")
	}
	for _, name := range []string{
		"yes", "purge", "abandon-resources", "short", "renew", "checksums", "dry-run",
		"trust-on-first-use", "verbose", "watch", "sensitive", "silent", "secrets",
		"ssh-ask-sudo-password", "ssh-user-for-provisioned",
	} {
		flags.Bool(name, true, "")
	}
	flags.Bool("password-stdin", false, "")
	flags.Bool("generate", false, "")
	flags.StringArray("file", []string{"inputs"}, "")
	flags.StringArray("authorize", []string{"data-loss"}, "")
	flags.String("watch-interval", "7s", "")
	return flags
}
