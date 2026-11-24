package cli

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

func machineCommands() []commandSpec {
	discoveryStage := stageFlag()
	discoveryStage.enum = []string{"controller"}
	discoveryStage.help = "Select controller and infrastructure prerequisites for discovery"
	return []commandSpec{
		machineEnrollCommand(),
		available(commandSpec{path: "machine discover", results: []jsonResult{planPresentation{}, machineDiscoveryPresentation{}}, render: renderMachineDiscovery(), operation: true, validate: validateMachineNames, example: "--context lab --name node --stage controller --dry-run", invoke: Services.invokeMachineDiscovery, short: "Inventory physical root disks through a temporary RAM boot", long: "Record the disk identities of the selected powered-off physical machines: from the BMC drive inventory when it is complete, otherwise by booting inventory-only RHEL media and powering off. --name takes a comma-separated list. --dry-run previews without effects. --stage controller also plans controller and infrastructure setup; review the complete plan before confirmation. Discovery never installs an OS or creates RAID volumes.", flags: []flagSpec{machineNamesFlag(), boolFlag("dry-run", "Preview the discovery plan without booting a machine"), discoveryStage, confirmationFlag()}}),
		available(commandSpec{
			path: "machine inventory", results: []jsonResult{diskInventoriesPresentation{}}, render: renderDiskInventory(),
			invoke: Services.invokeDiskInventory, validate: validateMachineNames, flags: []flagSpec{machineNamesFlag()},
			short: "Inspect recorded disk inventory", example: "--context lab --name node",
			long: "Show recorded disk identities, exact capacities, and the source and operation that collected them. The command reads the context's current apply lineage from local records.",
		}),
		available(commandSpec{
			path: "machine list", results: []jsonResult{machineListPresentation{}}, render: renderMachineList(), validate: validateMachineList, example: "--context lab --power-status", invoke: Services.invokeMachineList, short: "List machines and the lifecycle position each has reached",
			long: "List machines and the lifecycle position each has reached, as this context's own durable " +
				"evidence proves it. Add --power-status to also read each machine's power state from its management " +
				"controller, which is the only part of this command that contacts a host.",
			flags: []flagSpec{
				blankClustersFlag(), boolFlag("power-status", "Read each machine's power state from its management controller"),
				boolFlag("short", "Print only sorted machine names"),
			},
		}),
		available(commandSpec{
			path: "machine info", results: []jsonResult{machineInfoPresentation{}}, render: renderMachineInfo(), validate: validateInfoNames("machine"), example: "--context lab --name node", invoke: Services.invokeMachineInfo, short: "Inspect machine placement, access, and recorded evidence",
			long:  "Show the selected machines' declared placement and access and the lifecycle evidence recorded by this context. --name takes a comma-separated list (default: all). This command reads local inventory without contacting a host or reading power.",
			flags: []flagSpec{stringFlag("name", "Select comma-separated machine names (default: all)")},
		}),
		available(sessionCommand(commandSpec{path: "machine rsh", validate: validateMachineName, example: "--context lab --name node", sshFlags: accountSSHFlags, invoke: Services.invokeMachineRsh, short: "Open an SSH session", flags: []flagSpec{nameFlag()}})),
		available(machineExecCommand()),
		machineCopyCommand(),
		available(powerCommand("machine start", "Power machines on through their management controllers", false, Services.invokeMachineStart)),
		available(powerCommand("machine stop", "Shut machines down through their management controllers", true, Services.invokeMachineStop)),
		available(powerCommand("machine restart", "Restart machines through their management controllers", true, Services.invokeMachineRestart)),
		available(ejectMediaCommand()),
		available(commandSpec{
			path: "machine media status", short: "Read the virtual media status of machines", example: "--context lab --name node",
			flags: []flagSpec{machineNamesFlag()}, invoke: Services.invokeMachineMediaStatus, validate: validateMachineNames,
			render: renderMachineMediaStatus(), results: []jsonResult{machineMediaStatusPresentation{}},
			long: "Read the virtual-media device Bootwright uses on each named machine. --name takes a comma-separated list. " +
				"Each machine reports present, none, or unknown. The command reads live state without changing it.",
		}),
	}
}

