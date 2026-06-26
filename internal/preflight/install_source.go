package preflight

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	checkGroupInstallSource   = "Install source"
	installSourceProbeTimeout = 15 * time.Second
	installSourceRepoMetadata = "repodata/repomd.xml"
)

// installSourceReachabilityChecks probes the package install trees a boot-ISO
// managed-OS machine depends on. A boot ISO carries no packages, so Anaconda
// fetches them from spec.installSource (a url-type install tree plus extra
// repositories) during install; an unreachable mirror or a wrong install-tree
// path otherwise fails minutes deep in the machines phase with an opaque
// Anaconda error. Surfacing it here points the operator at the mirror up front.
//
// The probe is best-effort: the install *target*, not the controller, is the
// authoritative fetcher, so a controller that cannot reach the mirror (a
// different network, a self-signed TLS chain it does not trust) only warns,
// while a mirror that answers but serves no repodata at the path (a wrong
// install-tree URL) fails outright. redhatCDN install sources are skipped — the
// CDN needs entitlement auth to probe, and its credentials are already covered
// by the entitlement secret-material checks.
func installSourceReachabilityChecks(state v1alpha1.State, selected []Phase, deps Deps, secretScope *SecretScope) []Check {
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
		source := image.Spec.InstallSource
		if source.Type != v1alpha1.MachineImageInstallSourceTypeURL {
			// DVD media carries its own packages, and redhatCDN reachability
			// hides behind entitlement auth; neither is probed here.
			continue
		}
		for _, base := range installSourceURLs(source) {
			if seen[base] {
				continue
			}
			seen[base] = true
			checks = append(checks, installSourceCheck(base, deps))
		}
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	return checks
}

// installSourceURLs lists the install-tree and repository base URLs that
// Anaconda fetches packages from, in declaration order (primary tree first).
func installSourceURLs(source v1alpha1.MachineImageInstallSource) []string {
	var urls []string
	if source.URL != "" {
		urls = append(urls, source.URL)
	}
	for _, repo := range source.Repositories {
		if repo.BaseURL != "" {
			urls = append(urls, repo.BaseURL)
		}
	}
	return urls
}

func installSourceCheck(baseURL string, deps Deps) Check {
	probe := strings.TrimRight(baseURL, "/") + "/" + installSourceRepoMetadata
	req, err := http.NewRequest(http.MethodGet, probe, nil)
	if err != nil {
		return failCheck(checkGroupInstallSource, baseURL, err.Error(),
			"Boot-ISO installs fetch packages from this repository during install",
			"check the installSource URL "+baseURL)
	}
	resp, err := installSourceHTTPDo(deps, req)
	if err != nil {
		return Check{
			Group:       checkGroupInstallSource,
			Name:        baseURL,
			Status:      StatusWarn,
			Evidence:    "controller probe failed: " + err.Error(),
			Impact:      "The install target fetches packages from this repository; the controller could not verify it (it may not share the install network)",
			Remediation: "confirm the install nodes can reach " + baseURL + " (for a self-signed mirror, add its CA to the MachineImage trustRefs)",
		}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return okCheck(checkGroupInstallSource, baseURL, fmt.Sprintf("%s answered HTTP %d", probe, resp.StatusCode))
	default:
		return failCheck(checkGroupInstallSource, baseURL,
			fmt.Sprintf("%s answered HTTP %d", probe, resp.StatusCode),
			"The server answered but serves no yum metadata at this path, so Anaconda cannot install from it",
			"point installSource at the install-tree root that contains repodata/ (for example .../BaseOS/x86_64/os/)")
	}
}

func installSourceHTTPDo(deps Deps, req *http.Request) (*http.Response, error) {
	if deps.HTTPDo != nil {
		return deps.HTTPDo(req, false)
	}
	client := &http.Client{Timeout: installSourceProbeTimeout}
	return client.Do(req)
}
