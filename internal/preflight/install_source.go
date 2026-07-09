package preflight

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	checkGroupPackageSource   = "Package source"
	packageSourceRepoMetadata = "repodata/repomd.xml"
)

func packageSourceReachabilityChecks(state v1alpha1.State, selected []Phase, deps Deps, secretScope *SecretScope) []Check {
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
		mirror := profile.Spec.Installer.Anaconda.PackageSource.GetMirror()
		if mirror == nil {
			continue
		}
		for _, base := range packageMirrorURLs(mirror) {
			if seen[base] {
				continue
			}
			seen[base] = true
			checks = append(checks, packageSourceMirrorCheck(base, deps))
		}
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	return checks
}

func packageMirrorURLs(mirror *v1alpha1.MachineInstallPackageMirror) []string {
	var urls []string
	if mirror.BaseURL != "" {
		urls = append(urls, mirror.BaseURL)
	}
	for _, repo := range mirror.Repositories {
		if repo.BaseURL != "" {
			urls = append(urls, repo.BaseURL)
		}
	}
	return urls
}

func packageSourceMirrorCheck(baseURL string, deps Deps) Check {
	if strings.ContainsRune(baseURL, '$') {
		return infoCheck(checkGroupPackageSource, baseURL, "contains a yum variable (e.g. $basearch/$releasever); the install target expands it at install time, so the controller cannot probe this URL")
	}
	probe := strings.TrimRight(baseURL, "/") + "/" + packageSourceRepoMetadata
	req, err := http.NewRequest(http.MethodGet, probe, nil)
	if err != nil {
		return failCheck(checkGroupPackageSource, baseURL, err.Error(),
			"Boot-ISO installs fetch packages from this repository during install",
			"check the packageSource.mirror URL "+baseURL)
	}
	resp, err := preflightHTTPDo(deps, req, false)
	if err != nil {
		return Check{
			Group:       checkGroupPackageSource,
			Name:        baseURL,
			Status:      StatusWarn,
			Evidence:    "controller probe failed: " + err.Error(),
			Impact:      "The install target fetches packages from this repository; the controller could not verify it (it may not share the install network)",
			Remediation: "confirm the install nodes can reach " + baseURL + " (for a self-signed mirror, add its CA to Environment.spec.installTrust.caBundleRefs)",
		}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return okCheck(checkGroupPackageSource, baseURL, fmt.Sprintf("%s answered HTTP %d", probe, resp.StatusCode))
	default:
		return failCheck(checkGroupPackageSource, baseURL,
			fmt.Sprintf("%s answered HTTP %d", probe, resp.StatusCode),
			"The server answered but serves no yum metadata at this path, so Anaconda cannot install from it",
			"point packageSource.mirror.baseURL or repositories[].baseURL at the install-tree root that contains repodata/ (for example .../BaseOS/x86_64/os/)")
	}
}