func ejectMediaCommand() commandSpec {
	return commandSpec{
		path: "machine media eject", short: "Eject whatever virtual media machines present", example: "--context lab --name node --yes",
		flags: []flagSpec{machineNamesFlag(), confirmationFlag()}, invoke: Services.invokeMachineEjectMedia,
		validate: firstRefusal(validateMachineNames, validateEjectConfirmation), render: renderMachinePower(), results: []jsonResult{machinePowerPresentation{}},
		long: "Eject whatever image the virtual-media device Bootwright inserts through presents on each machine, whichever image it is and whoever attached it, " +
			"and leave its power as it is. --name takes a comma-separated list, and the command applies to every listed machine at once. " +
			"Success reports that the controller was read back presenting no media, not that an eject was requested. " +
			"A machine still running from the ejected image may fail; review the machines before confirming; --yes skips ordinary confirmation.",
	}
}

func machineExecCommand() commandSpec {
	spec := sessionCommand(commandSpec{path: "machine exec", validate: validateMachineNames, example: "--context lab --name node -- uname -a", sshFlags: accountSSHFlags, invoke: Services.invokeMachineExec, short: "Run a command over SSH", flags: []flagSpec{machineNamesFlag()}, payload: true})
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
func powerCommand(path, short string, interrupts bool, invoke commandInvoker) commandSpec {
	flags := []flagSpec{machineNamesFlag()}
	validate := validateMachineNames
	if interrupts {
		flags = append(flags, boolFlag("force", "Cut power instead of asking the operating system to shut down"), confirmationFlag())
		validate = firstRefusal(validateMachineNames, validatePowerConfirmation)
	}
	return commandSpec{
		path: path, short: short, flags: flags, example: "--context lab --name node", invoke: invoke, validate: validate, render: renderMachinePower(), results: []jsonResult{machinePowerPresentation{}},
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
	EjectMedia(context.Context, power.PowerRequest) (*power.Results, error)
	MediaStatus(context.Context, power.MediaStatusRequest) (*power.MediaStatusResult, error)
}

func (s Services) invokeMachineList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineInventory == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, inventory.ListRequest{
		ContextName: values.text("context"),
		Clusters:    values.names("clusters"),
		Power:       values.boolean("power-status"),
	}, s.MachineInventory.List)
	return commandResult{machines: result}, err
}

func (s Services) invokeMachineInfo(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineInventory == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, inventory.ListRequest{ContextName: values.text("context"), Names: selectedInfoNames(values), InspectAccess: true}, s.MachineInventory.List)
	return commandResult{machines: result}, err
}

func (s Services) invokeMachineRsh(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineAccess == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, machineaccess.RshRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		SSH:         values.sshSession(),
	}, s.MachineAccess.Rsh)
	return commandResult{session: result}, err
}

func (s Services) invokeMachineExec(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if s.MachineAccess == nil {
		return commandResult{}, errMissingService
	}
	command, err := remoteCommand(values.flags, args)
	if err != nil {
		return commandResult{}, err
	}
	result, err := invokeResult(ctx, values, machineaccess.ExecRequest{
		ContextName: values.text("context"),
		Names:       values.names("name"),
		SSH:         values.sshSession(),
		Command:     command,
	}, s.MachineAccess.Exec)
	return commandResult{session: result}, err
}

func (s Services) powerRequest(values *requestValues) power.PowerRequest {
	return power.PowerRequest{
		ContextName:      values.text("context"),
		Names:            values.names("name"),
		Force:            values.optionalBoolean("force"),
		SkipConfirmation: values.optionalBoolean("yes"),
	}
}

func (s Services) invokeMachineStart(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, s.powerRequest(values), s.MachinePower.Start)
	return commandResult{power: result}, err
}

func (s Services) invokeMachineStop(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, s.powerRequest(values), s.MachinePower.Stop)
	return commandResult{power: result}, err
}

func (s Services) invokeMachineRestart(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, s.powerRequest(values), s.MachinePower.Restart)
	return commandResult{power: result}, err
}

func (s Services) invokeMachineEjectMedia(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, s.powerRequest(values), s.MachinePower.EjectMedia)
	return commandResult{power: result}, err
}

func (s Services) invokeMachineMediaStatus(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachinePower == nil {
		return commandResult{}, errMissingService
	}
	request := power.MediaStatusRequest{ContextName: values.text("context"), Names: values.names("name")}
	result, err := invokeResult(ctx, values, request, s.MachinePower.MediaStatus)
	return commandResult{mediaStatus: result}, err
}
