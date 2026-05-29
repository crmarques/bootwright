package cli

import (
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/runtime/context"
)

const (
	ansibleVenvDirName    = "ansible-venv"
	ansibleBundlesDirName = "ansible-bundles"
)

func defaultControllerCLIInstallDir() string {
	return "/usr/local/bin"
}

func cacheDir() string {
	return filepath.Join(contextstore.RootDir(), contextstore.CacheDirName)
}

func ansibleVenvDir() string {
	return filepath.Join(cacheDir(), ansibleVenvDirName)
}

func controllerRuntimeDir(contextName string) string {
	ctx, err := contextstore.NewContext(contextName)
	if err != nil {
		return filepath.Join(contextstore.RootDir(), "contexts", contextName, contextstore.RuntimeDirName)
	}
	return ctx.RuntimeDir
}

func ansibleVenvBin(name string) string {
	return filepath.Join(ansibleVenvDir(), "bin", name)
}

func resolveAnsiblePlaybook() string {
	bin := ansibleVenvBin("ansible-playbook")
	if isExecutable(bin) {
		return bin
	}
	return "ansible-playbook"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
