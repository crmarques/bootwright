package contextstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/runtime/root/localroot"
	"github.com/crmarques/bootwright/internal/runtime/root/managedroot"
	"go.yaml.in/yaml/v3"
)

const (
	RegistryFileName    = "contexts.yaml"
	InternalRegistryEnv = "BOOTWRIGHT_INTERNAL_REGISTRY"
	DefaultRootDir      = "/var/lib/bootwright"
	CacheDirName        = "cache"
	InputDirName        = "input"
	RenderedDirName     = "rendered"
	SecretsDirName      = "secrets"
	RunsDirName         = "runs"
	ClustersDirName     = "clusters"
	ManagedServicesName = "managed-services"
	ProviderStateName   = "provider-state"
)

type Store struct {
	Current  string   `yaml:"current,omitempty" json:"current,omitempty"`
	Contexts []string `yaml:"contexts,omitempty" json:"contexts,omitempty"`
}

type Context struct {
	Name               string   `yaml:"-" json:"name"`
	BaseDir            string   `yaml:"-" json:"baseDir"`
	InputDir           string   `yaml:"-" json:"inputDir"`
	RenderedDir        string   `yaml:"-" json:"renderedDir"`
	SecretsDir         string   `yaml:"-" json:"secretsDir"`
	RunsDir            string   `yaml:"-" json:"runsDir"`
	ClustersDir        string   `yaml:"-" json:"clustersDir"`
	ManagedServicesDir string   `yaml:"-" json:"managedServicesDir"`
	ProviderStateDir   string   `yaml:"-" json:"providerStateDir"`
	InputPaths         []string `yaml:"-" json:"inputPaths"`
}

type ClusterPaths struct {
	Name        string
	BaseDir     string
	RenderedDir string
	RuntimeDir  string
	SecretsDir  string
	RunsDir     string
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
	if override := strings.TrimSpace(os.Getenv(InternalRegistryEnv)); override != "" && localroot.IsInternalRootChild() {
		path, err := cleanPath(override)
		if err != nil {
			return "", err
		}
		return path, nil
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
		Name:               name,
		BaseDir:            baseDir,
		InputDir:           inputDir,
		RenderedDir:        filepath.Join(baseDir, RenderedDirName),
		SecretsDir:         filepath.Join(baseDir, SecretsDirName),
		RunsDir:            filepath.Join(baseDir, RunsDirName),
		ClustersDir:        filepath.Join(baseDir, ClustersDirName),
		ManagedServicesDir: filepath.Join(baseDir, ManagedServicesName),
		ProviderStateDir:   filepath.Join(baseDir, ProviderStateName),
		InputPaths:         []string{inputDir},
	}
}

func (ctx Context) Cluster(name string) ClusterPaths {
	baseDir := filepath.Join(ctx.ClustersDir, name)
	return ClusterPaths{
		Name:        name,
		BaseDir:     baseDir,
		RenderedDir: filepath.Join(baseDir, RenderedDirName),
		RuntimeDir:  filepath.Join(baseDir, "runtime"),
		SecretsDir:  filepath.Join(baseDir, SecretsDirName),
		RunsDir:     filepath.Join(baseDir, RunsDirName),
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
	if err := rejectLegacyContextMapRegistry(path, data); err != nil {
		return Store{}, err
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
	chownRegistryToInternalCaller(dir, path)
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
		ctx.RenderedDir,
		ctx.SecretsDir,
		ctx.RunsDir,
		ctx.ClustersDir,
		ctx.ManagedServicesDir,
		ctx.ProviderStateDir,
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
	cacheDir := filepath.Join(root, CacheDirName)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", cacheDir, err)
	}
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", cacheDir, err)
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
		"inputDir":           filepath.Join(baseDir, InputDirName),
		"renderedDir":        filepath.Join(baseDir, RenderedDirName),
		"secretsDir":         filepath.Join(baseDir, SecretsDirName),
		"runsDir":            filepath.Join(baseDir, RunsDirName),
		"clustersDir":        filepath.Join(baseDir, ClustersDirName),
		"managedServicesDir": filepath.Join(baseDir, ManagedServicesName),
		"providerStateDir":   filepath.Join(baseDir, ProviderStateName),
	}
	got := map[string]string{
		"inputDir":           ctx.InputDir,
		"renderedDir":        ctx.RenderedDir,
		"secretsDir":         ctx.SecretsDir,
		"runsDir":            ctx.RunsDir,
		"clustersDir":        ctx.ClustersDir,
		"managedServicesDir": ctx.ManagedServicesDir,
		"providerStateDir":   ctx.ProviderStateDir,
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

func rejectLegacyContextMapRegistry(path string, data []byte) error {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := node.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "contexts" && root.Content[i+1].Kind == yaml.MappingNode {
			return fmt.Errorf("validate %s: legacy context registry map is not supported; remove %s and recreate contexts with `bootwright context init <name> -f <path> --yes`", path, path)
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

func chownRegistryToInternalCaller(paths ...string) {
	uid, gid, ok := localroot.CallerUIDGID()
	if !ok {
		return
	}
	for _, path := range paths {
		_ = os.Chown(path, int(uid), int(gid))
	}
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
