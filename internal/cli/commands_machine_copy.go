package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
)

func machineCopyCommand() commandSpec {
	return available(commandSpec{
		path: "machine cp", results: []jsonResult{machineCopyPresentation{}}, render: renderMachineCopy(), validate: validateMachineNames, operandCheck: copyOperands, example: "--context lab --name node local:./config.yaml remote:config.yaml", sshFlags: accountSSHFlags, invoke: Services.invokeMachineCopy, short: "Copy files or directories to or from machines", operands: "<source> <destination>",
		long: "Copy one local:<path> to remote:<path>, or the reverse. Directories are copied recursively. " +
			"--name takes a comma-separated list; transfers run concurrently, at most eight at a time. " +
			"Several-machine downloads require a local directory and save each source beneath <directory>/<machine>/<source-basename>. " +
			"An existing destination directory receives the source basename. Existing files require --overwrite. " +
			"Paths are literal; symlinks and special files are refused. Python 3.9 or newer is required on both sides. " +
			"The resolved SSH account's permissions apply, without remote sudo. --ssh-user and --ssh-id-file select access as for machine exec. " +
			"Local files are read and written as the invoking user. " +
			"Bootwright flags go before the two copy operands.",
		flags: []flagSpec{machineNamesFlag(), boolFlag("overwrite", "Replace existing regular files atomically")},
	})
}

func copyOperands(args []string) (incomplete bool, problem string) {
	_, _, err := machine.ParseCopyPaths(args)
	if errors.Is(err, machine.ErrMissingCopyOperand) {
		return true, ""
	}
	return false, refusalText(err)
}

type MachineCopyService interface {
	Copy(context.Context, machineaccess.CopyRequest) (*machineaccess.CopyResult, error)
}

func (s Services) invokeMachineCopy(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if s.MachineCopy == nil {
		return commandResult{}, errMissingService
	}
	request := machineaccess.CopyRequest{ContextName: values.text("context"), Names: values.names("name"), SSH: values.sshAccount(), Overwrite: values.boolean("overwrite"), NonInteractive: values.text("output") != "text"}
	source, destination, err := machine.ParseCopyPaths(args)
	if err != nil {
		return commandResult{}, err
	}
	request.Source, request.Destination = source, destination
	result, err := invokeResult(ctx, values, request, s.MachineCopy.Copy)
	return commandResult{copy: result}, err
}
