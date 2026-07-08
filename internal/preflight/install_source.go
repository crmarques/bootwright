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

// packageSourceReachabilityChecks probes the package install trees a boot-ISO
// managed-OS machine depends on. A boot ISO carries no packages, so Anaconda
// fetches them from the Anaconda packageSource during install; an unreachable
// mirror or a wrong install-tree
// path otherwise fails minutes deep in the machines phase with an opaque
// Anaconda error. Surfacing it here points the operator at the mirror up front.
//
// The probe is best-effort: the install *target*, not the controller, is the
// authoritative fetcher, so a controller that cannot reach the mirror (a
// different network, a self-signed TLS chain it does not trust) only warns,
// while a mirror that answers but serves no repodata at the path (a wrong
// install-tree URL) fails outright. redhatCDN package sources are skipped — the
// CDN needs entitlement auth to probe, and its credentials are already covered
// by the entitlement secret-material checks.
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
			// A full DVD carries its own packages; redhatCDN reachability hides
			// behind entitlement auth; a hostedTree is served locally by
			// bootwright. None is probed here — only an operator-hosted mirror.
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

// packageMirrorURLs lists the install-tree and repository base URLs that
// Anaconda fetches packages from, in declaration order (primary tree first).
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
	// A baseURL carrying yum variables ($basearch/$releasever/...) is resolved by
	// Anaconda on the install target, not the controller. Probing the literal
	// unexpanded path would 404 and hard-fail a source the target can install
	// from, so report it INFO (operator-resolved) instead of probing.
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
