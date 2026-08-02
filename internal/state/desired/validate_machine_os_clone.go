package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateMachineInstallCloneArm(prefix string, profile v1alpha1.MachineInstallProfile) []string {
	clone := profile.Spec.Installer.TemplateClone
	if clone == nil {
		return nil
	}
	var errs []string
	if clone.Seed.ArmCount() != 1 {
		errs = append(errs, prefix+".installer.templateClone.seed must set exactly one of: cloudInit")
	}
	return append(errs, validateMachineInstallCloneRefusals(prefix+".customizations", profile.Spec.Customizations)...)
}

func validateMachineInstallCloneRefusals(prefix string, c v1alpha1.MachineInstallCustomizations) []string {
	var errs []string
	if c.Storage.RootDevice.Source != "" {
		errs = append(errs, prefix+".storage.rootDevice has no effect under installer.templateClone: a clone inherits the template's partitioning and Bootwright never runs clearpart on it. Remove storage.rootDevice, or switch the profile to installer.anaconda to have Bootwright partition the disk.")
	}
	if c.Packages.Environment != "" || len(c.Packages.Install) > 0 || c.Packages.ExcludeDocs || c.Packages.InstallWeakDeps != nil {
		errs = append(errs, prefix+".packages has no effect under installer.templateClone: the template's package set is fixed when the template is built. Build the packages into the template (it must ship at least openssh-server, nmstate and cloud-init), or declare day-2 repositories in customizations.repositories.")
	}
	if l := c.Localization; l.Language != "" || l.Formats != "" || l.Keyboard != "" || l.Timezone != "" || len(l.AdditionalLocales) > 0 {
		errs = append(errs, prefix+".localization has no effect under installer.templateClone: the template owns its locale, keyboard and timezone. Set them when the template is built.")
	}
	if c.Security.SELinux.Mode != "" {
		errs = append(errs, prefix+".security.selinux has no effect under installer.templateClone: SELinux mode is a property of the template. Set it when the template is built.")
	}
	if c.Security.Firewall.Enabled != nil {
		errs = append(errs, prefix+".security.firewall has no effect under installer.templateClone: the firewall state is a property of the template. Set it when the template is built.")
	}
	if c.Security.FIPS.Enabled {
		errs = append(errs, prefix+".security.fips.enabled is not supported under installer.templateClone: FIPS is enabled by an installer kernel argument (fips=1) that only Anaconda can pass. Build the template with fips-mode-setup --enable, or switch the profile to installer.anaconda.")
	}
	if c.Security.DiskEncryption != nil {
		errs = append(errs, prefix+".security.diskEncryption is not supported under installer.templateClone: the LUKS container is created by the installer's partitioner, and a clone never partitions. Build the template with an encrypted root, or switch the profile to installer.anaconda.")
	}
	if c.SSH.InitialPassword != nil {
		errs = append(errs, prefix+".ssh.initialPassword is not supported under installer.templateClone: the clone is personalized through vCenter extraConfig, which is stored in plaintext in the VMX and readable by anyone with VirtualMachine read privilege. Only the install identity's public key is delivered that way. Reach the machine over SSH with the key in access.ssh, or switch the profile to installer.anaconda.")
	}
	return errs
}
