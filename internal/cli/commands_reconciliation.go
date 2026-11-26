package cli

import (
	"context"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lifecycleInspectionCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "status", results: []jsonResult{statusResult{}}, render: renderStatus(), example: "--context lab", invoke: Services.invokeStatus, short: "Show recorded lifecycle progress and next steps", flags: []flagSpec{boolFlag("paths", "Also list where this host keeps the context's data and whether each path is present")}, long: "Report lifecycle progress, recorded prerequisites and next steps from local evidence. While an operation runs it lists each running block's newest output file and the bytes written so far. With --paths it also lists the store, log, hosted and guest-image paths of the context on this host and whether each is present, examining no content. It performs no probe."}),
		logsCommandSpec(),
		available(commandSpec{path: "diff", results: []jsonResult{diffResult{}}, render: renderDiff(), validate: validateDiffFlags, refuse: (*Runner).diffToolRefusal, example: "--context lab --observe", invoke: Services.invokeDiff, short: "Compare desired state with what this context realized", flags: []flagSpec{boolFlag("observe", "Read each realized block's target through its read-only observation"), boolFlag("exit-code", "Exit 3 when any object differs, is unmanaged or is unknown"), directoryFlag("output-dir", "Write both sides as files into a directory that does not exist or is empty"), stringFlag("tool", "Show each listed file pair with a comparison program")}, long: "Compare the context's desired state with what its apply realized, object by object, file by file. Each side is shown as the input files that declare its objects, desired/<path> beside applied/<path>, or real/<path> with --observe. Without --observe it reads durable records only and contacts no host; with --observe it reads each realized block's target through that block's read-only observation. It exits 0 whenever it renders a comparison; with --exit-code it exits 3 instead when any object differs, is unmanaged or is unknown. --color selects terminal colors. --output-dir also writes both sides as trees of files, with notes.txt, so a directory comparison tool can show them side by side. --tool runs a program over each listed file pair, with the desired file first, once the result is written; it needs a terminal and text output and conflicts with --output-dir."}),
	}
}

func lifecycleMutationCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "apply", results: []jsonResult{operationPresentation{}, planPresentation{}}, render: renderLifecycle(), operation: true, classify: classifyRules{enrollmentInput: true}, validate: validateLifecycleFlags, example: "--context lab --stage machines", sshFlags: allSSHFlags, invoke: Services.invokeApply, short: "Apply the complete selected lifecycle unit", flags: []flagSpec{dryRunFlag(), authorizationFlag(), stageFlag(), applyTargetsFlag("clusters"), applyTargetsFlag("machines"), untilFlag(), parallelismFlag(), confirmationFlag()}, long: "Before starting a new apply, stop every managed Machine that already exists. Bootwright checks their power before changing state. An unfinished apply continues from its recorded work. Realize the complete selected Environment, or continue the exact operation an interruption left behind. Missing disk inventory is prepared automatically before installation is planned; the resolved installation needs separate data-loss authorization. Each phase has a complete plan; --stage only gates which blocks this invocation starts, and the operation pauses at that boundary. Controller-only apply leaves inventory pending. --clusters and --machines select named targets and their prerequisites for this invocation. After inventory preparation, --until <cluster>.<package> starts only that add-on instance and the blocks it requires, then pauses; a later apply continues the rest. --parallelism limits running steps (default 8); dependencies, exclusive resources and group throttles still apply. Use --dry-run to preview the selected work and check prerequisites without making changes. First-access credentials come from the Machine definition. Use machine enroll for a single-machine enrollment with an explicit key file. --yes skips ordinary confirmation during execution."}),
		available(commandSpec{path: "destroy", results: []jsonResult{operationPresentation{}, planPresentation{}}, render: renderLifecycle(), operation: true, classify: classifyRules{enrollmentInput: true}, validate: validateDestroyFlags, example: "--context lab", sshFlags: allSSHFlags, invoke: Services.invokeDestroy, short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{dryRunFlag(), floorFlag(), floorClustersFlag(), boolFlag("keep-going", "Continue independent cleanup after a target-local failure or a refused enrollment proof"), destroyAuthorizationFlag(), unregisteredFlag(), parallelismFlag(), confirmationFlag()}, long: "Remove what this context's apply owns, whether that apply completed or not, proving each removal before it registers; a failed destroy is replaced by a fresh removal. Stop all managed Machines with bootwright machine stop before starting destroy. A new destroy requires every target off or absent. --stage <floor> takes back that stage and every stage built on it, keeping the stages below; with --stage clusters or --stage add-ons, --clusters takes back only the named clusters' steps and what depends on them, keeping the other clusters. Once registered, destroy starts Machines for required guest cleanup and stops them again. Stop after online cleanup lists the Machines whose power is checked again. Repeating an unfinished destroy continues its work. Use --keep-going to continue independent steps after a recorded target-local failure and report the work that remains. Pair it with --dry-run to see current prerequisites and what previous failures would hold back. A plan that deletes disks needs --authorize data-loss; with --yes or without a terminal it must be --authorize data-loss=<digest>, the digest bootwright destroy --dry-run prints for those steps. A rotated management controller or registry login credential is presented at its current version only with --authorize rebind=<secret>, which records a new binding version. A Machine whose declared removal erase you know to be more than you need is relaxed for this destroy only with --authorize erase=<machine>:metadataClear, which clears only metadata, or --authorize erase=<machine>:retain, which erases nothing and leaves its data and installed system in place; each Machine is named once, and the plan and its digest show the relaxation. When you are sure a Machine's remaining effects can stay where they are, such as an enrollment whose host refuses its proof, --authorize abandon=<machine> leaves each of its steps that can be abandoned in place without contacting the host, lets the destroy complete, and needs a confirmation answered in a terminal; remove what it leaves by hand. A Machine whose subscription registration cannot be read and that you unregistered with the subscription service's own tools is named with --unregistered. --parallelism limits running steps (default 8); dependencies, exclusive resources and group throttles still apply. Use --dry-run to preview the selected work and check prerequisites without making changes. --yes skips ordinary confirmation during execution."}),
	}
}

