package cli

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
const localRootSudoAuthEnv = "BOOTWRIGHT_INTERNAL_LOCAL_SUDO_AUTH"
const localRootBecomePasswordFileEnv = "BOOTWRIGHT_INTERNAL_BECOME_PASSWORD_FILE"

const (
	localSudoAuthNonInteractive = "noninteractive"
	localSudoAuthPrompted       = "prompted"
)

type localSudoSession struct {
	password       string
	authMethod     string
	stderr         io.Writer
	keepAliveEvery time.Duration
	commandContext func(context.Context, string, ...string) *exec.Cmd
}

func newLocalSudoSession(ctx context.Context, stdin io.Reader, stderr io.Writer, commandContext func(context.Context, string, ...string) *exec.Cmd) (*localSudoSession, error) {
	session := &localSudoSession{
		stderr:         stderr,
		commandContext: commandContext,
	}
	err := session.validateNonInteractive(ctx)
	if err == nil {
		session.authMethod = localSudoAuthNonInteractive
		session.keepAliveEvery = session.resolveKeepAliveInterval(ctx)
		return session, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("sudo is required for this command: %w", err)
	}
	for attempt := 1; attempt <= localSudoPasswordAttempts; attempt++ {
		password, err := readSudoPassword(stdin, stderr)
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
		session.authMethod = localSudoAuthPrompted
		session.keepAliveEvery = session.resolveKeepAliveInterval(ctx)
		return session, nil
	}
	return nil, fmt.Errorf("sudo authentication failed after %d attempts", localSudoPasswordAttempts)
}

func (s *localSudoSession) sudoArgs(args ...string) []string {
	out := []string{"-n"}
	return append(out, args...)
}

func (s *localSudoSession) childEnv(includeBecomePassword bool) ([]string, func(), error) {
	env := []string{localRootSudoAuthEnv + "=" + s.authMethod}
	if s.password == "" || !includeBecomePassword {
		return env, func() {}, nil
	}
	path, cleanup, err := writeBecomePasswordFile(s.password)
	if err != nil {
		return nil, nil, err
	}
	env = append(env, localRootBecomePasswordFileEnv+"="+path)
	return env, cleanup, nil
}

func (s *localSudoSession) keepAlive(ctx context.Context) func() {
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

func (s *localSudoSession) validateNonInteractive(ctx context.Context) error {
	cmd := s.commandContext(ctx, "sudo", "-n", "-v")
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (s *localSudoSession) refreshKeepAlive(ctx context.Context) error {
	if s.password == "" {
		return s.validateNonInteractive(ctx)
	}
	return s.refreshWithPassword(ctx)
}

func (s *localSudoSession) refreshWithPassword(ctx context.Context) error {
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

func (s *localSudoSession) resolveKeepAliveInterval(ctx context.Context) time.Duration {
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

func (s *localSudoSession) configuredTimestampTimeout(ctx context.Context) (time.Duration, bool) {
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
