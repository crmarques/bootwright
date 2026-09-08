package cli

import (
	"context"
	"errors"

	"github.com/spf13/pflag"
)

var errMissingService = errors.New("application service is not configured")

func (s Services) invoke(ctx context.Context, path string, flags *pflag.FlagSet, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if flags == nil {
		return errors.New("command flags are not configured")
	}
	values := requestValues{flags: flags}
	switch path {
	case "context init", "context update", "context use", "context list", "context current", "context delete":
		return s.invokeContexts(ctx, path, &values, args)
	case "add-ons list", "add-ons add", "add-ons delete":
		return s.invokeAddOnCatalog(ctx, path, &values, args)
	case "secret set", "secret generate", "secret check", "secret list", "secret show", "secret delete":
		return s.invokeSecrets(ctx, path, &values, args)
	case "secret encryption init", "secret encryption status", "secret encryption rotate":
		return s.invokeEncryption(ctx, path, &values, args)
	case "media add", "media list", "media delete":
		return s.invokeMedia(ctx, path, &values, args)
	case "validate", "render effective":
		return s.invokeDesiredState(ctx, path, &values, args)
	case "preflight bastion", "bastion setup":
		return s.invokeController(ctx, path, &values, args)
	case "preflight infra", "preflight clusters", "preflight all", "cluster list", "cluster info", "cluster rsh", "cluster exec":
		return s.invokeEnvironment(ctx, path, &values, args)
	case "preflight container-cluster":
		return s.invokeContainerPreflight(ctx, path, &values, args)
	case "preflight storage-cluster":
		return s.invokeStoragePreflight(ctx, path, &values, args)
	case "preflight add-ons":
		return s.invokeAddOnPreflight(ctx, path, &values, args)
	case "plan", "status", "apply", "destroy":
		return s.invokeLifecycle(ctx, path, &values, args)
	case "render":
		return s.invokeArtifacts(ctx, path, &values, args)
	case "render installer":
		return s.invokeInstaller(ctx, path, &values, args)
	case "render storage":
		return s.invokeStorageArtifacts(ctx, path, &values, args)
	case "machine list":
		return s.invokeMachineInventory(ctx, path, &values, args)
	case "machine rsh", "machine exec":
		return s.invokeMachineAccess(ctx, path, &values, args)
	case "machine trust":
		return s.invokeMachineTrust(ctx, path, &values, args)
	case "cluster oc", "cluster kubectl", "cluster kubeconfig":
		return s.invokeClusterAccess(ctx, path, &values, args)
	default:
		return errors.New("command has no application dispatch")
	}
}
