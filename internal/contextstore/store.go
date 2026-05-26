package contextstore

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/managedroot"
	"github.com/crmarques/bootwright/internal/safefs"
	"go.yaml.in/yaml/v3"
)

const (
	RegistryFileName    = "contexts.yaml"
	InternalRegistryEnv = "BOOTWRIGHT_INTERNAL_REGISTRY"
	DefaultRootDir      = "/var/lib/bootwright"
	InputDirName        = "input-files"
	StateDirName        = "state"
	SecretsDirName      = "secrets"
	RuntimeDirName      = "runtime"
	WorkflowDirName     = "workflow"
	ArtifactsDirName    = "artifacts-server"
)

type Store struct {
	Current  string   `yaml:"current,omitempty" json:"current,omitempty"`
	Contexts []string `yaml:"contexts,omitempty" json:"contexts,omitempty"`
}

type Context struct {
	Name        string   `yaml:"-" json:"name"`
	BaseDir     string   `yaml:"-" json:"baseDir"`
	InputDir    string   `yaml:"-" json:"inputDir"`
	StateDir    string   `yaml:"-" json:"stateDir"`
	SecretsDir  string   `yaml:"-" json:"secretsDir"`
	RuntimeDir  string   `yaml:"-" json:"runtimeDir"`
	WorkflowDir string   `yaml:"-" json:"workflowDir"`
	InputPaths  []string `yaml:"-" json:"inputPaths"`
}

var contextNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var rootDir = DefaultRootDir

func SetRootDirForTest(path string) func() {
	previous := rootDir
	rootDir = path
	return func() { rootDir = previous }
}

func RootDir() string {
	return rootDir
}

func DefaultRegistryPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(InternalRegistryEnv)); override != "" && os.Geteuid() == 0 {
		path, err := cleanPath(override)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, ".bootwright", RegistryFileName), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bootwright", RegistryFileName), nil
}

func NewContext(name string) (Context, error) {
	if err := ValidateName(name); err != nil {
		return Context{}, err
	}
	baseDir, err := cleanPath(filepath.Join(rootDir, "contexts", name))
	if err != nil {
		return Context{}, err
	}
	if _, err := managedroot.ValidatePath(baseDir); err != nil {
		return Context{}, err
	}
	return newContextAt(name, baseDir), nil
}

func NewStagingContext(name, baseDir string) (Context, error) {
	if err := ValidateName(name); err != nil {
		return Context{}, err
	}
	baseDir, err := cleanPath(baseDir)
	if err != nil {
		return Context{}, err
	}
	if _, err := managedroot.ValidateTarget(baseDir); err != nil {
		return Context{}, err
	}
	return newContextAt(name, baseDir), nil
}

func newContextAt(name, baseDir string) Context {
	inputDir := filepath.Join(baseDir, InputDirName)
	return Context{
		Name:        name,
		BaseDir:     baseDir,
		InputDir:    inputDir,
		StateDir:    filepath.Join(baseDir, StateDirName),
		SecretsDir:  filepath.Join(baseDir, SecretsDirName),
		RuntimeDir:  filepath.Join(baseDir, RuntimeDirName),
		WorkflowDir: filepath.Join(baseDir, WorkflowDirName),
		InputPaths:  []string{inputDir},
	}
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
		return store, nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := validateStore(store); err != nil {
		return Store{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return store, nil
}

func Save(path string, store Store) error {
	if err := validateStore(store); err != nil {
		return err
	}
	store.Contexts = normalizeNames(store.Contexts)
	data, err := yaml.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return err
	}
	chownRegistryToSudoUser(dir, path)
	return nil
}

func Current(store Store) (Context, error) {
	if strings.TrimSpace(store.Current) == "" {
		return Context{}, errors.New("no current context; run `bootwright context init <name> -f <path>` or `bootwright context use <name>`")
	}
	if !Contains(store, store.Current) {
		return Context{}, fmt.Errorf("current context %q is not defined", store.Current)
	}
	ctx, err := NewContext(store.Current)
	if err != nil {
		return Context{}, err
	}
	if err := ValidateContext(ctx); err != nil {
		return Context{}, err
	}
	return ctx, nil
}

func Names(store Store) []string {
	return normalizeNames(store.Contexts)
}

func Contains(store Store, name string) bool {
	for _, got := range store.Contexts {
		if got == name {
			return true
		}
	}
	return false
}

func Add(store *Store, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if !Contains(*store, name) {
		store.Contexts = append(store.Contexts, name)
	}
	store.Contexts = normalizeNames(store.Contexts)
	return nil
}

func Remove(store *Store, name string) {
	out := make([]string, 0, len(store.Contexts))
	for _, got := range store.Contexts {
		if got != name {
			out = append(out, got)
		}
	}
	store.Contexts = out
	if store.Current == name {
		store.Current = ""
	}
}

func EnsureDirs(ctx Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}
	if err := EnsureBaseDir(ctx); err != nil {
		return err
	}
	for _, dir := range []string{
		ctx.BaseDir,
		ctx.InputDir,
		ctx.StateDir,
		ctx.SecretsDir,
		ctx.RuntimeDir,
		ctx.WorkflowDir,
		filepath.Join(ctx.BaseDir, ArtifactsDirName),
		filepath.Join(ctx.BaseDir, ArtifactsDirName, "tls"),
	} {
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
	root, err := cleanPath(rootDir)
	if err != nil {
		return err
	}
	if _, err := managedroot.Ensure(root, 0o700); err != nil {
		return err
	}
	contextsDir := filepath.Join(root, "contexts")
	if err := os.MkdirAll(contextsDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", contextsDir, err)
	}
	if err := os.Chmod(contextsDir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", contextsDir, err)
	}
	_, err = managedroot.Ensure(ctx.BaseDir, 0o700)
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
		"inputDir":    filepath.Join(baseDir, InputDirName),
		"stateDir":    filepath.Join(baseDir, StateDirName),
		"secretsDir":  filepath.Join(baseDir, SecretsDirName),
		"runtimeDir":  filepath.Join(baseDir, RuntimeDirName),
		"workflowDir": filepath.Join(baseDir, WorkflowDirName),
	}
	got := map[string]string{
		"inputDir":    ctx.InputDir,
		"stateDir":    ctx.StateDir,
		"secretsDir":  ctx.SecretsDir,
		"runtimeDir":  ctx.RuntimeDir,
		"workflowDir": ctx.WorkflowDir,
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

func validateStore(store Store) error {
	seen := map[string]bool{}
	for _, name := range store.Contexts {
		if err := ValidateName(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("context %q is listed more than once", name)
		}
		seen[name] = true
	}
	if store.Current != "" && !seen[store.Current] {
		return fmt.Errorf("current context %q is not defined", store.Current)
	}
	return nil
}

func normalizeNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}

func chownRegistryToSudoUser(paths ...string) {
	uid, gid, ok := sudoUserIDs()
	if !ok {
		return
	}
	for _, path := range paths {
		_ = os.Chown(path, uid, gid)
	}
}

func sudoUserIDs() (int, int, bool) {
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "" || sudoUser == "root" {
		return 0, 0, false
	}
	uidRaw := strings.TrimSpace(os.Getenv("SUDO_UID"))
	gidRaw := strings.TrimSpace(os.Getenv("SUDO_GID"))
	if uidRaw == "" || gidRaw == "" {
		return 0, 0, false
	}
	uid, uidErr := strconv.Atoi(uidRaw)
	gid, gidErr := strconv.Atoi(gidRaw)
	if uidErr != nil || gidErr != nil || uid < 0 || gid < 0 {
		return 0, 0, false
	}
	return uid, gid, true
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
