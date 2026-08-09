package cli

import (
	"errors"

	"github.com/crmarques/bootwright/internal/converge"
)

type scopeDestroyOptions struct {
	use           string
	short         string
	long          string
	example       string
	stageSelector bool
	commandLabel  string
}

type scopeDestroyLabels struct {
	use          string
	short        string
	commandLabel string
}

func resolveScopeDestroyLabels(scope converge.Scope, options scopeDestroyOptions) scopeDestroyLabels {
	labels := scopeDestroyLabels{
		use:          "destroy",
		short:        "Destroy " + scope.Name + " runtime state",
		commandLabel: scope.Name + " destroy",
	}
	if options.use != "" {
		labels.use = options.use
	}
	if options.short != "" {
		labels.short = options.short
	}
	if options.commandLabel != "" {
		labels.commandLabel = options.commandLabel
	}
	return labels
}

func resolveScopeDestroyIntent(output string, dryRun bool, authorizeFlag []string) (*authorizations, error) {
	if err := validateOutputFormat(output); err != nil {
		return nil, failErr(2, err)
	}
	if output == outputJSON && !dryRun {
		return nil, failErr(2, mutatingJSONDryRunConflict(authorizeVerbDestroy))
	}
	auth, err := parseAuthorizations(authorizeFlag, authorizeVerbDestroy)
	if err != nil {
		return nil, failErr(2, err)
	}
	return auth, nil
}

func resolveDestroyCephRecovery(scope converge.Scope, value string) (map[string]string, error) {
	confirmed, err := converge.ParseDestroyCephOwnershipRecovery(value)
	if err != nil {
		return nil, failErr(2, err)
	}
	if len(confirmed) > 0 && !converge.ScopeTearsClusterLayer(scope) {
		return nil, failErr(2, errors.New("--recover-ceph-ownership runs only with the clusters stage or a full destroy"))
	}
	return confirmed, nil
}

func destroyUnownedAuthorizations(scope converge.Scope, auth *authorizations) (bool, bool) {
	if !converge.ScopeTearsMachineLayer(scope) {
		return false, false
	}
	return auth.allows(authorizeUnownedVMs), auth.allows(authorizeUnownedNetworks)
}

func resolveScopeDestroyRun(scope converge.Scope, commandLabel string, stageSelector bool, stage, clusters, machines string) (converge.Scope, string, runSelection, error) {
	selection := runSelection{stage: stage, clusters: clusters, machines: machines}
	if stageSelector {
		resolved, err := converge.DestroyStageScope(stage)
		if err != nil {
			return converge.Scope{}, "", selection, failErr(2, err)
		}
		scope = resolved
		commandLabel = converge.DestroyStageCommandLabel(stage, commandLabel)
	}
	if machines != "" {
		resolved, label, err := machineDestroyScope(clusters, stage)
		if err != nil {
			return converge.Scope{}, "", selection, err
		}
		scope, commandLabel = resolved, label
	}
	return scope, commandLabel, selection, nil
}
