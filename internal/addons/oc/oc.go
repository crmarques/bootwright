package oc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

type OCRunner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}

type CommandRunner struct {
	Command   string
	LogPath   string
	RedactLog bool
	Stdout    io.Writer
	Stderr    io.Writer
	Runner    execution.Runner
}

func (r CommandRunner) Run(ctx context.Context, kubeconfig string, args []string, input []byte) ([]byte, error) {
	name := strings.TrimSpace(r.Command)
	if name == "" {
		name = "oc"
	}
	if _, err := execution.LookPath(name); err != nil {
		return nil, fmt.Errorf("required command %q is unavailable on PATH: %w", name, err)
	}
	command := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := execution.Command{Name: name, Args: command}
	if len(input) > 0 {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = tee(&stdout, r.Stdout)
	cmd.Stderr = tee(&stderr, r.Stderr)
	err := r.runner().Run(ctx, cmd)
	out := append(stdout.Bytes(), stderr.Bytes()...)
	var logErr error
	if r.LogPath != "" {
		loggedInput, loggedOutput := input, out
		if r.RedactLog {
			loggedInput = redactedLogBytes(input)
			loggedOutput = redactedLogBytes(out)
		}
		logErr = appendLog(r.LogPath, name, command, loggedInput, loggedOutput)
	}
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if r.RedactLog {
			detail = strings.TrimSpace(stderr.String())
		}
		if logErr != nil {
			return out, fmt.Errorf("run %s %s: %w: %s (also failed to append oc log %s: %v)", name, shellquote.Quote(command), err, detail, r.LogPath, logErr)
		}
		return out, fmt.Errorf("run %s %s: %w: %s", name, shellquote.Quote(command), err, detail)
	}
	if logErr != nil {
		return stdout.Bytes(), fmt.Errorf("append oc log %s: %w", r.LogPath, logErr)
	}
	return stdout.Bytes(), nil
}

func (r CommandRunner) runner() execution.Runner {
	if r.Runner != nil {
		return r.Runner
	}
	return execution.OSRunner{}
}

func tee(buf *bytes.Buffer, w io.Writer) io.Writer {
	if w == nil {
		return buf
	}
	return io.MultiWriter(buf, w)
}

func redactedLogBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return []byte(fmt.Sprintf("[redacted %d bytes]", len(data)))
}

var appendLogMu sync.Mutex

func appendLog(path, name string, args []string, input []byte, output []byte) error {
	appendLogMu.Lock()
	defer appendLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "$ %s %s\n", name, shellquote.Quote(args)); err != nil {
		_ = file.Close()
		return err
	}
	if len(input) > 0 {
		if _, err := file.Write(input); err != nil {
			_ = file.Close()
			return err
		}
		if len(input) == 0 || input[len(input)-1] != '\n' {
			if _, err := file.Write([]byte("\n")); err != nil {
				_ = file.Close()
				return err
			}
		}
	}
	if len(output) > 0 {
		if _, err := file.Write(output); err != nil {
			_ = file.Close()
			return err
		}
		if output[len(output)-1] != '\n' {
			if _, err := file.Write([]byte("\n")); err != nil {
				_ = file.Close()
				return err
			}
		}
	}
	return file.Close()
}

const (
	waitIntervalDivisor = 100
	minWaitInterval     = 5 * time.Second
	maxWaitInterval     = 15 * time.Second
)

func WaitInterval(timeout time.Duration) time.Duration {
	if timeout < time.Second {
		return timeout
	}
	if timeout < 10*time.Second {
		return time.Second
	}
	interval := timeout / waitIntervalDivisor
	if interval < minWaitInterval {
		return minWaitInterval
	}
	if interval > maxWaitInterval {
		return maxWaitInterval
	}
	return interval
}
