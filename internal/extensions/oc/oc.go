package oc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type OCRunner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}

type CommandRunner struct {
	Command string
	LogPath string
	Stdout  io.Writer
	Stderr  io.Writer
}

func (r CommandRunner) Run(ctx context.Context, kubeconfig string, args []string, input []byte) ([]byte, error) {
	name := strings.TrimSpace(r.Command)
	if name == "" {
		name = "oc"
	}
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("required command %q is unavailable on PATH: %w", name, err)
	}
	command := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.CommandContext(ctx, name, command...)
	if len(input) > 0 {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, r.Stdout)
	cmd.Stderr = io.MultiWriter(&stderr, r.Stderr)
	err := cmd.Run()
	out := append(stdout.Bytes(), stderr.Bytes()...)
	if r.LogPath != "" {
		_ = appendLog(r.LogPath, name, command, input, out)
	}
	if err != nil {
		return out, fmt.Errorf("run %s %s: %w: %s", name, shellJoin(command), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func appendLog(path, name string, args []string, input []byte, output []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "$ %s %s\n", name, shellJoin(args))
	if len(input) > 0 {
		_, _ = file.Write(input)
		if len(input) == 0 || input[len(input)-1] != '\n' {
			_, _ = file.Write([]byte("\n"))
		}
	}
	if len(output) > 0 {
		_, _ = file.Write(output)
		if output[len(output)-1] != '\n' {
			_, _ = file.Write([]byte("\n"))
		}
	}
	return nil
}

func shellJoin(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			parts = append(parts, "''")
			continue
		}
		if strings.ContainsAny(arg, " \t\n'\"$`\\") {
			parts = append(parts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

func WaitInterval(timeout time.Duration) time.Duration {
	if timeout < time.Second {
		return timeout
	}
	if timeout < 10*time.Second {
		return time.Second
	}
	return 5 * time.Second
}
