package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
)

type RebindRequest struct {
	SkipConfirmation bool
	MachineID        string
	ProductUUID      string
	FilesystemUUID   string
	RecordedRetired  bool
}

type RebindResult struct {
	Outcome      string
	Bindings     int
	Reservations int
}

const rebindCommand = "bootwright controller rebind"

func (s Service) Rebind(ctx context.Context, request RebindRequest) (*RebindResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rebinder, ok := s.storage.(ControllerRebinder)
	if s.storage == nil || s.host == nil || !ok {
		return nil, failure("controller.identity", "controller rebind is not configured", "")
	}
	current, err := s.host.Identity(ctx)
	if err != nil {
		return nil, err
	}
	named := request.MachineID != "" || request.ProductUUID != "" || request.FilesystemUUID != ""
	if request.SkipConfirmation && !request.RecordedRetired {
		return nil, failure("controller.identity", "--yes rebinds only with --recorded-host-retired affirming the recorded host no longer runs this store",
			"run "+rebindCommand+" interactively to review both identities, or affirm --recorded-host-retired with --yes")
	}
	if request.SkipConfirmation && !named || named && (request.MachineID != current.MachineID() ||
		request.ProductUUID != current.ProductUUID() || request.FilesystemUUID != current.FilesystemUUID()) {
		return nil, failure("controller.identity", "--yes rebinds only to the host identity it names, and the named identity must be this host's",
			"run "+rebindCommand+" interactively to review both identities, or name this host's --machine-id, --product-uuid and --root-uuid with --yes")
	}
	var recorded controller.InstalledHostIdentity
	err = s.storage.ReadController(ctx, "", func(view StorageView) error {
		if !view.Exists || !view.Initialized {
			return failure("controller.identity", "this store holds no controller record to rebind",
				"the first bootwright apply on this host records it; nothing needs rebinding")
		}
		recorded = view.State.Host
		return nil
	})
	if err != nil {
		return nil, err
	}
	if recorded.Equal(current) {
		return nil, failure("controller.identity", "the controller record already names this host; nothing changed", "")
	}
	if !request.SkipConfirmation {
		if s.options.Confirmer == nil {
			return nil, failure("controller.identity", "controller rebind requires an interactive terminal, or --yes with --recorded-host-retired and the new identity named", "")
		}
		if err := s.options.Confirmer.Confirm(ctx, "controller rebind", identityText(recorded)+" to "+identityText(current)); err != nil {
			return nil, err
		}
	}
	result, err := rebinder.RebindController(ctx, recorded, current)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func identityText(identity controller.InstalledHostIdentity) string {
	return "machine-id " + identity.MachineID() + ", DMI product UUID " + identity.ProductUUID() + ", root filesystem UUID " + identity.FilesystemUUID()
}
