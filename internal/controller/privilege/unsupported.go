//go:build !linux || !amd64

package privilege

import "context"

func (Resolver) Resolve(context.Context) (Account, error) { return Account{}, errAccount }

func QualifiedSudo() (string, error) { return "", errAccount }

type ProcessExecutor struct{}

func (ProcessExecutor) Run(context.Context, Command) (int, error) { return 1, errAccount }

func Executable() (string, error) { return "", errAccount }

func ReexecutionPath() (string, error) { return "", errAccount }

func GuardParent(int) (func(), error) { return nil, errAccount }

func Begin(ctx context.Context) (context.Context, func()) { return context.WithCancel(ctx) }

func ExitCode(_ context.Context, fallback int) int { return fallback }
