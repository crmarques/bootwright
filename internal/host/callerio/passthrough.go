package callerio

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func RunPassthrough(ctx context.Context, name string, args, extraEnv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	credential, elevated, err := callerCredential()
	if err != nil {
		return 0, err
	}
	resolved, baseEnv, err := resolvePassthroughCommand(name, elevated)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, resolved, args...)
	cmd.Env = mergeEnv(baseEnv, extraEnv)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if elevated {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	if err := cmd.Run(); err != nil {
		if code, ok := passthroughExitCode(err); ok {
			return code, nil
		}
		return 0, err
	}
	return 0, nil
}

func resolvePassthroughCommand(name string, elevated bool) (string, []string, error) {
	if elevated {
		resolved := name
		if !strings.ContainsRune(name, os.PathSeparator) {
			path, found, err := LookPath(name)
			if err != nil {
				return "", nil, err
			}
			if found {
				resolved = path
			}
		}
		return resolved, callerEnv(), nil
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", nil, err
	}
	return resolved, os.Environ(), nil
}

func mergeEnv(base, extra []string) []string {
	out := base
	for _, entry := range extra {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		out = withEnv(out, key, value)
	}
	return out
}

func passthroughExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	if status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	if code := exitErr.ExitCode(); code >= 0 {
		return code, true
	}
	return 1, true
}
