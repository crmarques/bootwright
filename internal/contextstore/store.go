package contextstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/managedroot"
	"github.com/crmarques/bootwright/internal/safefs"
	"go.yaml.in/yaml/v3"
)

const (
	RegistryFileName = "contexts.yaml"
	InputDirName     = "input-files"
	StateDirName     = "state"
	SecretsDirName   = "secrets"
)

type Store struct {
	Current  string             `yaml:"current,omitempty" json:"current,omitempty"`
	Contexts map[string]Context `yaml:"contexts,omitempty" json:"contexts,omitempty"`
}

type Context struct {
	Name       string   `yaml:"-" json:"name"`
	BaseDir    string   `yaml:"baseDir" json:"baseDir"`
	InputDir   string   `yaml:"inputDir" json:"inputDir"`
	StateDir   string   `yaml:"stateDir" json:"stateDir"`
	SecretsDir string   `yaml:"secretsDir" json:"secretsDir"`
	InputPaths []string `yaml:"inputPaths" json:"inputPaths"`
}

var contextNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func DefaultRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bootwright", RegistryFileName), nil
}

func DefaultBaseDir(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "bootwright", name), nil
}

func NewContext(name, baseDir string) (Context, error) {
	if err := ValidateName(name); err != nil {
		return Context{}, err
	}
	if strings.TrimSpace(baseDir) == "" {
		var err error
		baseDir, err = DefaultBaseDir(name)
		if err != nil {
			return Context{}, err
		}
	}
	baseDir, err := cleanPath(baseDir)
	if err != nil {
		return Context{}, err
	}
	if _, err := managedroot.ValidateTarget(baseDir); err != nil {
		return Context{}, err
	}
	inputDir := filepath.Join(baseDir, InputDirName)
	return Context{
		Name:       name,
		BaseDir:    baseDir,
		InputDir:   inputDir,
		StateDir:   filepath.Join(baseDir, StateDirName),
		SecretsDir: filepath.Join(baseDir, SecretsDirName),
		InputPaths: []string{inputDir},
	}, nil
}

func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("context name is required")
	}
	if len(name) > 63 || !contextNameRE.MatchString(name) {
		return fmt.Errorf("context name %q must be a lowercase DNS label", name)
	}
	return nil
}

func Load(path string) (Store, error) {
	var store Store
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		store.Contexts = map[string]Context{}
		return store, nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if store.Contexts == nil {
		store.Contexts = map[string]Context{}
	}
	for name, ctx := range store.Contexts {
		ctx.Name = name
		store.Contexts[name] = ctx
	}
	return store, nil
}

func Save(path string, store Store) error {
	if store.Contexts == nil {
		store.Contexts = map[string]Context{}
	}
	if store.Current != "" {
		if _, ok := store.Contexts[store.Current]; !ok {
			return fmt.Errorf("current context %q is not defined", store.Current)
		}
	}
	data, err := yaml.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", filepath.Dir(path), err)
	}
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

func Current(store Store) (Context, error) {
	if strings.TrimSpace(store.Current) == "" {
		return Context{}, errors.New("no current context; run `bootwright context init <name> -f <path>` or `bootwright context use <name>`")
	}
	ctx, ok := store.Contexts[store.Current]
	if !ok {
		return Context{}, fmt.Errorf("current context %q is not defined", store.Current)
	}
	ctx.Name = store.Current
	if err := ValidateContext(ctx); err != nil {
		return Context{}, err
	}
	return ctx, nil
}

func Names(store Store) []string {
	names := make([]string, 0, len(store.Contexts))
	for name := range store.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func EnsureDirs(ctx Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}
	if err := EnsureBaseDir(ctx); err != nil {
		return err
	}
	for _, dir := range []string{ctx.BaseDir, ctx.InputDir, ctx.StateDir, ctx.SecretsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
	}
	return nil
}

func EnsureBaseDir(ctx Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}
	_, err := managedroot.Ensure(ctx.BaseDir, 0o700)
	return err
}

func SafePurgeBaseDir(ctx Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}
	baseDir, err := managedroot.Require(ctx.BaseDir)
	if err != nil {
		return err
	}
	return os.RemoveAll(baseDir)
}

func ValidateContext(ctx Context) error {
	if err := ValidateName(ctx.Name); err != nil {
		return err
	}
	baseDir, err := cleanPath(ctx.BaseDir)
	if err != nil {
		return err
	}
	if _, err := managedroot.ValidatePath(baseDir); err != nil {
		return err
	}
	want := map[string]string{
		"inputDir":   filepath.Join(baseDir, InputDirName),
		"stateDir":   filepath.Join(baseDir, StateDirName),
		"secretsDir": filepath.Join(baseDir, SecretsDirName),
	}
	got := map[string]string{
		"inputDir":   ctx.InputDir,
		"stateDir":   ctx.StateDir,
		"secretsDir": ctx.SecretsDir,
	}
	for field, raw := range got {
		clean, err := cleanPath(raw)
		if err != nil {
			return fmt.Errorf("context %q %s: %w", ctx.Name, field, err)
		}
		if clean != want[field] {
			return fmt.Errorf("context %q %s must be %s, got %s", ctx.Name, field, want[field], clean)
		}
	}
	return nil
}

func cleanPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	return filepath.Clean(abs), nil
}
