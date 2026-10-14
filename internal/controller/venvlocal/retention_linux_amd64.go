//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const (
	requirementsName = "requirements.lock"
	hashOption       = "--hash=sha256:"
)

func (a *Adapter) RuntimeWheels(ctx context.Context, digest string) ([]string, error) {
	directory, err := a.runtimeDirectory(digest)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, requirementsName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errors.Join(ctx.Err(), prerequisites.ErrRuntimeUnavailable)
	}
	if err != nil {
		return nil, failure("the controller runtime " + digest[:12] + " has no readable " + requirementsName)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecord+1))
	if err != nil || len(data) > maxRecord {
		return nil, failure("the controller runtime " + digest[:12] + " has no readable " + requirementsName)
	}
	wheels := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		_, hash, found := strings.Cut(line, hashOption)
		if !found || !digestPattern.MatchString(strings.TrimSpace(hash)) {
			return nil, failure("the controller runtime " + digest[:12] + " has a " + requirementsName + " this build did not write")
		}
		wheels = append(wheels, strings.TrimSpace(hash))
	}
	return wheels, ctx.Err()
}
