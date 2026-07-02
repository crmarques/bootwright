// Package become manages local sudo sessions and the BECOME password material
// shared between the bootwright parent process and its rootful child: sudo
// keepalive, the internal handoff environment variables, and the short-lived
// plaintext password file. Interactive prompting stays with the caller, which
// supplies password material through callbacks.
package become

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const localSudoFallbackKeepAliveInterval = time.Minute
const localSudoMinimumKeepAliveInterval = time.Second
const localSudoPasswordAttempts = 3

// SudoAuthEnv tells the rootful child how the parent authenticated sudo.
const SudoAuthEnv = "BOOTWRIGHT_INTERNAL_LOCAL_SUDO_AUTH"

// PasswordFileEnv hands the captured BECOME password file to the rootful child.
const PasswordFileEnv = "BOOTWRIGHT_INTERNAL_BECOME_PASSWORD_FILE"

const (
	AuthNonInteractive = "noninteractive"
	AuthPrompted       = "prompted"
)

// CommandFactory builds the sudo probe and keep-alive commands a Session runs.
// Callers outside internal/host pass DefaultCommandFactory so they never touch
// os/exec themselves.
type CommandFactory = func(context.Context, string, ...string) *exec.Cmd

// DefaultCommandFactory is the os/exec-backed CommandFactory.
func DefaultCommandFactory(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

type Session struct {
	password       string
	authMethod     string
	stderr         io.Writer
	keepAliveEvery time.Duration
	commandContext CommandFactory
}

func NewSession(ctx context.Context, readPassword func() (string, error), stderr io.Writer, commandContext CommandFactory) (*Session, error) {
	session := &Session{
		stderr:         stderr,
		commandContext: commandContext,
	}
	err := session.validateNonInteractive(ctx)
	if err == nil {
		session.authMethod = AuthNonInteractive
		session.keepAliveEvery = session.resolveKeepAliveInterval(ctx)
		return session, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("sudo is required for this command: %w", err)
	}
	for attempt := 1; attempt <= localSudoPasswordAttempts; attempt++ {
		password, err := readPassword()
		if err != nil {
			return nil, err
		}
		session.password = password
		if err := session.refreshWithPassword(ctx); err != nil {
			if attempt == localSudoPasswordAttempts {
				return nil, fmt.Errorf("sudo authentication failed after %d attempts: %w", localSudoPasswordAttempts, err)
			}
			continue
		}
		session.authMethod = AuthPrompted
		session.keepAliveEvery = session.resolveKeepAliveInterval(ctx)
		return session, nil
	}
	return nil, fmt.Errorf("sudo authentication failed after %d attempts", localSudoPasswordAttempts)
}

// SudoArgs prepends the non-interactive flag to argv for a sudo invocation. It
// carries no session state, so it is a package function rather than a method.
func SudoArgs(args ...string) []string {
	out := []string{"-n"}
	return append(out, args...)
}

func (s *Session) ChildEnv(includeBecomePassword bool) ([]string, func(), error) {
	env := []string{SudoAuthEnv + "=" + s.authMethod}
	if s.password == "" || !includeBecomePassword {
		return env, func() {}, nil
	}
	path, cleanup, err := WritePasswordFile(s.password)
	if err != nil {
		return nil, nil, err
	}
	env = append(env, PasswordFileEnv+"="+path)
	return env, cleanup, nil
}

func (s *Session) KeepAlive(ctx context.Context) func() {
	interval := s.keepAliveEvery
	if interval <= 0 {
		interval = localSudoFallbackKeepAliveInterval
	}
	if interval <= 0 {
		return func() {}
	}
	keepAliveCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.refreshKeepAlive(keepAliveCtx)
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

func (s *Session) validateNonInteractive(ctx context.Context) error {
	cmd := s.commandContext(ctx, "sudo", "-n", "-v")
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (s *Session) refreshKeepAlive(ctx context.Context) error {
	if s.password == "" {
		return s.validateNonInteractive(ctx)
	}
	return s.refreshWithPassword(ctx)
}

func (s *Session) refreshWithPassword(ctx context.Context) error {
	cmd := s.commandContext(ctx, "sudo", "-S", "-p", "", "-v")
	cmd.Stdin = strings.NewReader(s.password + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stderr.Len() > 0 && s.stderr != nil {
		_, _ = s.stderr.Write(filterLocalSudoValidationStderr(stderr.String()))
	}
	return err
}

func (s *Session) resolveKeepAliveInterval(ctx context.Context) time.Duration {
	timeout, ok := s.configuredTimestampTimeout(ctx)
	if !ok || timeout <= 0 {
		return localSudoFallbackKeepAliveInterval
	}
	interval := timeout * 3 / 4
	if interval < localSudoMinimumKeepAliveInterval {
		return localSudoMinimumKeepAliveInterval
	}
	return interval
}

func (s *Session) configuredTimestampTimeout(ctx context.Context) (time.Duration, bool) {
	cmd := s.commandContext(ctx, "sudo", "-V")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return 0, false
	}
	return parseSudoTimestampTimeout(output.String())
}

func parseSudoTimestampTimeout(output string) (time.Duration, bool) {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "timestamp") || !strings.Contains(lower, "timeout") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			value = line
		}
		fields := strings.Fields(strings.TrimSpace(value))
		if len(fields) == 0 {
			continue
		}
		amount, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		unit := "minutes"
		if len(fields) > 1 {
			unit = strings.ToLower(strings.TrimRight(fields[1], "s"))
		}
		switch unit {
		case "second", "sec":
			return time.Duration(amount * float64(time.Second)), true
		case "minute", "min":
			return time.Duration(amount * float64(time.Minute)), true
		case "hour", "hr":
			return time.Duration(amount * float64(time.Hour)), true
		default:
			return time.Duration(amount * float64(time.Minute)), true
		}
	}
	return 0, false
}

func filterLocalSudoValidationStderr(stderr string) []byte {
	var out strings.Builder
	for _, line := range strings.SplitAfter(stderr, "\n") {
		if strings.TrimRight(line, "\r\n") == "sudo: no password was provided" {
			continue
		}
		out.WriteString(line)
	}
	return []byte(out.String())
}
