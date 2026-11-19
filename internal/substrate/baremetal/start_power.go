package baremetal

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (c MachineCapability) StartPowerSecrets(block reconciliation.Block) ([]string, error) {
	return c.SecretReferences(block)
}

// StartPower identifies the physical machine from its complete Redfish inventory
// even when this context has never provisioned it. A previous pin, when
// available, also has to identify the same hardware.
func (c MachineCapability) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	execution := probe.Execution()
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Quiescence{}, err
	}
	unproved := lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "the management controller did not prove the physical machine's identity and power"}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil || ValidatePresence(result.Evidence, request, execution.Block.RequestDigest) != nil {
		return unproved, ctx.Err()
	}
	observed, _ := decodeEvidence(result.Evidence, execution.Block.RequestDigest)
	if len(execution.PreviousEvidence) != 0 {
		if ValidatePresence(execution.PreviousEvidence, request, execution.Block.RequestDigest) != nil {
			return unproved, nil
		}
		pin, _ := ProvedIdentity(execution.PreviousEvidence)
		now, _ := ProvedIdentity(result.Evidence)
		if pin.UUID != "" && !strings.EqualFold(pin.UUID, now.UUID) || pin.Serial != "" && pin.Serial != now.Serial ||
			pin.RootVolume.Valid() && pin.RootVolume != now.RootVolume {
			return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: "the physical machine differs from its recorded hardware identity"}, nil
		}
	}
	switch observed.Power {
	case "Off":
		return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the management controller reports the machine off"}, nil
	case "On":
		return lifecycle.Quiescence{State: lifecycle.Live, Reason: "the management controller reports the machine on",
			Stop: "bootwright machine stop --context " + execution.Context + " --name " + request.Identity.Object}, nil
	default:
		return unproved, nil
	}
}
