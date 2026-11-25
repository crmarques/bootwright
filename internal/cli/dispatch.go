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
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/managedos/media"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/pflag"
)

var errMissingService = errors.New("application service is not configured")

// commandResult is the closed set of successful application results understood
// by this executable. Unavailable error-only ports return its zero value.
type commandResult struct {
	policySet         *policySet
	policyShow        *policyShown
	policyDocument    []byte
	policyDelete      *policyDeletion
	policyPermissions *policyPermissions
	addOns            *addoncatalog.ListResult
	addOnInfo         *addoncatalog.DescribeResult
	validation        *compilation.Report
	admission         *contexts.AdmissionResult
	use               *contexts.UseResult
	list              *contexts.ListResult
	current           *contexts.CurrentResult
	deletion          *contexts.DeleteResult
	effective         *compilation.EffectiveResult
	rendered          *artifactrendering.Result
	controller        *prerequisites.Report
	rebind            *prerequisites.RebindResult
	uninstall         *prerequisites.UninstallResult

	lifecyclePlan      *lifecycle.PlanResult
	lifecycleStatus    *lifecycle.StatusResult
	lifecycleDiff      *lifecycle.DiffResult
	lifecycleOperation *lifecycle.OperationResult
	machineDiscovery   *diskdiscovery.DiscoverResult
	diskInventories    *diskdiscovery.InventoryResult

	logs       *logview.Result
	logsFollow *logview.Summary

	mediaMutation *media.MutationResult
	mediaList     *media.ListResult

	machines    *inventory.ListResult
	session     *machine.SessionResult
	copy        *machineaccess.CopyResult
	trust       *enrollment.Report
	power       *power.Results
	mediaStatus *power.MediaStatusResult

	secretMutation     *custody.MutationResult
	secretCheck        *custody.CheckResult
	secretList         *custody.ListResult
	secretReveal       *custody.RevealResult
	encryptionMutation *encryption.MutationResult
	encryptionStatus   *encryption.StatusResult

	handoff    *containeraccess.Handoff
	kubeconfig *containeraccess.KubeconfigResult
	keyring    *inspection.KeyringResult

	clusterList      *inspection.ListResult
	clusterInfo      *inspection.InfoResult
	adminCredentials *lifecycle.UpdateAdminCredentialsResult
	signOn           *lifecycle.SignOnResult
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
	spec := specFor(path)
	if spec.invoke == nil {
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return spec.invoke(s, ctx, &values, args)
}