type LifecycleService interface {
	Plan(context.Context, lifecycle.PlanRequest) (*lifecycle.PlanResult, error)
	Status(context.Context, lifecycle.StatusRequest) (*lifecycle.StatusResult, error)
	Diff(context.Context, lifecycle.DiffRequest) (*lifecycle.DiffResult, error)
	Apply(context.Context, lifecycle.ApplyRequest) (*lifecycle.OperationResult, error)
	Destroy(context.Context, lifecycle.DestroyRequest) (*lifecycle.OperationResult, error)
}

func (s Services) invokePlanApply(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	result, err := s.invokePlan(ctx, values, lifecycle.PlanRequest{
		Verb: "apply", ContextName: values.text("context"),
		Stages: values.names("stage"), Until: values.text("until"),
		Clusters: values.names("clusters"), Machines: values.names("machines"),
		Readiness: true, Parallelism: values.parallelism(),
	})
	plan := result.lifecyclePlan
	if err == nil && plan != nil && !plan.Settled && !plan.Finalizes && s.EnvironmentPreflight != nil {
		err = s.checkApplyPreview(ctx, values, plan)
	}
	return result, err
}

func (s Services) checkApplyPreview(ctx context.Context, values *requestValues, plan *lifecycle.PlanResult) error {
	var stages []string
	var clusters, deferredClusters []string
	for _, step := range plan.Steps {
		if step.State == "done" || (step.Selection == lifecycle.StepNotSelected || step.Selection == lifecycle.StepBlocked) || (len(plan.Stages) != 0 || plan.Until != "" || len(plan.Clusters) != 0 || len(plan.Machines) != 0) && step.Selection == "" {
			continue
		}
		stage := step.Stage
		if !slices.Contains(stages, step.Stage) {
			stages = append(stages, step.Stage)
		}
		if stage != "clusters" || !api.IsClusterKind(api.Kind(step.Kind)) {
			continue
		}
		waiting := slices.ContainsFunc(step.After, func(position int) bool {
			dependency := plan.Steps[position-1]
			return dependency.State != "done"
		})
		if waiting {
			deferredClusters = append(deferredClusters, step.Object)
		} else {
			clusters = append(clusters, step.Object)
		}
	}
	slices.Sort(clusters)
	clusters = slices.Compact(clusters)
	slices.Sort(deferredClusters)
	for _, name := range slices.Compact(deferredClusters) {
		if !slices.Contains(clusters, name) {
			plan.Checks = append(plan.Checks, lifecycle.PlanCheck{ID: "clusters", Status: "deferred", Summary: "Cluster " + name + " waits for prerequisites in the plan"})
		}
	}
	for _, stage := range []string{"machines", "add-ons"} {
		if slices.Contains(stages, stage) {
			plan.Checks = append(plan.Checks, lifecycle.PlanCheck{ID: stage, Status: "deferred", Summary: "Prerequisites are proved during " + stage + " execution"})
		}
	}
	stages = slices.DeleteFunc(stages, func(stage string) bool {
		switch stage {
		case "controller", "substrates":
			return false
		case "clusters":
			return len(clusters) == 0
		default:
			return true
		}
	})
	if len(stages) == 0 {
		return nil
	}
	report, err := s.EnvironmentPreflight.Preflight(ctx, preflight.PreflightRequest{
		ContextName: plan.Context.Name, Stages: stages, Clusters: clusters,
		ApplyPreview: true, ObserveHostKeys: true, SSH: values.ssh(),
	})
	if report == nil {
		return err
	}
	for _, row := range report.Checks {
		plan.Checks = append(plan.Checks, lifecycle.PlanCheck{ID: row.ID, Status: row.Status, Summary: row.Target + ": " + row.Reason, Remedy: row.Next})
	}
	if report.Summary.Failed+report.Summary.Unknown == 0 {
		return nil
	}
	// The shared report names a plain preflight. Repeat this preview with its
	// original stage and add-on selection after the caller resolves the blockers.
	repeat := "bootwright apply --dry-run --context " + plan.Context.Name
	if stages := values.names("stage"); len(stages) > 0 {
		repeat += " --stage " + strings.Join(stages, ",")
	}
	if until := values.text("until"); until != "" {
		repeat += " --until " + until
	}
	code := "preflight.unknown"
	if report.Summary.Failed != 0 {
		code = "preflight.failed"
	}
	return diagnostics.NewFailureWithRemediation(code, "apply prerequisites are unverified or blocked", "", "resolve the reported checks and repeat "+repeat)
}

