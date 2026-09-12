package cli

import (
	"context"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/managedos/media"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/pflag"
)

type dispatchRecord struct {
	report    *compilation.Report
	result    commandResult
	calls     int
	path      string
	ctx       context.Context
	request   any
	err       error
	afterCall func()
	types     []string
	typeCalls int
}

func (r *dispatchRecord) called(ctx context.Context, path string, request any) error {
	r.calls++
	r.ctx, r.path, r.request = ctx, path, request
	if r.afterCall != nil {
		r.afterCall()
	}
	return r.err
}

type contextsSpy struct{ record *dispatchRecord }

func (s contextsSpy) Init(ctx context.Context, request contexts.InitRequest) (*contexts.AdmissionResult, error) {
	return s.record.result.admission, s.record.called(ctx, "context init", request)
}

func (s contextsSpy) Update(ctx context.Context, request contexts.UpdateRequest) (*contexts.AdmissionResult, error) {
	return s.record.result.admission, s.record.called(ctx, "context update", request)
}

func (s contextsSpy) Use(ctx context.Context, request contexts.UseRequest) (*contexts.UseResult, error) {
	return s.record.result.use, s.record.called(ctx, "context use", request)
}

func (s contextsSpy) List(ctx context.Context, request contexts.ListRequest) (*contexts.ListResult, error) {
	return s.record.result.list, s.record.called(ctx, "context list", request)
}

func (s contextsSpy) Current(ctx context.Context, request contexts.CurrentRequest) (*contexts.CurrentResult, error) {
	return s.record.result.current, s.record.called(ctx, "context current", request)
}

func (s contextsSpy) Delete(ctx context.Context, request contexts.DeleteRequest) (*contexts.DeleteResult, error) {
	return s.record.result.deletion, s.record.called(ctx, "context delete", request)
}

type addOnCatalogSpy struct{ record *dispatchRecord }

func (s addOnCatalogSpy) List(ctx context.Context, request addoncatalog.ListRequest) error {
	return s.record.called(ctx, "add-ons list", request)
}

func (s addOnCatalogSpy) Add(ctx context.Context, request addoncatalog.AddRequest) error {
	return s.record.called(ctx, "add-ons add", request)
}

func (s addOnCatalogSpy) Delete(ctx context.Context, request addoncatalog.DeleteRequest) error {
	return s.record.called(ctx, "add-ons delete", request)
}

type secretsSpy struct{ record *dispatchRecord }

func (s secretsSpy) Set(ctx context.Context, request custody.SetRequest) (*custody.MutationResult, error) {
	return s.record.result.secretMutation, s.record.called(ctx, "secret set", request)
}

func (s secretsSpy) Generate(ctx context.Context, request custody.GenerateRequest) (*custody.MutationResult, error) {
	return s.record.result.secretMutation, s.record.called(ctx, "secret generate", request)
}

func (s secretsSpy) Check(ctx context.Context, request custody.CheckRequest) (*custody.CheckResult, error) {
	return s.record.result.secretCheck, s.record.called(ctx, "secret check", request)
}

func (s secretsSpy) List(ctx context.Context, request custody.ListRequest) (*custody.ListResult, error) {
	return s.record.result.secretList, s.record.called(ctx, "secret list", request)
}

func (s secretsSpy) Show(ctx context.Context, request custody.ShowRequest) (*custody.RevealResult, error) {
	return s.record.result.secretReveal, s.record.called(ctx, "secret show", request)
}

func (s secretsSpy) Delete(ctx context.Context, request custody.DeleteRequest) (*custody.MutationResult, error) {
	return s.record.result.secretMutation, s.record.called(ctx, "secret delete", request)
}

type encryptionSpy struct{ record *dispatchRecord }

func (s encryptionSpy) Types() []string {
	s.record.typeCalls++
	return append([]string(nil), s.record.types...)
}

func (s encryptionSpy) Init(ctx context.Context, request encryption.EncryptionInitRequest) (*encryption.MutationResult, error) {
	return s.record.result.encryptionMutation, s.record.called(ctx, "secret encryption init", request)
}

func (s encryptionSpy) Status(ctx context.Context, request encryption.EncryptionStatusRequest) (*encryption.StatusResult, error) {
	return s.record.result.encryptionStatus, s.record.called(ctx, "secret encryption status", request)
}

func (s encryptionSpy) Rotate(ctx context.Context, request encryption.EncryptionRotateRequest) (*encryption.MutationResult, error) {
	return s.record.result.encryptionMutation, s.record.called(ctx, "secret encryption rotate", request)
}

