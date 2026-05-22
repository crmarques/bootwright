package contextstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ImportInputs(paths []string, inputDir string, replace bool) ([]string, error) {
	files, err := discoverInputFiles(paths)
	if err != nil {
		return nil, err
	}
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

type inputFile struct {
	Path    string
	RelPath string
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
		info, err := os.Stat(raw)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", raw, err)
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
				if path != root && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
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
