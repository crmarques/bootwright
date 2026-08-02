package roles

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	OSInstallRoleAnaconda      = "bootwright.core.machine_os_install_anaconda"
	OSInstallRoleTemplateClone = "bootwright.core.machine_os_install_clone"
)

func LookupOSInstallRole(installer v1alpha1.MachineInstallProfileInstaller) string {
	switch installer.Mode() {
	case v1alpha1.MachineInstallModeAnaconda:
		return OSInstallRoleAnaconda
	case v1alpha1.MachineInstallModeTemplateClone:
		return OSInstallRoleTemplateClone
	default:
		return ""
	}
}
