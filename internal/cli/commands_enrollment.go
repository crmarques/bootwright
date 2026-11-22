package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/account"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type MachineEnrollmentService interface {
	Prepare(context.Context, lifecycle.PrepareRequest) (*lifecycle.OperationResult, error)
	PlanPreparation(context.Context, lifecycle.PrepareRequest) (*lifecycle.PlanResult, error)
}

func machineEnrollCommand() commandSpec {
	return available(commandSpec{
		path: "machine enroll", operation: true, classify: classifyRules{enrollmentInput: true}, render: renderLifecycle(),
		results: []jsonResult{operationPresentation{}, planPresentation{}},
		example: "--context lab --name node --dry-run", invoke: Services.invokeMachineEnroll,
		validate: validateMachineName, short: "Establish the declared lifecycle SSH account on one provided Machine",
		long:  "Claim one provided Machine, create its declared lifecycle login through first access, and prove a new connection and passwordless sudo. Ordinary apply uses the same enrollment. An explicit key file is used only for this Machine's operator-identity bootstrap. The preview opens no enrollment connection and asks for no bootstrap credential.",
		flags: []flagSpec{nameFlag(), dryRunFlag(), confirmationFlag()}, sshFlags: []string{"ssh-id-file"},
	})
}

func (s Services) invokeMachineEnroll(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineEnrollment == nil {
		return commandResult{}, errMissingService
	}
	name := values.text("name")
	request := lifecycle.PrepareRequest{ContextName: values.text("context"), Kind: machine.EnrollmentFamily,
		Targets: []string{name}, SkipConfirmation: values.boolean("yes")}
	if values.boolean("dry-run") {
		result, err := invokeResult(ctx, values, request, s.MachineEnrollment.PlanPreparation)
		return commandResult{lifecyclePlan: result}, err
	}
	ctx = account.WithKeyFile(ctx, name, values.text("ssh-id-file"))
	result, err := invokeResult(ctx, values, request, s.MachineEnrollment.Prepare)
	return commandResult{lifecycleOperation: result}, err
}
