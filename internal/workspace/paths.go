package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	ansibleVenvDirName    = "ansible-venv"
	ansibleBundlesDirName = "ansible-bundles"
)

func DefaultControllerCLIInstallDir() string {
	return "/usr/local/bin"
}

func CacheDir() string {
	return filepath.Join(RootDir(), CacheDirName)
}

func AnsibleVenvDir() string {
	return filepath.Join(CacheDir(), ansibleVenvDirName)
}

func ControllerClustersDir(contextName string) string {
	ctx, err := NewContext(contextName)
	if err != nil {
		return filepath.Join(RootDir(), "contexts", contextName, ClustersDirName)
	}
	return ctx.ClustersDir
}

func AnsibleVenvBin(name string) string {
	return filepath.Join(AnsibleVenvDir(), "bin", name)
}

func ResolveAnsiblePlaybook() string {
	bin := AnsibleVenvBin("ansible-playbook")
	if isExecutable(bin) {
		return bin
	}
	return "ansible-playbook"
}

func BundleDir(versionMarker string) (string, error) {
	return filepath.Abs(filepath.Join(CacheDir(), ansibleBundlesDirName, bundleCacheKey(versionMarker)))
}

func bundleCacheKey(versionMarker string) string {
	for _, line := range strings.Split(versionMarker, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "version=unknown"
}

func CurrentContext() (Context, error) {
	_, store, err := LoadDefaultStore()
	if err != nil {
		return Context{}, err
	}
	return Current(store)
}

func LoadDefaultStore() (string, Store, error) {
	registry, err := DefaultRegistryPath()
	if err != nil {
		return "", Store{}, err
	}
	store, err := Load(registry)
	if err != nil {
		return "", Store{}, err
	}
	return registry, store, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
