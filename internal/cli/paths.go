package cli

import (
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/contextstore"
)

const (
	ansibleVenvDirName = "ansible-venv"
)

func defaultControllerCLIInstallDir() string {
	return "/usr/local/bin"
}

func ansibleVenvDir() string {
	return filepath.Join(contextstore.RootDir(), ansibleVenvDirName)
}

func controllerRuntimeDir(contextName string) string {
	ctx, err := contextstore.NewContext(contextName)
	if err != nil {
		return filepath.Join(contextstore.RootDir(), "contexts", contextName)
	}
	return ctx.BaseDir
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
