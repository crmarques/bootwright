package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const localSudoKeepAliveInterval = time.Minute

type localSudoSession struct {
	password       string
	stderr         io.Writer
	commandContext func(context.Context, string, ...string) *exec.Cmd
}

func newLocalSudoSession(ctx context.Context, stdin io.Reader, stderr io.Writer, commandContext func(context.Context, string, ...string) *exec.Cmd) (*localSudoSession, error) {
	session := &localSudoSession{
		stderr:         stderr,
		commandContext: commandContext,
	}
	err := session.validateNonInteractive(ctx)
	if err == nil {
		return session, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("sudo is required for this command: %w", err)
	}
	password, err := readSudoPassword(stdin, stderr)
	if err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("SUDO password cannot be empty")
	}
	session.password = password
	if err := session.refresh(ctx); err != nil {
		return nil, fmt.Errorf("validate sudo credentials: %w", err)
	}
	return session, nil
}

func (s *localSudoSession) sudoArgs(args ...string) []string {
	out := []string{"-n"}
	return append(out, args...)
}

func (s *localSudoSession) keepAlive(ctx context.Context) func() {
	if s.password == "" {
		return func() {}
	}
	keepAliveCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(localSudoKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.refresh(keepAliveCtx)
			case <-keepAliveCtx.Done():
				return
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *localSudoSession) validateNonInteractive(ctx context.Context) error {
	cmd := s.commandContext(ctx, "sudo", "-n", "-v")
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (s *localSudoSession) refresh(ctx context.Context) error {
	cmd := s.commandContext(ctx, "sudo", "-S", "-p", "", "-v")
	cmd.Stdin = strings.NewReader(s.password + "\n")
	cmd.Stderr = s.stderr
	return cmd.Run()
}
