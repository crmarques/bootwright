package cli

import (
	"os"
	"path/filepath"
)

const (
	bootwrightBaseDirEnv    = "BOOTWRIGHT_BASE_DIR"
	bootwrightStateDirEnv   = "BOOTWRIGHT_STATE_DIR"
	bootwrightSecretsDirEnv = "BOOTWRIGHT_SECRETS_DIR"

	defaultHostStateDir = "/var/lib/bootwright"
	ansibleVenvDirName  = "ansible-venv"
)

func openshiftInstallSearchDirs(_ string) []string {
	return []string{defaultControllerCLIInstallDir()}
}

func defaultControllerCLIInstallDir() string {
	return "/usr/local/bin"
}

func ansibleVenvDir() string {
	return filepath.Join(defaultHostStateDir, ansibleVenvDirName)
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
