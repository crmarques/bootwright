package cli

import (
	"errors"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type scopeApplyOptions struct {
	use           string
	short         string
	long          string
	example       string
	defaultPlan   bool
	hideDryRun    bool
	hideApproval  bool
	hideExecFlags bool
	stageSelector bool
	commandLabel  string
	action        string
}

type scopeApplyLabels struct {
	use          string
	short        string
	example      string
	commandLabel string
	action       string
}

var applyClusterAvailabilityChecker workflow.ClusterAvailabilityChecker

func resolveScopeApplyLabels(scope converge.Scope, options scopeApplyOptions) scopeApplyLabels {
	labels := scopeApplyLabels{
		use:          "apply",
		short:        "Apply " + scope.Name + " desired state",
		example:      options.example,
		commandLabel: scope.Name + " apply",
		action:       "apply",
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
	if options.action != "" {
		labels.action = options.action
	}
	return labels
}

func resolveScopeApplyIntent(output string, defaultPlan, dryRun bool, modeFlag string, authorizeFlag []string) (workflow.ApplyMode, *authorizations, error) {
	if err := validateOutputFormat(output); err != nil {
		return "", nil, failErr(2, err)
	}
	if output == outputJSON && !dryRun {
		return "", nil, failErr(2, mutatingJSONDryRunConflict(authorizeVerbApply))
	}
	if defaultPlan && !dryRun {
		return "", nil, failErr(2, errors.New("plan is always read-only"))
	}
	mode, err := parseApplyMode(modeFlag)
	if err != nil {
		return "", nil, failErr(2, err)
	}
	auth, err := parseAuthorizations(authorizeFlag, authorizeVerbApply)
	if err != nil {
		return "", nil, failErr(2, err)
	}
	return mode, auth, nil
}