type mediaSpy struct{ record *dispatchRecord }

func (s mediaSpy) Add(ctx context.Context, request media.AddMediaRequest) error {
	return s.record.called(ctx, "media add", request)
}

func (s mediaSpy) List(ctx context.Context, request media.ListMediaRequest) error {
	return s.record.called(ctx, "media list", request)
}

func (s mediaSpy) Delete(ctx context.Context, request media.DeleteMediaRequest) error {
	return s.record.called(ctx, "media delete", request)
}

type desiredStateSpy struct{ record *dispatchRecord }

func (s desiredStateSpy) Validate(ctx context.Context, request compilation.ValidateRequest) (*compilation.Report, error) {
	return s.record.report, s.record.called(ctx, "validate", request)
}

func (s desiredStateSpy) RenderEffective(ctx context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return s.record.result.effective, s.record.called(ctx, "render effective", request)
}

type controllerSpy struct{ record *dispatchRecord }

func (s controllerSpy) Check(ctx context.Context, request prerequisites.CheckRequest) (*prerequisites.Report, error) {
	return s.record.result.controller, s.record.called(ctx, "preflight bastion", request)
}

func (s controllerSpy) Setup(ctx context.Context, request prerequisites.SetupRequest) (*prerequisites.Report, error) {
	return s.record.result.controller, s.record.called(ctx, "bastion setup", request)
}

type environmentSpy struct{ record *dispatchRecord }

func (s environmentSpy) PreflightInfrastructure(ctx context.Context, request environmentpreflight.InfrastructurePreflightRequest) error {
	return s.record.called(ctx, "preflight infra", request)
}

func (s environmentSpy) PreflightClusters(ctx context.Context, request environmentpreflight.ClustersPreflightRequest) error {
	return s.record.called(ctx, "preflight clusters", request)
}

func (s environmentSpy) PreflightAll(ctx context.Context, request environmentpreflight.AllPreflightRequest) error {
	return s.record.called(ctx, "preflight all", request)
}

func (s environmentSpy) ListClusters(ctx context.Context, request inspection.ListClustersRequest) error {
	return s.record.called(ctx, "cluster list", request)
}

func (s environmentSpy) ClusterInfo(ctx context.Context, request inspection.ClusterInfoRequest) error {
	return s.record.called(ctx, "cluster info", request)
}

func (s environmentSpy) ClusterRsh(ctx context.Context, request environmentaccess.ClusterRshRequest) error {
	return s.record.called(ctx, "cluster rsh", request)
}

func (s environmentSpy) ClusterExec(ctx context.Context, request environmentaccess.ClusterExecRequest) error {
	return s.record.called(ctx, "cluster exec", request)
}

type containerPreflightSpy struct{ record *dispatchRecord }

func (s containerPreflightSpy) Check(ctx context.Context, request containerpreflight.PreflightRequest) error {
	return s.record.called(ctx, "preflight container-cluster", request)
}

type storagePreflightSpy struct{ record *dispatchRecord }

func (s storagePreflightSpy) Check(ctx context.Context, request storagepreflight.PreflightRequest) error {
	return s.record.called(ctx, "preflight storage-cluster", request)
}

type addOnPreflightSpy struct{ record *dispatchRecord }

func (s addOnPreflightSpy) Check(ctx context.Context, request addonpreflight.PreflightRequest) error {
	return s.record.called(ctx, "preflight add-ons", request)
}

type lifecycleSpy struct{ record *dispatchRecord }

func (s lifecycleSpy) Plan(ctx context.Context, request lifecycle.PlanRequest) (*lifecycle.PlanResult, error) {
	return nil, s.record.called(ctx, "plan", request)
}

func (s lifecycleSpy) Status(ctx context.Context, request lifecycle.StatusRequest) (*lifecycle.StatusResult, error) {
	return nil, s.record.called(ctx, "status", request)
}

func (s lifecycleSpy) Apply(ctx context.Context, request lifecycle.ApplyRequest) (*lifecycle.OperationResult, error) {
	return nil, s.record.called(ctx, "apply", request)
}

func (s lifecycleSpy) Destroy(ctx context.Context, request lifecycle.DestroyRequest) (*lifecycle.OperationResult, error) {
	return nil, s.record.called(ctx, "destroy", request)
}

type artifactsSpy struct{ record *dispatchRecord }

