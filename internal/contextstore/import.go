package contextstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/managedroot"
)

func ImportInputs(paths []string, inputDir string, replace bool) ([]string, error) {
	prepared, err := PrepareInputImport(paths, inputDir, replace)
	if err != nil {
		return nil, err
	}
	defer prepared.Cancel()
	return prepared.Commit()
}

type PreparedContextImport struct {
	finalCtx  Context
	stagedCtx Context
	inputs    *PreparedInputImport
	committed bool
}

func PrepareContextImport(name string, paths []string) (*PreparedContextImport, error) {
	finalCtx, err := NewContext(name)
	if err != nil {
		return nil, err
	}
	if err := ensureContextParent(finalCtx); err != nil {
		return nil, err
	}
	if err := rejectInputSourcesInside(paths, finalCtx.InputDir); err != nil {
		return nil, err
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(finalCtx.BaseDir), "."+name+"-context-")
	if err != nil {
		return nil, fmt.Errorf("create temporary context under %s: %w", filepath.Dir(finalCtx.BaseDir), err)
	}
	stagedCtx, err := NewStagingContext(name, stagingDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	if err := EnsureDirs(stagedCtx); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	preparedInputs, err := PrepareInputImport(paths, stagedCtx.InputDir, false)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	return &PreparedContextImport{
		finalCtx:  finalCtx,
		stagedCtx: stagedCtx,
		inputs:    preparedInputs,
	}, nil
}

func (p *PreparedContextImport) FinalContext() Context {
	return p.finalCtx
}

func (p *PreparedContextImport) ValidationPaths() []string {
	return p.inputs.ValidationPaths()
}

func (p *PreparedContextImport) Commit(replace bool) ([]string, error) {
	if p.committed {
		return nil, errors.New("context import already committed")
	}
	if _, err := p.inputs.Commit(); err != nil {
		return nil, err
	}
	var backupDir string
	if _, err := os.Stat(p.finalCtx.BaseDir); err == nil {
		if !replace {
			return nil, fmt.Errorf("context %q already exists; rerun with --yes to replace it", p.finalCtx.Name)
		}
		info, err := os.Stat(p.finalCtx.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p.finalCtx.BaseDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s exists and is not a directory", p.finalCtx.BaseDir)
		}
		createdBackup, err := os.MkdirTemp(filepath.Dir(p.finalCtx.BaseDir), "."+filepath.Base(p.finalCtx.BaseDir)+"-backup-")
		if err != nil {
			return nil, fmt.Errorf("create temporary context backup under %s: %w", filepath.Dir(p.finalCtx.BaseDir), err)
		}
		if err := os.Remove(createdBackup); err != nil {
			return nil, fmt.Errorf("prepare temporary context backup %s: %w", createdBackup, err)
		}
		if err := os.Rename(p.finalCtx.BaseDir, createdBackup); err != nil {
			return nil, fmt.Errorf("backup %s: %w", p.finalCtx.BaseDir, err)
		}
		backupDir = createdBackup
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", p.finalCtx.BaseDir, err)
	}
	if err := os.Rename(p.stagedCtx.BaseDir, p.finalCtx.BaseDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, p.finalCtx.BaseDir)
		}
		return nil, fmt.Errorf("replace %s: %w", p.finalCtx.BaseDir, err)
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return nil, fmt.Errorf("remove temporary context backup %s: %w", backupDir, err)
		}
	}
	p.committed = true
	return rewriteImportedPaths(p.inputs.ImportedPaths(), p.stagedCtx.InputDir, p.finalCtx.InputDir), nil
}

func (p *PreparedContextImport) Cancel() {
	if p == nil || p.committed {
		return
	}
	p.inputs.Cancel()
	_ = os.RemoveAll(p.stagedCtx.BaseDir)
}

type PreparedInputImport struct {
	inputDir   string
	stagingDir string
	paths      []string
	committed  bool
}

func ensureContextParent(ctx Context) error {
	root, err := cleanPath(rootDir)
	if err != nil {
		return err
	}
	if _, err := managedroot.Ensure(root, 0o700); err != nil {
		return err
	}
	contextsDir := filepath.Dir(ctx.BaseDir)
	if err := os.MkdirAll(contextsDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", contextsDir, err)
	}
	if err := os.Chmod(contextsDir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", contextsDir, err)
	}
	return nil
}

func PrepareInputImport(paths []string, inputDir string, replace bool) (*PreparedInputImport, error) {
	inputDir, err := cleanPath(inputDir)
	if err != nil {
		return nil, err
	}
	if err := rejectInputSourcesInside(paths, inputDir); err != nil {
		return nil, err
	}
	files, err := discoverInputFiles(paths)
	if err != nil {
		return nil, err
	}
	if err := rejectInputFilesInside(files, inputDir); err != nil {
		return nil, err
	}
	targetDir := inputDir
	stagingDir := ""
	if replace {
		if err := os.MkdirAll(filepath.Dir(inputDir), 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", filepath.Dir(inputDir), err)
		}
		targetDir, err = os.MkdirTemp(filepath.Dir(inputDir), "."+filepath.Base(inputDir)+"-import-")
		if err != nil {
			return nil, fmt.Errorf("create temporary input import under %s: %w", filepath.Dir(inputDir), err)
		}
		if err := os.Chmod(targetDir, 0o700); err != nil {
			_ = os.RemoveAll(targetDir)
			return nil, fmt.Errorf("chmod %s: %w", targetDir, err)
		}
		stagingDir = targetDir
	}
	copied, err := importInputFiles(files, targetDir, replace)
	if err != nil {
		if replace {
			_ = os.RemoveAll(targetDir)
		}
		return nil, err
	}
	if replace {
		copied = rewriteImportedPaths(copied, targetDir, inputDir)
	}
	return &PreparedInputImport{
		inputDir:   inputDir,
		stagingDir: stagingDir,
		paths:      copied,
	}, nil
}

