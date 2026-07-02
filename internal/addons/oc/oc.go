package oc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

type OCRunner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}

type CommandRunner struct {
	Command string
	LogPath string
	Stdout  io.Writer
	Stderr  io.Writer
	// Runner substitutes the local process launcher in tests; nil runs on the OS.
	Runner execution.Runner
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
	// tee keeps the captured buffer (used for the log and the failure-path error)
	// while streaming to the caller's writer only when one is set. A quiet runner
	// (nil writers) suppresses live console output — readiness polls use one so
	// expected NotFound/"no matches for kind" lines don't reach the terminal —
	// without losing the log file or error diagnostics.
	cmd.Stdout = tee(&stdout, r.Stdout)
	cmd.Stderr = tee(&stderr, r.Stderr)
	err := r.runner().Run(ctx, cmd)
	out := append(stdout.Bytes(), stderr.Bytes()...)
	var logErr error
	if r.LogPath != "" {
		logErr = appendLog(r.LogPath, name, command, input, out)
	}
	if err != nil {
		if logErr != nil {
			return out, fmt.Errorf("run %s %s: %w: %s (also failed to append oc log %s: %v)", name, shellquote.Quote(command), err, strings.TrimSpace(string(out)), r.LogPath, logErr)
		}
		return out, fmt.Errorf("run %s %s: %w: %s", name, shellquote.Quote(command), err, strings.TrimSpace(string(out)))
	}
	if logErr != nil {
		return stdout.Bytes(), fmt.Errorf("append oc log %s: %w", r.LogPath, logErr)
	}
	// oc routinely writes deprecation/TLS/auth warnings to stderr on a successful
	// `get -o json`; returning the combined buffer would corrupt JSON that callers
	// unmarshal, so the success path yields stdout only. stderr stays in the log
	// and in the failure-path error above.
	return stdout.Bytes(), nil
}

func (r CommandRunner) runner() execution.Runner {
	if r.Runner != nil {
		return r.Runner
	}
	return execution.OSRunner{}
}

// tee returns a writer that always captures into buf and additionally streams
// to w when w is non-nil. It avoids io.MultiWriter's panic on a nil writer so a
// quiet runner can leave Stdout/Stderr unset.
func tee(buf *bytes.Buffer, w io.Writer) io.Writer {
	if w == nil {
		return buf
	}
	return io.MultiWriter(buf, w)
}

func appendLog(path, name string, args []string, input []byte, output []byte) error {
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

func WaitInterval(timeout time.Duration) time.Duration {
	if timeout < time.Second {
		return timeout
	}
	if timeout < 10*time.Second {
		return time.Second
	}
	return 5 * time.Second
}