func (s Services) invokePlanDestroy(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	return s.invokePlan(ctx, values, lifecycle.PlanRequest{
		Verb: "destroy", ContextName: values.text("context"),
		Relaxations: values.relaxations(), Unregistered: values.unregistered(), Readiness: true, Parallelism: values.parallelism(), KeepGoing: values.boolean("keep-going"),
		Floor: values.text("stage"), FloorClusters: values.names("clusters"),
	})
}

func (s Services) invokePlan(ctx context.Context, values *requestValues, request lifecycle.PlanRequest) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, request, s.Lifecycle.Plan)
	return commandResult{lifecyclePlan: result}, err
}

func (s Services) invokeStatus(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.StatusRequest{
		ContextName: values.text("context"),
		Paths:       values.optionalBoolean("paths"),
	}, s.Lifecycle.Status)
	return commandResult{lifecycleStatus: result}, err
}

func (s Services) invokeDiff(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.DiffRequest{
		ContextName: values.text("context"),
		Observe:     values.boolean("observe"),
	}, s.Lifecycle.Diff)
	return commandResult{lifecycleDiff: result}, err
}

func (s Services) invokeApply(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if values.boolean("dry-run") {
		if s.Lifecycle != nil && values.ssh().Borrowed() {
			return commandResult{}, lifecycle.BorrowedSSHRefusal()
		}
		return s.invokePlanApply(ctx, values, args)
	}
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.ApplyRequest{
		ContextName:      values.text("context"),
		Parallelism:      values.parallelism(),
		Stages:           values.names("stage"),
		Clusters:         values.names("clusters"),
		Machines:         values.names("machines"),
		Until:            values.text("until"),
		Authorizations:   values.authorizations(),
		SkipConfirmation: values.boolean("yes"),
		Unattended:       values.boolean("yes"),
		SSH:              values.ssh(),
	}, s.Lifecycle.Apply)
	return commandResult{lifecycleOperation: result}, err
}

func (s Services) invokeDestroy(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if values.boolean("dry-run") {
		if s.Lifecycle != nil && values.ssh().Borrowed() {
			return commandResult{}, lifecycle.BorrowedSSHRefusal()
		}
		return s.invokePlanDestroy(ctx, values, args)
	}
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.DestroyRequest{
		ContextName:      values.text("context"),
		Parallelism:      values.parallelism(),
		Authorizations:   values.authorizations(),
		Relaxations:      values.relaxations(),
		Unregistered:     values.unregistered(),
		Floor:            values.text("stage"),
		Clusters:         values.names("clusters"),
		KeepGoing:        values.boolean("keep-going"),
		SkipConfirmation: values.boolean("yes"),
		Unattended:       values.boolean("yes"),
		SSH:              values.ssh(),
	}, s.Lifecycle.Destroy)
	return commandResult{lifecycleOperation: result}, err
}

func applyTargetsFlag(name string) flagSpec {
	flag := stringFlag(name, "Select comma-separated "+name+" and their prerequisites")
	flag.nameList = true
	return flag
}

func parallelismFlag() flagSpec {
	return flagSpec{name: "parallelism", help: "Set the maximum number of running steps (positive integer)", kind: "string", defaultValue: strconv.Itoa(lifecycle.MaxRunningBlocks)}
}

func (v *requestValues) parallelism() int {
	value, err := strconv.Atoi(v.text("parallelism"))
	v.recordFirstError(err)
	return value
}