func (p *PreparedInputImport) ValidationPaths() []string {
	if p.stagingDir != "" {
		return []string{p.stagingDir}
	}
	return []string{p.inputDir}
}

func (p *PreparedInputImport) ImportedPaths() []string {
	return append([]string(nil), p.paths...)
}

func (p *PreparedInputImport) Commit() ([]string, error) {
	if p.stagingDir == "" {
		p.committed = true
		return p.ImportedPaths(), nil
	}

	var backupDir string
	if _, err := os.Stat(p.inputDir); err == nil {
		createdBackup, err := os.MkdirTemp(filepath.Dir(p.inputDir), "."+filepath.Base(p.inputDir)+"-backup-")
		if err != nil {
			return nil, fmt.Errorf("create temporary input backup under %s: %w", filepath.Dir(p.inputDir), err)
		}
		if err := os.Remove(createdBackup); err != nil {
			return nil, fmt.Errorf("prepare temporary input backup %s: %w", createdBackup, err)
		}
		if err := os.Rename(p.inputDir, createdBackup); err != nil {
			return nil, fmt.Errorf("backup %s: %w", p.inputDir, err)
		}
		backupDir = createdBackup
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", p.inputDir, err)
	}

	if err := os.Rename(p.stagingDir, p.inputDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, p.inputDir)
		}
		return nil, fmt.Errorf("replace %s: %w", p.inputDir, err)
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return nil, fmt.Errorf("remove temporary input backup %s: %w", backupDir, err)
		}
	}
	p.committed = true
	return p.ImportedPaths(), nil
}

func (p *PreparedInputImport) Cancel() {
	if p.committed || p.stagingDir == "" {
		return
	}
	_ = os.RemoveAll(p.stagingDir)
}

func importInputFiles(files []inputFile, inputDir string, replace bool) ([]string, error) {
	if replace {
		if err := os.RemoveAll(inputDir); err != nil {
			return nil, fmt.Errorf("replace %s: %w", inputDir, err)
		}
	}
	if err := os.MkdirAll(inputDir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", inputDir, err)
	}
	if err := os.Chmod(inputDir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod %s: %w", inputDir, err)
	}

	targets := map[string]bool{}
	var copied []string
	for _, file := range files {
		target := filepath.Join(inputDir, file.RelPath)
		if targets[target] && !replace {
			return nil, fmt.Errorf("multiple input files resolve to %s", target)
		}
		targets[target] = true
		if _, err := os.Stat(target); err == nil && !replace {
			return nil, fmt.Errorf("%s already exists; rerun with --yes to replace imported input files", target)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat %s: %w", target, err)
		}
		if err := copyFile(file.Path, target); err != nil {
			return nil, err
		}
		copied = append(copied, target)
	}
	sort.Strings(copied)
	return copied, nil
}

func rejectInputSourcesInside(paths []string, inputDir string) error {
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		path, err := cleanPath(raw)
		if err != nil {
			return err
		}
		if pathWithinOrSame(path, inputDir) {
			return fmt.Errorf("import source %s must not be inside target input directory %s", path, inputDir)
		}
	}
	return nil
}

func rejectInputFilesInside(files []inputFile, inputDir string) error {
	for _, file := range files {
		path, err := cleanPath(file.Path)
		if err != nil {
			return err
		}
		if pathWithinOrSame(path, inputDir) {
			return fmt.Errorf("import source %s must not be inside target input directory %s", path, inputDir)
		}
	}
	return nil
}

func pathWithinOrSame(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func rewriteImportedPaths(paths []string, fromRoot, toRoot string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(fromRoot, path)
		if err != nil {
			out = append(out, filepath.Join(toRoot, filepath.Base(path)))
			continue
		}
		out = append(out, filepath.Join(toRoot, rel))
	}
	sort.Strings(out)
	return out
}

type inputFile struct {
	Path    string
	RelPath string
}

var nonInputDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
}

func discoverInputFiles(paths []string) ([]inputFile, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one -f path is required")
	}
	var out []inputFile
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, errors.New("empty -f path is not allowed")
		}
		info, err := os.Lstat(raw)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", raw, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("input source %s must not be a symlink", raw)
		}
		if !info.IsDir() {
			if !isYAML(raw) {
				return nil, fmt.Errorf("%s is not a .yaml or .yml file", raw)
			}
			out = append(out, inputFile{Path: filepath.Clean(raw), RelPath: filepath.Base(raw)})
			continue
		}
		root := filepath.Clean(raw)
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root {
					if strings.HasPrefix(entry.Name(), ".") {
						return filepath.SkipDir
					}
					if _, skip := nonInputDirs[entry.Name()]; skip {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if isYAML(path) {
					return fmt.Errorf("input file %s must not be a symlink", path)
				}
				return nil
			}
			if !isYAML(path) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, inputFile{Path: filepath.Clean(path), RelPath: filepath.Clean(rel)})
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", raw, err)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no .yaml or .yml files found")
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RelPath == out[j].RelPath {
			return out[i].Path < out[j].Path
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out, nil
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("input file %s must not be a symlink", src)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input file %s must be a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
	}
	if err := os.Chmod(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}