func (s artifactsSpy) Render(ctx context.Context, request artifactrendering.RenderRequest) error {
	return s.record.called(ctx, "render", request)
}

type installerSpy struct{ record *dispatchRecord }

func (s installerSpy) Render(ctx context.Context, request installation.RenderInstallerRequest) error {
	return s.record.called(ctx, "render installer", request)
}

type storageArtifactsSpy struct{ record *dispatchRecord }

func (s storageArtifactsSpy) Render(ctx context.Context, request storagerendering.RenderArtifactsRequest) error {
	return s.record.called(ctx, "render storage", request)
}

type machineInventorySpy struct{ record *dispatchRecord }

func (s machineInventorySpy) List(ctx context.Context, request inventory.ListRequest) error {
	return s.record.called(ctx, "machine list", request)
}

type machineAccessSpy struct{ record *dispatchRecord }

func (s machineAccessSpy) Rsh(ctx context.Context, request machineaccess.RshRequest) error {
	return s.record.called(ctx, "machine rsh", request)
}

func (s machineAccessSpy) Exec(ctx context.Context, request machineaccess.ExecRequest) error {
	return s.record.called(ctx, "machine exec", request)
}

type machineTrustSpy struct{ record *dispatchRecord }

func (s machineTrustSpy) Enroll(ctx context.Context, request enrollment.EnrollRequest) error {
	return s.record.called(ctx, "machine trust", request)
}

type clusterAccessSpy struct{ record *dispatchRecord }

func (s clusterAccessSpy) OC(ctx context.Context, request containeraccess.OCRequest) error {
	return s.record.called(ctx, "cluster oc", request)
}

func (s clusterAccessSpy) Kubectl(ctx context.Context, request containeraccess.KubectlRequest) error {
	return s.record.called(ctx, "cluster kubectl", request)
}

func (s clusterAccessSpy) Kubeconfig(ctx context.Context, request containeraccess.KubeconfigRequest) error {
	return s.record.called(ctx, "cluster kubeconfig", request)
}

func dispatchSpies(record *dispatchRecord) Services {
	return Services{
		Contexts:              contextsSpy{record},
		AddOnCatalog:          addOnCatalogSpy{record},
		Secrets:               secretsSpy{record},
		Encryption:            encryptionSpy{record},
		Media:                 mediaSpy{record},
		DesiredState:          desiredStateSpy{record},
		Controller:            controllerSpy{record},
		EnvironmentPreflight:  environmentSpy{record},
		EnvironmentInspection: environmentSpy{record},
		EnvironmentAccess:     environmentSpy{record},
		ContainerPreflight:    containerPreflightSpy{record},
		StoragePreflight:      storagePreflightSpy{record},
		AddOnPreflight:        addOnPreflightSpy{record},
		Lifecycle:             lifecycleSpy{record},
		Artifacts:             artifactsSpy{record},
		Installer:             installerSpy{record},
		StorageArtifacts:      storageArtifactsSpy{record},
		MachineInventory:      machineInventorySpy{record},
		MachineAccess:         machineAccessSpy{record},
		MachineTrust:          machineTrustSpy{record},
		ClusterAccess:         clusterAccessSpy{record},
	}
}

func dispatchFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	for name, value := range map[string]string{
		"context": "example", "name": "demo", "ssh-id-file": "key.pem",
		"ssh-user": "operator", "clusters": "first,second", "machines": "node-a,node-b",
		"replace": "node-b", "node": "node-a", "version": "", "part": "private-key",
		"from-file": "", "from-url": "https://example.invalid/image.iso",
		"sha256":     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"value-file": "secret.bin", "password-file": "", "certificate-file": "",
		"private-key-file": "", "public-key-file": "", "type": "local-keyring",
		"username": "", "input-dir": "inputs", "output-dir": "artifacts",
	} {
		flags.String(name, value, "")
	}
	for _, name := range []string{
		"yes", "purge", "short", "renew", "checksums", "dry-run",
		"trust-on-first-use", "verbose", "watch", "sensitive", "silent", "secrets",
		"ssh-ask-sudo-password", "ssh-user-for-provisioned",
	} {
		flags.Bool(name, true, "")
	}
	flags.Bool("password-stdin", false, "")
	flags.Bool("value-stdin", false, "")
	flags.StringArray("file", []string{"inputs"}, "")
	flags.StringArray("authorize", []string{"data-loss"}, "")
	flags.String("watch-interval", "7s", "")
	return flags
}
