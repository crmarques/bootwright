//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var _ lifecycle.ContextPaths = (*Store)(nil)

func (s *Store) Paths(ctx context.Context, name string) ([]lifecycle.ContextPath, []diagnostics.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !contextName(name) {
		return nil, nil, state("context paths require an explicit context name")
	}
	view, err := s.uninstallView()
	if err != nil {
		return nil, nil, err
	}
	roots := []lifecycle.ContextPath{
		{Kind: "store", Path: filepath.Join(view.Root, "contexts")},
		{Kind: "logs", Path: filepath.Join(view.Logs, logContexts)},
		{Kind: "hosted", Path: view.Hosted},
		{Kind: "libvirt-images", Path: view.Images},
	}
	paths := make([]lifecycle.ContextPath, 0, len(roots))
	var warnings []diagnostics.Diagnostic
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		root.Path = filepath.Join(root.Path, name)
		var stat syscall.Stat_t
		err := syscall.Lstat(root.Path, &stat)
		root.Present = err == nil
		if err != nil && !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ENOTDIR) {
			warnings = append(warnings, diagnostics.Diagnostic{Severity: "warning", Code: "context.state",
				Message:     "whether " + root.Path + " exists is unknown: " + err.Error(),
				Remediation: "check the permissions of the directories above " + root.Path + ", then repeat bootwright status --context " + name + " --paths"})
		}
		paths = append(paths, root)
	}
	return paths, warnings, nil
}
