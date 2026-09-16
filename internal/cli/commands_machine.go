package cli

import (
	"context"
	"errors"
	"slices"

	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

func machineCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "machine list", short: "List machines and ownership-backed state", flags: []flagSpec{clustersFlag(), boolFlag("silent", "Print only sorted machine names"), outputFlag()}}),
		available(accessCommand(commandSpec{path: "machine rsh", short: "Print an SSH access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}}, "")),
		available(accessCommand(commandSpec{path: "machine exec", short: "Print a command access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true}, "")),
		available(powerCommand("machine start", "Power one machine on through its management controller", false)),
		available(powerCommand("machine stop", "Shut one machine down through its management controller", true)),
		available(powerCommand("machine restart", "Restart one machine through its management controller", true)),
	}
}

// powerCommand describes one power verb. Only a verb that interrupts a running
// system takes a confirmation and a force arm; powering on interrupts nothing.
func powerCommand(path, short string, interrupts bool) commandSpec {
	flags := []flagSpec{nameFlag()}
	if interrupts {
		flags = append(flags, boolFlag("force", "Cut power instead of asking the operating system to shut down"), confirmationFlag())
	}
	return commandSpec{
		path: path, short: short, flags: append(flags, outputFlag()),
		long: short + ". Success reports the power state the controller proved, not the state that was requested.",
	}
}

type MachineInventoryService interface {
	List(context.Context, inventory.ListRequest) (*inventory.ListResult, error)
}

type MachineAccessService interface {
	Rsh(context.Context, machineaccess.RshRequest) (*machineaccess.Descriptor, error)
	Exec(context.Context, machineaccess.ExecRequest) (*machineaccess.Descriptor, error)
}

type MachinePowerService interface {
	Start(context.Context, power.PowerRequest) (*power.Result, error)
	Stop(context.Context, power.PowerRequest) (*power.Result, error)
	Restart(context.Context, power.PowerRequest) (*power.Result, error)
}

func (s Services) invokeMachineInventory(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.MachineInventory == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "machine list":
		result, err := invokeResult(ctx, values, inventory.ListRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
			Silent:      values.boolean("silent"),
		}, s.MachineInventory.List)
		return commandResult{machines: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}

func (s Services) invokeMachineAccess(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.MachineAccess == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "machine rsh":
		result, err := invokeResult(ctx, values, machineaccess.RshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
		}, s.MachineAccess.Rsh)
		return commandResult{descriptor: result}, err
	case "machine exec":
		result, err := invokeResult(ctx, values, machineaccess.ExecRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.MachineAccess.Exec)
		return commandResult{descriptor: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}

func (s Services) invokeMachinePower(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	request := power.PowerRequest{
		ContextName:      values.text("context"),
		Name:             values.text("name"),
		Force:            values.optionalBoolean("force"),
		SkipConfirmation: values.optionalBoolean("yes"),
	}
	var invoke func(context.Context, power.PowerRequest) (*power.Result, error)
	switch path {
	case "machine start":
		invoke = s.MachinePower.Start
	case "machine stop":
		invoke = s.MachinePower.Stop
	case "machine restart":
		invoke = s.MachinePower.Restart
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	result, err := invokeResult(ctx, values, request, invoke)
	return commandResult{power: result}, err
}
