package nativeexec

import (
	"context"
	"syscall"
	"time"
)

const SelfExecutable = "/proc/self/exe"

type Account struct {
	UID, GID uint32
	Groups   []uint32
}

func Helper(mode string, account Account) Command {
	return Command{
		Path: SelfExecutable, Args: []string{mode}, Env: []string{"LANG=C", "LC_ALL=C"}, Dir: "/",
		Credential:  &syscall.Credential{Uid: account.UID, Gid: account.GID, Groups: append([]uint32(nil), account.Groups...)},
		ParentDeath: syscall.SIGKILL, WaitDelay: time.Second,
	}
}

type Launcher struct {
	Runner  func(context.Context, Command) Result
	Starter func(context.Context, Command) (*Process, error)
}

func (l Launcher) Run(ctx context.Context, command Command) Result {
	if l.Runner != nil {
		return l.Runner(ctx, command)
	}
	return Run(ctx, command)
}

func (l Launcher) Start(ctx context.Context, command Command) (*Process, error) {
	if l.Starter != nil {
		return l.Starter(ctx, command)
	}
	return Start(ctx, command)
}
