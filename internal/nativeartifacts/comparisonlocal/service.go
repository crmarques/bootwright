package comparisonlocal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

const (
	terminationGrace = 5 * time.Second
	toolSearch       = "/usr/local/bin:/usr/bin:/bin"
	scratchPrefix    = "bootwright-diff-"
)

type Trees interface {
	Resolve(string) (string, error)
	PublishTree(ctx context.Context, directory string, files []rendering.TreeFile) (string, error)
}

type Account struct {
	UID, GID int
	Groups   []uint32
	Home     string
}

type Service struct {
	Trees              Trees
	Account            func(context.Context) (Account, error)
	In                 io.Reader
	Out, Err           io.Writer
	Terminal, Language string
	Scratch            string

	effective func() int
	helper    func(context.Context, Account, []byte) ([]byte, error)
	start     func(*exec.Cmd) (bool, error)
}

type identity struct {
	account  Account
	delegate bool
}

func (s Service) identity(ctx context.Context) (identity, error) {
	if s.Account == nil {
		return identity{}, refuse("the invoking account is not configured", "report this; the comparison files need the invoking account")
	}
	account, err := s.Account(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return identity{}, ctx.Err()
		}
		return identity{}, refuse("the invoking account cannot be verified", "run bootwright from a local account the account database names")
	}
	account.Groups = append([]uint32(nil), account.Groups...)
	effective := os.Geteuid
	if s.effective != nil {
		effective = s.effective
	}
	switch euid := effective(); {
	case euid == account.UID:
		return identity{account: account}, nil
	case euid == 0 && account.UID > 0 && account.GID >= 0:
		return identity{account: account, delegate: true}, nil
	}
	return identity{}, refuse("the invoking account's identity is unavailable", "run bootwright as the account that names the comparison files")
}

func (s Service) WriteTrees(ctx context.Context, directory string, files []rendering.TreeFile) (string, error) {
	who, err := s.identity(ctx)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil || strings.ContainsRune(directory, 0) {
		return "", refuse("the path "+directory+" cannot be resolved", "pass a path this account can reach")
	}
	return s.perform(ctx, who, helperRequest{Action: actionPublish, Directory: absolute, Files: files})
}

func (s Service) RunTool(ctx context.Context, program string, files []rendering.TreeFile, pairs []rendering.ComparisonPair) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	who, err := s.identity(ctx)
	if err != nil {
		return err
	}
	if who.account.UID == 0 {
		return diagnostics.NewFailureWithRemediation("runtime.tool", "the comparison program "+program+" is not run, because the invoking account is root", "", "write the trees with --output-dir and run the program on them")
	}
	executable, err := lookTool(program, filepath.SplitList(toolSearch))
	if err != nil {
		return toolFailure(program, err)
	}
	scratch, err := s.perform(ctx, who, helperRequest{Action: actionScratch, Directory: s.Scratch, Files: files})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = s.perform(context.WithoutCancel(ctx), who, helperRequest{Action: actionRemove, Directory: scratch})
	}()
	environment := toolEnvironment(s.Terminal, s.Language, who.account.Home)
	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return err
		}
		command := toolCommand(ctx, who, executable, scratch, filepath.Join(scratch, pair.Desired), filepath.Join(scratch, pair.Other), environment)
		command.Stdin, command.Stdout, command.Stderr = s.In, s.Out, s.Err
		if err := s.run(ctx, command, program); err != nil {
			return err
		}
	}
	return nil
}

func toolCommand(ctx context.Context, who identity, executable, directory, desired, other string, environment []string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, desired, other)
	command.Env = append([]string(nil), environment...)
	command.Dir = directory
	var credential *syscall.Credential
	if who.delegate {
		credential = accountCredential(who.account)
	}
	command.SysProcAttr = processAttributes(credential)
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = terminationGrace
	return command
}

func accountCredential(account Account) *syscall.Credential {
	return &syscall.Credential{Uid: uint32(account.UID), Gid: uint32(account.GID), Groups: append([]uint32(nil), account.Groups...)}
}

func toolEnvironment(terminal, language, home string) []string {
	var environment []string
	if terminal != "" {
		environment = append(environment, "TERM="+terminal)
	}
	if language != "" {
		environment = append(environment, "LANG="+language)
	}
	return append(environment, "HOME="+home, "PATH="+toolSearch)
}

func lookTool(program string, search []string) (string, error) {
	switch {
	case program == "" || strings.ContainsRune(program, 0):
		return "", exec.ErrNotFound
	case strings.ContainsRune(program, '/'):
		return filepath.Abs(program)
	}
	for _, directory := range search {
		candidate := filepath.Join(directory, program)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func (s Service) run(ctx context.Context, command *exec.Cmd, program string) error {
	start := supervise
	if s.start != nil {
		start = s.start
	}
	started, err := start(command)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case started:
		return nil
	}
	return toolFailure(program, err)
}

func supervise(command *exec.Cmd) (bool, error) {
	type outcome struct {
		started bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := command.Start(); err != nil {
			done <- outcome{err: err}
			return
		}
		done <- outcome{started: true, err: command.Wait()}
	}()
	result := <-done
	return result.started, result.err
}

func toolFailure(program string, err error) error {
	reason := "cannot be started"
	if errors.Is(err, exec.ErrNotFound) {
		reason = "was not found on " + toolSearch
	}
	return diagnostics.NewFailureWithRemediation("runtime.tool", "the comparison program "+program+" "+reason, "", "pass --tool a program on "+toolSearch+" or a path to an executable this account can run")
}

func refuse(message, remedy string) error {
	return diagnostics.NewFailureWithRemediation("render.publish", message, "", remedy)
}
