package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/machine"
)

func (s Services) invokeMachineInventory(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.MachineInventory == nil {
		return errMissingService
	}
	switch path {
	case "machine list":
		return invokeRequest(ctx, values, machine.ListRequest{
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
		return invokeRequest(ctx, values, machine.RshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
		}, s.MachineAccess.Rsh)
	case "machine exec":
		return invokeRequest(ctx, values, machine.ExecRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.MachineAccess.Exec)
	default:
		return errors.New("command has no application dispatch")
	}
}
