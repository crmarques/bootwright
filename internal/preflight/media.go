package preflight

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const checkGroupInstallerMedia = "Installer media"

// installerMediaChecks verifies that every controller-local install ISO a
// managed-OS machine references (a "local-media:" managed-store entry or a
// "file://" path) is present on the controller before the machines phase runs.
// The machine_os_install_anaconda role copies that source ISO from the
// controller (remote_src: false) to build the Kickstart install media with
// mkksiso, so a missing file otherwise fails deep into a run — minutes per node
// — with an opaque Ansible "Could not find or access ... on the Ansible
// Controller" error. Surfacing it here points the operator at "bootwright media
// add" up front. "http(s)://" images download on the provider host and need no
// controller-local file, so they are skipped.
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
		for _, ref := range machineImageMediaRefs(image) {
			resolved, err := media.Resolve(ref)
			if err != nil || resolved.Path == "" {
				// A bad reference is a validate-phase concern; an empty path is an
				// http(s):// image the provider host fetches itself.
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

// machineImageMediaRefs lists the local ISO references an image needs staged on
// the controller: bootMedia always, plus the hostedTree DVD when set.
func machineImageMediaRefs(image v1alpha1.MachineImage) []string {
	refs := []string{image.Spec.BootMedia}
	if t := image.Spec.PackageSource.GetHostedTree(); t != nil {
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
