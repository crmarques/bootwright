package cli

import (
	"context"
	"errors"
	"slices"

	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
)

func machineCommands() []commandSpec {
	return []commandSpec{
		{path: "machine list", short: "List machines and ownership-backed state", flags: []flagSpec{clustersFlag(), boolFlag("silent", "Print only sorted machine names"), outputFlag()}},
		accessCommand(commandSpec{path: "machine rsh", short: "Print an SSH access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}}, ""),
		accessCommand(commandSpec{path: "machine exec", short: "Print a command access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true}, ""),
	}
}

type MachineInventoryService interface {
	List(context.Context, inventory.ListRequest) error
}

type MachineAccessService interface {
	Rsh(context.Context, machineaccess.RshRequest) error
	Exec(context.Context, machineaccess.ExecRequest) error
}

func (s Services) invokeMachineInventory(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.MachineInventory == nil {
		return errMissingService
	}
	switch path {
	case "machine list":
		return invokeRequest(ctx, values, inventory.ListRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
			Silent:      values.boolean("silent"),
		}, s.MachineInventory.List)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeMachineAccess(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.MachineAccess == nil {
		return errMissingService
	}
	switch path {
	case "machine rsh":
		return invokeRequest(ctx, values, machineaccess.RshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
		}, s.MachineAccess.Rsh)
	case "machine exec":
		return invokeRequest(ctx, values, machineaccess.ExecRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.MachineAccess.Exec)
	default:
		return errors.New("command has no application dispatch")
	}
}
