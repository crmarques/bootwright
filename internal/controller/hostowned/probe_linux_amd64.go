//go:build linux && amd64

package hostowned

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

const (
	outputLimit  = 16 << 20
	stderrLimit  = 4 << 10
	probeTimeout = time.Minute
	maxEntries   = 1 << 16
)

func New(config Config) *Probe {
	return &Probe{config: config, run: execute, qualify: nativeexec.Qualify, sockets: socketBeneath, entries: directoryNames}
}

type bounded struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *bounded) Write(p []byte) (int, error) {
	room := b.limit - len(b.data)
	if len(p) > room {
		b.overflow = true
		p = p[:max(room, 0)]
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

var errOverflow = errors.New("probe output exceeds its bound")

func execute(ctx context.Context, path string, args []string) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	stdout, stderr := &bounded{limit: outputLimit}, &bounded{limit: stderrLimit}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin", "HOME=/root"}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	err := cmd.Run()
	if stdout.overflow {
		return result{}, errOverflow
	}
	answer := result{stdout: stdout.data, stderr: string(stderr.data)}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() {
		answer.code = exit.ExitCode()
		return answer, nil
	}
	return answer, err
}

func socketBeneath(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSocket != 0 {
			return true, nil
		}
		if !entry.IsDir() {
			continue
		}
		nested, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			return false, err
		}
		for _, inner := range nested {
			if inner.Type()&os.ModeSocket != 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func directoryNames(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	names, err := file.Readdirnames(maxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maxEntries {
		return nil, errOverflow
	}
	return names, nil
}
