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
	InputSourceFileName = "input-source.yaml"
	RenderedDirName     = "rendered"
	SecretsDirName      = "secrets"
	RunsDirName         = "runs"
	ClustersDirName     = "clusters"
	ManagedServicesName = "managed-services"
	ProviderStateName   = "provider-state"
	OwnershipName       = "ownership"
)

type Store struct {
	Current string `yaml:"current,omitempty" json:"current,omitempty"`
}

// Context describes one shared context under the Bootwright root. InputDir
// and InputPaths point at the operator's workspace directory recorded at
// `context init` time (see InputSourceFileName); every other directory is
// Bootwright-owned output/state under BaseDir.
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
	OwnershipDir       string   `yaml:"-" json:"ownershipDir"`
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

func newContextAt(name, baseDir string) Context {
	return Context{
		Name:               name,
		BaseDir:            baseDir,
		RenderedDir:        filepath.Join(baseDir, RenderedDirName),
		SecretsDir:         filepath.Join(baseDir, SecretsDirName),
		RunsDir:            filepath.Join(baseDir, RunsDirName),
		ClustersDir:        filepath.Join(baseDir, ClustersDirName),
		ManagedServicesDir: filepath.Join(baseDir, ManagedServicesName),
		ProviderStateDir:   filepath.Join(baseDir, ProviderStateName),
		OwnershipDir:       filepath.Join(baseDir, OwnershipName),
	}
}

// WithInputSource returns a copy of ctx whose InputDir and InputPaths point at
// the given workspace directory.
func (ctx Context) WithInputSource(sourceDir string) Context {
	ctx.InputDir = sourceDir
	ctx.InputPaths = []string{sourceDir}
	return ctx
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
	store.Current = strings.TrimSpace(store.Current)
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
	current := strings.TrimSpace(store.Current)
	if current == "" {
		return Context{}, errors.New("no current context; run `bootwright context init <name> -f <path>` or `bootwright context use <name>`")
	}
	ctx, err := ResolveExistingContext(current)
	if err != nil {
		return Context{}, fmt.Errorf("current context %q is not available in shared storage: %w", current, err)
	}
	return ctx, nil
}

func ListContexts() ([]Context, error) {
	contextsDir, err := ContextsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(contextsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read contexts directory %s: %w", contextsDir, err)
	}
	var contexts []Context
	for _, entry := range entries {
		name := entry.Name()
		if err := ValidateName(name); err != nil {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		ctx, err := NewContext(name)
		if err != nil {
			return nil, err
		}
		usable, err := isUsableContext(ctx)
		if err != nil {
			return nil, err
		}
		if usable {
			contexts = append(contexts, ctx)
		}
	}
	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].Name < contexts[j].Name
	})
	return contexts, nil
}

func ContextsDir() (string, error) {
	return cleanPath(filepath.Join(rootDir, "contexts"))
}

func ContextExists(name string) (bool, error) {
	ctx, err := NewContext(name)
	if err != nil {
		return false, err
	}
	return isUsableContext(ctx)
}

func RequireExistingContext(name string) (Context, error) {
	ctx, err := NewContext(name)
	if err != nil {
		return Context{}, err
	}
	info, err := os.Lstat(ctx.BaseDir)
	if errors.Is(err, os.ErrNotExist) {
		contextsDir, dirErr := ContextsDir()
		if dirErr != nil {
			return Context{}, dirErr
		}
		return Context{}, fmt.Errorf("context %q not found under %s", name, contextsDir)
	}
	if err != nil {
		return Context{}, fmt.Errorf("stat context %q at %s: %w", name, ctx.BaseDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Context{}, fmt.Errorf("context %q path must not be a symlink: %s", name, ctx.BaseDir)
	}
	if !info.IsDir() {
		return Context{}, fmt.Errorf("context %q path is not a directory: %s", name, ctx.BaseDir)
	}
	ok, err := managedroot.MarkerExists(ctx.BaseDir)
	if err != nil {
		return Context{}, err
	}
	if !ok {
		return Context{}, fmt.Errorf("context %q is not marked as Bootwright-managed at %s", name, ctx.BaseDir)
	}
	return ctx, nil
}

// ResolveExistingContext returns the context with its recorded workspace
// directory loaded into InputDir/InputPaths. It is the resolution path every
// command uses; a context without a recorded workspace path (for example one
// created before workspace recording existed) fails with a named error.
func ResolveExistingContext(name string) (Context, error) {
	ctx, err := RequireExistingContext(name)
	if err != nil {
		return Context{}, err
	}
	source, err := ReadInputSource(ctx)
	if err != nil {
		return Context{}, err
	}
	return ctx.WithInputSource(source), nil
}

func isUsableContext(ctx Context) (bool, error) {
	info, err := os.Lstat(ctx.BaseDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat context %q at %s: %w", ctx.Name, ctx.BaseDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	ok, err := managedroot.MarkerExists(ctx.BaseDir)
	if err != nil {
		return false, err
	}
	return ok, nil
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
		ctx.RenderedDir,
		ctx.SecretsDir,
		ctx.RunsDir,
		ctx.ClustersDir,
		ctx.ManagedServicesDir,
		ctx.ProviderStateDir,
		ctx.OwnershipDir,
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
		"renderedDir":        filepath.Join(baseDir, RenderedDirName),
		"secretsDir":         filepath.Join(baseDir, SecretsDirName),
		"runsDir":            filepath.Join(baseDir, RunsDirName),
		"clustersDir":        filepath.Join(baseDir, ClustersDirName),
		"managedServicesDir": filepath.Join(baseDir, ManagedServicesName),
		"providerStateDir":   filepath.Join(baseDir, ProviderStateName),
		"ownershipDir":       filepath.Join(baseDir, OwnershipName),
	}
	got := map[string]string{
		"renderedDir":        ctx.RenderedDir,
		"secretsDir":         ctx.SecretsDir,
		"runsDir":            ctx.RunsDir,
		"clustersDir":        ctx.ClustersDir,
		"managedServicesDir": ctx.ManagedServicesDir,
		"providerStateDir":   ctx.ProviderStateDir,
		"ownershipDir":       ctx.OwnershipDir,
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
	if strings.TrimSpace(store.Current) != "" {
		return ValidateName(store.Current)
	}
	return nil
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
