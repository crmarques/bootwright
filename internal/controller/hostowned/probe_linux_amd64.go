//go:build linux && amd64

package hostowned

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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

var errOverflow = errors.New("probe output exceeds its bound")

func execute(ctx context.Context, path string, args []string) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	stdout, stderr := nativeexec.Head(outputLimit), nativeexec.Head(stderrLimit)
	ran := nativeexec.Run(ctx, nativeexec.Command{
		Path: path, Args: args, Env: []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin", "HOME=/root"},
		Stdout: stdout, Stderr: stderr, WaitDelay: 10 * time.Second,
	})
	if stdout.Overflowed() {
		return result{}, errOverflow
	}
	answer := result{stdout: stdout.Bytes(), stderr: stderr.String(), code: ran.ExitCode}
	if ran.Outcome == nativeexec.Exited && ran.Err == nil {
		return answer, nil
	}
	answer.code = 0
	return answer, ran.Err
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
