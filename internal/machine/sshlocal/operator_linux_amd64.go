package sshlocal

import (
	"context"
	"io"
	"os"
	"slices"
	"strconv"
	"syscall"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/nativeexec"
)

func operatorCommand(command nativeexec.Command, session machine.Session, pinned string, account nativeexec.Account, files []*os.File) nativeexec.Command {
	command.Path = nativeexec.SelfExecutable
	command.Args = append([]string{operatorMode}, clientArguments(session, paths{config: 3, knownHosts: 4}, pinned, strconv.Itoa)...)
	command.Credential = &syscall.Credential{Uid: account.UID, Gid: account.GID, Groups: account.Groups}
	command.ParentDeath = syscall.SIGKILL
	command.ExtraFiles = files
	return command
}

func (l Launcher) invokingCommand(ctx context.Context, command nativeexec.Command, session machine.Session, pinned string, files []*os.File) (nativeexec.Command, error) {
	if l.Account == nil {
		return nativeexec.Command{}, failure("the invoking SSH account is unavailable", "use --ssh-id-file with an explicit SSH user")
	}
	account, err := l.Account(ctx)
	if err != nil {
		return nativeexec.Command{}, failure("the invoking SSH account could not be verified", "use --ssh-id-file with an explicit SSH user")
	}
	if account.UID == 0 {
		return command, nil
	}
	return operatorCommand(command, session, pinned, account, files), nil
}

// ServeOperator holds the SSH policy and host key under the account that runs
// SSH. OpenSSH closes inherited descriptors on start, and cannot reopen a
// root parent's procfs descriptors after the credential change.
func ServeOperator(args []string) (bool, int) {
	if len(args) == 0 || args[0] != operatorMode {
		return false, 0
	}
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() || len(args) < 4 || args[1] != "-F" || args[2] != "3" {
		return true, 255
	}
	return true, runOperator(args[1:], os.NewFile(3, "SSH policy"), os.NewFile(4, "SSH host pin"), os.Stdin, os.Stdout, os.Stderr)
}

func runOperator(args []string, configSource, pinSource *os.File, in io.Reader, out, errOut io.Writer) int {
	defer configSource.Close()
	defer pinSource.Close()
	client, err := nativeexec.Qualify(Client)
	if err != nil {
		return 255
	}
	directory, err := os.MkdirTemp("/tmp", "bootwright-ssh-")
	if err != nil {
		return 255
	}
	defer os.RemoveAll(directory)
	config, err := operatorDescriptor(directory, configSource)
	if err != nil {
		return 255
	}
	defer config.Close()
	known, err := operatorDescriptor(directory, pinSource)
	if err != nil {
		return 255
	}
	defer known.Close()
	clientArgs := slices.Clone(args)
	clientArgs[1] = descriptorPath(int(config.Fd()))
	index := slices.Index(clientArgs, "UserKnownHostsFile=4")
	if index < 3 || clientArgs[index-1] != "-o" {
		return 255
	}
	clientArgs[index] = "UserKnownHostsFile=" + descriptorPath(int(known.Fd()))
	command := interactive(client, clientArgs, (Launcher{}).environment(), in, out, errOut)
	command.ParentDeath = syscall.SIGKILL
	status, err := exitStatus(nativeexec.Run(context.Background(), command))
	if err != nil {
		return 255
	}
	return status
}

func operatorDescriptor(directory string, source *os.File) (*os.File, error) {
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxObservedBytes {
		return nil, failure("the operator session received invalid SSH material", "")
	}
	data, err := io.ReadAll(io.LimitReader(source, maxObservedBytes+1))
	if err != nil || len(data) > maxObservedBytes {
		return nil, failure("the operator session could not read its SSH material", "")
	}
	return anonymous(directory, data)
}
