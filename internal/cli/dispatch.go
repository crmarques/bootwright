package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/pflag"
)

var errMissingService = errors.New("application service is not configured")

// commandResult is the closed set of successful application results understood
// by this executable. Unavailable error-only ports return its zero value.
type commandResult struct {
	validation *compilation.Report
	admission  *contexts.AdmissionResult
	use        *contexts.UseResult
	list       *contexts.ListResult
	current    *contexts.CurrentResult
	deletion   *contexts.DeleteResult
	effective  *compilation.EffectiveResult
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
	case "add-ons list", "add-ons add", "add-ons delete":
		return commandResult{}, s.invokeAddOnCatalog(ctx, path, &values, args)
	case "secret set", "secret generate", "secret check", "secret list", "secret show", "secret delete":
		return commandResult{}, s.invokeSecrets(ctx, path, &values, args)
	case "secret encryption init", "secret encryption status", "secret encryption rotate":
		return commandResult{}, s.invokeEncryption(ctx, path, &values, args)
	case "media add", "media list", "media delete":
		return commandResult{}, s.invokeMedia(ctx, path, &values, args)
	case "validate", "render effective":
		return s.invokeDesiredState(ctx, path, &values, args)
	case "preflight bastion", "bastion setup":
		return commandResult{}, s.invokeController(ctx, path, &values, args)
	case "preflight infra", "preflight clusters", "preflight all":
		return commandResult{}, s.invokeEnvironmentPreflight(ctx, path, &values, args)
	case "cluster list", "cluster info":
		return commandResult{}, s.invokeEnvironmentInspection(ctx, path, &values, args)
	case "cluster rsh", "cluster exec":
		return commandResult{}, s.invokeEnvironmentAccess(ctx, path, &values, args)
	case "preflight container-cluster":
		return commandResult{}, s.invokeContainerPreflight(ctx, path, &values, args)
	case "preflight storage-cluster":
		return commandResult{}, s.invokeStoragePreflight(ctx, path, &values, args)
	case "preflight add-ons":
		return commandResult{}, s.invokeAddOnPreflight(ctx, path, &values, args)
	case "plan", "status", "apply", "destroy":
		return commandResult{}, s.invokeLifecycle(ctx, path, &values, args)
	case "render":
		return commandResult{}, s.invokeArtifacts(ctx, path, &values, args)
	case "render installer":
		return commandResult{}, s.invokeInstaller(ctx, path, &values, args)
	case "render storage":
		return commandResult{}, s.invokeStorageArtifacts(ctx, path, &values, args)
	case "machine list":
		return commandResult{}, s.invokeMachineInventory(ctx, path, &values, args)
	case "machine rsh", "machine exec":
		return commandResult{}, s.invokeMachineAccess(ctx, path, &values, args)
	case "machine trust":
		return commandResult{}, s.invokeMachineTrust(ctx, path, &values, args)
	case "cluster oc", "cluster kubectl", "cluster kubeconfig":
		return commandResult{}, s.invokeClusterAccess(ctx, path, &values, args)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
