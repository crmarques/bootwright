package preflight

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const checkGroupInstallerMedia = "Installer media"

func installerMediaChecks(state v1alpha1.State, selected []Phase, deps Deps, secretScope *SecretScope) []Check {
	if !phaseInScope("machines", selected, true) {
		return nil
	}
	seen := map[string]bool{}
	var checks []Check
	for _, machine := range state.Machines {
		if !v1alpha1.MachineInstallsOS(machine) || !secretScope.allowsMachine(machine.Metadata.Name) {
			continue
		}
		profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
		if !ok || profile.Spec.Installer.Anaconda == nil {
			continue
		}
		image, ok := stateview.MachineImage(state, profile.Spec.Installer.Anaconda.ImageRef.Name)
		if !ok {
			continue
		}
		for _, ref := range machineInstallMediaRefs(image, profile) {
			resolved, err := media.Resolve(ref)
			if err != nil || resolved.Path == "" {
				continue
			}
			if seen[resolved.Path] {
				continue
			}
			seen[resolved.Path] = true
			checks = append(checks, installerMediaCheck(resolved, deps))
		}
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	return checks
}

func machineInstallMediaRefs(image v1alpha1.MachineImage, profile v1alpha1.MachineInstallProfile) []string {
	refs := []string{image.Spec.BootMedia}
	if t := profile.Spec.Installer.Anaconda.PackageSource.GetHostedTree(); t != nil {
		refs = append(refs, t.FromMedia)
	}
	return refs
}

func installerMediaCheck(resolved media.Resolved, deps Deps) Check {
	name := resolved.Original
	info, err := deps.StatPath(resolved.Path)
	if err != nil {
		return failCheck(checkGroupInstallerMedia, name, resolved.Path+" missing",
			"Managed-OS install copies this source ISO from the controller to build the Kickstart media",
			installerMediaRemediation(resolved))
	}
	if info.IsDir() {
		return failCheck(checkGroupInstallerMedia, name, resolved.Path+" is a directory",
			"The install source ISO must be a regular file",
			installerMediaRemediation(resolved))
	}
	return okCheck(checkGroupInstallerMedia, name, resolved.Path)
}

func installerMediaRemediation(resolved media.Resolved) string {
	if resolved.Key != "" {
		return "bootwright media add --name " + resolved.Key + " --from-file <iso> (or --from-url <url>)"
	}
	return "place the install ISO at " + resolved.Path
}
