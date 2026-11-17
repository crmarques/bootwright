package cli

import (
	"context"
	"strconv"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lifecycleInspectionCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "plan", results: []jsonResult{planPresentation{}}, render: renderPlan(), validate: validatePlanFlags, example: "--context lab --stage machines", invoke: Services.invokePlan, short: "Preview the next complete lifecycle operation", flags: []flagSpec{eraseAuthorizationFlag(), stageFlag(), untilFlag()}, long: "Preview the next legal full-context operation, or the exact continuation point of an incomplete one. It reads durable state and changes nothing. With --stage or --until it also shows which blocks that selection would start. --authorize erase=<machine>:metadataClear or erase=<machine>:retain, repeatable, previews a destroy that relaxes that Machine's declared removal erase, so its data-loss digest can be reviewed before destroy --yes."}),
		available(commandSpec{path: "status", results: []jsonResult{statusResult{}}, render: renderStatus(), example: "--context lab", invoke: Services.invokeStatus, short: "Show context readiness and lifecycle state", flags: []flagSpec{boolFlag("paths", "Also list where this host keeps the context's data and whether each path is present")}, long: "Report context readiness, desired state and lifecycle progress from durable records. While an operation runs it lists each running block's newest output file and the bytes written so far. With --paths it also lists the store, log, hosted and guest-image paths of the context on this host and whether each is present, examining no content. It performs no probe."}),
		logsCommandSpec(),
		available(commandSpec{path: "diff", results: []jsonResult{diffResult{}}, render: renderDiff(), validate: validateDiffFlags, refuse: (*Runner).diffToolRefusal, example: "--context lab --observe", invoke: Services.invokeDiff, short: "Compare desired state with what this context realized", flags: []flagSpec{boolFlag("observe", "Read each realized block's target through its read-only observation"), boolFlag("exit-code", "Exit 3 when any object differs, is unmanaged or is unknown"), directoryFlag("output-dir", "Write both sides as files into a directory that does not exist or is empty"), stringFlag("tool", "Show each listed file pair with a comparison program")}, long: "Compare the context's desired state with what its apply realized, object by object, file by file. Each side is shown as the input files that declare its objects, desired/<path> beside applied/<path>, or real/<path> with --observe. Without --observe it reads durable records only and contacts no host; with --observe it reads each realized block's target through that block's read-only observation. It exits 0 whenever it renders a comparison; with --exit-code it exits 3 instead when any object differs, is unmanaged or is unknown. --color selects terminal colors. --output-dir also writes both sides as trees of files, with notes.txt, so a directory comparison tool can show them side by side. --tool runs a program over each listed file pair, with the desired file first, once the result is written; it needs a terminal and text output and conflicts with --output-dir."}),
	}
}

func lifecycleMutationCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "apply", results: []jsonResult{operationPresentation{}}, render: renderOperation(), operation: true, validate: validateLifecycleFlags, example: "--context lab --stage machines", sshFlags: allSSHFlags, invoke: Services.invokeApply, short: "Apply the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), stageFlag(), untilFlag(), parallelismFlag(), confirmationFlag()}, long: "Realize the complete selected Environment, or continue the exact operation an interruption left behind. Missing disk inventory is prepared automatically before installation is planned; the resolved installation needs separate data-loss authorization. Each phase has a complete plan; --stage only gates which blocks this invocation starts, and the operation pauses at that boundary. Controller-only apply leaves inventory pending. After inventory preparation, --until <cluster>.<package> starts only that add-on instance and the blocks it requires, then pauses; a later apply continues the rest. --parallelism limits running steps (default 8); dependencies, exclusive resources and group throttles still apply. Review the plan before confirming; --yes skips ordinary confirmation."}),
		available(commandSpec{path: "destroy", results: []jsonResult{operationPresentation{}}, render: renderOperation(), operation: true, validate: validateDestroyFlags, example: "--context lab", sshFlags: allSSHFlags, invoke: Services.invokeDestroy, short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{destroyAuthorizationFlag(), unregisteredFlag(), parallelismFlag(), confirmationFlag()}, long: "Remove what this context's apply owns, whether that apply completed or not, proving each removal before it registers; a failed destroy is replaced by a fresh removal. Follow the plan's Stop first and Stop after online cleanup lists (bootwright machine stop): online cleanup, such as storage cleanup, a subscription unregistration or removing the VirtualMachines a host cluster of this context runs, needs its Machines running, and destroy pauses for their stop after it. A plan that deletes disks needs --authorize data-loss; with --yes or without a terminal it must be --authorize data-loss=<digest>, the digest bootwright plan prints for those steps. A rotated management controller or registry login credential is presented at its current version only with --authorize rebind=<secret>, which records a new binding version. A Machine whose declared removal erase you know to be more than you need is relaxed for this destroy only with --authorize erase=<machine>:metadataClear, which clears only metadata, or --authorize erase=<machine>:retain, which erases nothing and leaves its data and installed system in place; each Machine is named once, and the plan and its digest show the relaxation. A Machine whose subscription registration cannot be read and that you unregistered with the subscription service's own tools is named with --unregistered. --parallelism limits running steps (default 8); dependencies, exclusive resources and group throttles still apply. Review the plan before confirming; --yes skips ordinary confirmation."}),
	}
}

type LifecycleService interface {
	Plan(context.Context, lifecycle.PlanRequest) (*lifecycle.PlanResult, error)
	Status(context.Context, lifecycle.StatusRequest) (*lifecycle.StatusResult, error)
	Diff(context.Context, lifecycle.DiffRequest) (*lifecycle.DiffResult, error)
	Apply(context.Context, lifecycle.ApplyRequest) (*lifecycle.OperationResult, error)
	Destroy(context.Context, lifecycle.DestroyRequest) (*lifecycle.OperationResult, error)
}

func (s Services) invokePlan(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.PlanRequest{
		ContextName: values.text("context"),
		Stages:      values.names("stage"),
		Until:       values.text("until"),
		Relaxations: values.relaxations(),
	}, s.Lifecycle.Plan)
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

func (s Services) invokeApply(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.ApplyRequest{
		ContextName:      values.text("context"),
		Parallelism:      values.parallelism(),
		Stages:           values.names("stage"),
		Until:            values.text("until"),
		Authorizations:   values.authorizations(),
		SkipConfirmation: values.boolean("yes"),
		Unattended:       values.boolean("yes"),
		SSH:              values.ssh(),
	}, s.Lifecycle.Apply)
	return commandResult{lifecycleOperation: result}, err
}

func (s Services) invokeDestroy(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, lifecycle.DestroyRequest{
		ContextName:      values.text("context"),
		Parallelism:      values.parallelism(),
		Authorizations:   values.authorizations(),
		Relaxations:      values.relaxations(),
		Unregistered:     values.unregistered(),
		SkipConfirmation: values.boolean("yes"),
		Unattended:       values.boolean("yes"),
		SSH:              values.ssh(),
	}, s.Lifecycle.Destroy)
	return commandResult{lifecycleOperation: result}, err
}

func parallelismFlag() flagSpec {
	return flagSpec{name: "parallelism", help: "Set the maximum number of running steps (positive integer)", kind: "string", defaultValue: strconv.Itoa(lifecycle.MaxRunningBlocks)}
}

func (v *requestValues) parallelism() int {
	value, err := strconv.Atoi(v.text("parallelism"))
	v.recordFirstError(err)
	return value
}
