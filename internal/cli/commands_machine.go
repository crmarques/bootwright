package cli

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

func machineCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{
			path: "machine list", short: "List machines and the lifecycle position each has reached",
			long: "List machines and the lifecycle position each has reached, as this context's own durable " +
				"evidence proves it. Add --power-status to also read each machine's power state from its management " +
				"controller, which is the only part of this command that contacts a host.",
			flags: []flagSpec{
				clustersFlag(), boolFlag("power-status", "Read each machine's power state from its management controller"),
				boolFlag("short", "Print only sorted machine names"),
			},
		}),
		available(commandSpec{
			path: "machine info", short: "Inspect machine placement, access, and recorded evidence",
			long:  "Show the selected machines' declared placement and access and the lifecycle evidence recorded by this context. --name takes a comma-separated list (default: all). This command reads local inventory without contacting a host or reading power.",
			flags: []flagSpec{stringFlag("name", "Select comma-separated machine names (default: all)")},
		}),
		available(sessionCommand(commandSpec{path: "machine rsh", short: "Open an SSH session", flags: []flagSpec{nameFlag()}})),
		available(machineExecCommand()),
		available(powerCommand("machine start", "Power machines on through their management controllers", false)),
		available(powerCommand("machine stop", "Shut machines down through their management controllers", true)),
		available(powerCommand("machine restart", "Restart machines through their management controllers", true)),
	}
}

func machineExecCommand() commandSpec {
	spec := sessionCommand(commandSpec{path: "machine exec", short: "Run a command over SSH", flags: []flagSpec{machineNamesFlag()}, payload: true})
	spec.long = strings.Replace(spec.long, "The exit status is the client's.", "The exit status is the client's. "+
		"--name takes a comma-separated list: the command runs on every listed machine concurrently, at most eight at a time, each output line is "+
		"prefixed by its machine, and the exit status is the highest any client returned.", 1)
	return spec
}

func machineNamesFlag() flagSpec {
	return requiredFlag("name", "Select comma-separated machine names")
}

// powerCommand describes one power verb. Only a verb that interrupts a running
// system takes a confirmation and a force arm; powering on interrupts nothing.
func powerCommand(path, short string, interrupts bool) commandSpec {
	flags := []flagSpec{machineNamesFlag()}
	if interrupts {
		flags = append(flags, boolFlag("force", "Cut power instead of asking the operating system to shut down"), confirmationFlag())
	}
	return commandSpec{
		path: path, short: short, flags: flags,
		long: short + ". --name takes a comma-separated list, and the command applies to every listed machine at once. Success reports the power state the controller proved, not the state that was requested.",
	}
}

type MachineInventoryService interface {
	List(context.Context, inventory.ListRequest) (*inventory.ListResult, error)
}

type MachineAccessService interface {
	Rsh(context.Context, machineaccess.RshRequest) (*machine.SessionResult, error)
	Exec(context.Context, machineaccess.ExecRequest) (*machine.SessionResult, error)
}

type MachinePowerService interface {
	Start(context.Context, power.PowerRequest) (*power.Results, error)
	Stop(context.Context, power.PowerRequest) (*power.Results, error)
	Restart(context.Context, power.PowerRequest) (*power.Results, error)
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
			Power:       values.boolean("power-status"),
		}, s.MachineInventory.List)
		return commandResult{machines: result}, err
	case "machine info":
		names := selectedInfoNames(values)
		result, err := invokeResult(ctx, values, inventory.ListRequest{ContextName: values.text("context")}, s.MachineInventory.List)
		if err != nil {
			return commandResult{}, err
		}
		if result != nil {
			if err := refuseUnknownInfo("Machine", result.Context, names, machineInfoNames(result)); err != nil {
				return commandResult{}, err
			}
		}
		return commandResult{machines: selectMachineInfo(result, names)}, nil
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
		return commandResult{session: result}, err
	case "machine exec":
		result, err := invokeResult(ctx, values, machineaccess.ExecRequest{
			ContextName: values.text("context"),
			Names:       values.names("name"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.MachineAccess.Exec)
		return commandResult{session: result}, err
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
		Names:            values.names("name"),
		Force:            values.optionalBoolean("force"),
		SkipConfirmation: values.optionalBoolean("yes"),
	}
	var invoke func(context.Context, power.PowerRequest) (*power.Results, error)
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
