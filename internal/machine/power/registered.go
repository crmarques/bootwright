package power

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// DriveRegistered uses the registered attempt’s runtime, logs and bound material.
func DriveRegistered(ctx context.Context, runner Runner, execution lifecycle.Execution, frozen Request, pin machine.HardwareIdentity, host HostIdentity) error {
	if runner == nil || lifecycle.ReadOnlyRun(execution) || execution.Operation == "" || execution.Block.ID == "" ||
		execution.Context != string(frozen.Identity.Context) || frozen.Identity.Object == "" ||
		frozen.Force || frozen.Verb != Start && frozen.Verb != Stop || frozen.Version != RequestVersion ||
		!slices.Contains(substrate.Realized(), frozen.Arm) || frozen.Controller.HostCluster != "" {
		return powerRequestInvalid.Fail("this attempt cannot drive the requested machine power transition", "")
	}
	physical := substrate.PhysicalController(frozen.Arm)
	if !pin.Present() || physical && strings.TrimSpace(host.UUID) == "" && len(host.MACs) == 0 {
		return powerIdentityChanged.Fail("the power transition lacks its original Machine identity or its controller host proof", "restore the original Machine proof and controller host access")
	}
	canonical, err := frozen.Canonical()
	if err != nil {
		return err
	}
	digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{
		Kind: "Machine", Object: frozen.Identity.Object, Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical,
	})
	if err != nil {
		return err
	}
	bounds, _ := substrate.MachineBoundsFor(frozen.Arm)
	request := lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: Operation, Variable: Variable, Canonical: canonical,
		Placement: frozen.Placement, Materials: driveMaterials(frozen), Values: pinValues(pin),
		Deadline: powerDeadline(frozen.Arm, frozen.Verb) + bounds.PreBoot,
	})
	request.Digest = digest
	if request.MaterialValues == nil {
		request.MaterialValues = map[string]string{}
	}
	request.Refusals = map[string]error{identityMismatch: identityRefusal(frozen, request.OutputRemediation)}
	physicalProofs(&request, target{frozen: frozen, physical: physical, pin: pin, host: host})
	request.MaterialValues["lifecyclePower"] = "true"
	if substrate.EmulatedBMC(frozen.Arm) {
		request.MaterialValues["proveControllerSystem"] = "true"
	}
	result, err := runner.Run(ctx, request)
	defer lifecycle.ClearProduced(result.Produced)
	if err != nil {
		return err
	}
	_, err = validate(result.Evidence, frozen, digest)
	return err
}
