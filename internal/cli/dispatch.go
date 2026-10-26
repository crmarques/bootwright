package cli

import (
	"context"
	"errors"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/pflag"
)

var errMissingService = errors.New("application service is not configured")

// commandResult is the closed set of successful application results understood
// by this executable. Unavailable error-only ports return its zero value.
type commandResult struct {
	addOns           *addoncatalog.ListResult
	validation       *compilation.Report
	admission        *contexts.AdmissionResult
	use              *contexts.UseResult
	list             *contexts.ListResult
	current          *contexts.CurrentResult
	deletion         *contexts.DeleteResult
	effective        *compilation.EffectiveResult
	controller       *prerequisites.Report
	rebind           *prerequisites.RebindResult
	storagePreflight *storagepreflight.Report

	lifecyclePlan      *lifecycle.PlanResult
	lifecycleStatus    *lifecycle.StatusResult
	lifecycleDiff      *lifecycle.DiffResult
	lifecycleOperation *lifecycle.OperationResult

	mediaMutation *media.MutationResult
	mediaList     *media.ListResult

	machines *inventory.ListResult
	session  *machine.SessionResult
	trust    *enrollment.Report
	power    *power.Results

	secretMutation     *custody.MutationResult
	secretCheck        *custody.CheckResult
	secretList         *custody.ListResult
	secretReveal       *custody.RevealResult
	encryptionMutation *encryption.MutationResult
	encryptionStatus   *encryption.StatusResult

	kubeconfig *containeraccess.KubeconfigResult
	keyring    *inspection.KeyringResult

	clusterList *inspection.ListResult
	clusterInfo *inspection.InfoResult
}

func (r commandResult) clearSensitive() {
	if r.secretReveal != nil {
		r.secretReveal.Material.Clear()
	}
	if r.kubeconfig != nil {
		r.kubeconfig.Material.Clear()
	}
	if r.keyring != nil {
		r.keyring.Material.Clear()
	}
}

func (s Services) invoke(ctx context.Context, path string, flags *pflag.FlagSet, args []string) (commandResult, error) {
	if err := ctx.Err(); err != nil {
		return commandResult{}, err
	}
	if flags == nil {
		return commandResult{}, errors.New("command flags are not configured")
	}
	values := requestValues{flags: flags}
	switch path {
	case "context init", "context update", "context use", "context list", "context current", "context delete":
		return s.invokeContexts(ctx, path, &values, args)
	case "add-ons list":
		return s.invokeAddOnCatalog(ctx, path, &values, args)
	case "secret set", "secret generate", "secret check", "secret list", "secret info", "secret show", "secret delete":
		return s.invokeSecrets(ctx, path, &values, args)
	case "secret encryption init", "secret encryption status", "secret encryption rotate":
		return s.invokeEncryption(ctx, path, &values, args)
	case "media add", "media list", "media delete":
		return s.invokeMedia(ctx, path, &values, args)
	case "validate", "render effective":
		return s.invokeDesiredState(ctx, path, &values, args)
	case "preflight controller", "setup", "controller rebind":
		return s.invokeController(ctx, path, &values, args)
	case "preflight infra", "preflight clusters", "preflight all":
		return commandResult{}, s.invokeEnvironmentPreflight(ctx, path, &values, args)
	case "cluster list", "cluster info", "cluster keyring":
		return s.invokeEnvironmentInspection(ctx, path, &values, args)
	case "cluster rsh", "cluster exec":
		return s.invokeEnvironmentAccess(ctx, path, &values, args)
	case "preflight container-cluster":
		return s.invokeContainerPreflight(ctx, path, &values, args)
	case "preflight storage-cluster":
		return s.invokeStoragePreflight(ctx, path, &values, args)
	case "preflight add-ons":
		return commandResult{}, s.invokeAddOnPreflight(ctx, path, &values, args)
	case "plan", "status", "diff", "apply", "destroy":
		return s.invokeLifecycle(ctx, path, &values, args)
	case "render":
		return commandResult{}, s.invokeArtifacts(ctx, path, &values, args)
	case "render installer":
		return commandResult{}, s.invokeInstaller(ctx, path, &values, args)
	case "render storage":
		return commandResult{}, s.invokeStorageArtifacts(ctx, path, &values, args)
	case "machine list", "machine info":
		return s.invokeMachineInventory(ctx, path, &values, args)
	case "machine rsh", "machine exec":
		return s.invokeMachineAccess(ctx, path, &values, args)
	case "machine start", "machine stop", "machine restart":
		return s.invokeMachinePower(ctx, path, &values, args)
	case "machine trust":
		return s.invokeMachineTrust(ctx, path, &values, args)
	case "cluster oc", "cluster kubectl", "cluster kubeconfig":
		return s.invokeClusterAccess(ctx, path, &values, args)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
