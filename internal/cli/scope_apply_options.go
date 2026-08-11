package cli

import (
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type scopeApplyOptions struct {
	use           string
	short         string
	long          string
	example       string
	defaultPlan   bool
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

func resolveScopeApplyRunScope(scope converge.Scope, options scopeApplyOptions, stage, through, action, commandLabel, machinesScope, clusterScope string) (converge.Scope, string, error) {
	runScope := scope
	runCommandLabel := commandLabel
	if options.stageSelector {
		var err error
		switch {
		case stage != "" && through != "":
			runScope, err = converge.ApplyRangeScope(stage, through)
			runCommandLabel = converge.ApplyRangeCommandLabel(stage, through, action, commandLabel)
		case through != "":
			runScope, err = converge.ApplyThroughScope(through)
			runCommandLabel = converge.ApplyThroughCommandLabel(through, action, commandLabel)
		default:
			runScope, err = converge.ApplyStageScope(stage)
			runCommandLabel = converge.ApplyStageCommandLabel(stage, action, commandLabel)
		}
		if err != nil {
			return converge.Scope{}, "", failErr(2, err)
		}
	}
	if machinesScope == "" {
		return runScope, runCommandLabel, nil
	}
	stageProvided := stage != "" || through != ""
	var err error
	runScope, err = machineApplyRunScope(machinesScope, clusterScope, stageProvided, runScope)
	if err != nil {
		return converge.Scope{}, "", err
	}
	if !stageProvided {
		runCommandLabel = "machines " + action
	}
	return runScope, runCommandLabel, nil
}

func resolveScopeApplyIntent(output string, dryRun bool, modeFlag string, authorizeFlag []string) (workflow.ApplyMode, *authorizations, error) {
	if err := validateOutputFormat(output); err != nil {
		return "", nil, failErr(2, err)
	}
	if output == outputJSON && !dryRun {
		return "", nil, failErr(2, mutatingJSONDryRunConflict(authorizeVerbApply))
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
