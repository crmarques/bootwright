package preflight

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/workspace"
)

const preflightHTTPProbeTimeout = 15 * time.Second

func stateNeedsVSphere(state v1alpha1.State) bool {
	providers := map[string]v1alpha1.InfraProvider{}
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	for _, machine := range state.Machines {
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere {
			return true
		}
	}
	return false
}

func vspherePyvmomiCheck(deps Deps) Check {
	name := "pyvmomi (vCenter SDK)"
	venvPython := filepath.Join(workspace.AnsibleVenvDir(), "bin", "python")
	out, err := deps.CommandOutputLocalRoot(venvPython, "-c", "import pyVmomi")
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		return failCheck(checkGroupInstallerTools, name, evidence, "vSphere machine creation drives vCenter through pyvmomi-backed Ansible modules", "bootwright bastion setup")
	}
	return okCheck(checkGroupInstallerTools, name, venvPython+" imports pyVmomi")
}

func vsphereVCenterChecks(state v1alpha1.State, selected []Phase, contextName, secretsDir string, deps Deps) []Check {
	if !anyPhaseInScope([]string{"machines", "base"}, selected) || !stateNeedsVSphere(state) {
		return nil
	}
	resolver := secret.NewResolver(contextName, secretsDir, secret.NewIndex(state))
	seen := map[string]bool{}
	var vCenters []v1alpha1.VSphereVCenter
	for _, p := range state.InfraProviders {
		if p.Spec.Type != v1alpha1.ProvisionerVSphere || p.Spec.VSphere == nil {
			continue
		}
		for _, vc := range p.Spec.VSphere.VCenters {
			key := fmt.Sprintf("%s|%d|%s", vc.Server, vc.Port, vc.CredentialsRef.Name)
			if vc.Server == "" || seen[key] {
				continue
			}
			seen[key] = true
			vCenters = append(vCenters, vc)
		}
	}
	if len(vCenters) == 0 {
		return nil
	}
	sessions := make([]Check, len(vCenters))
	forEachBounded(len(vCenters), vCenterParallelism, func(i int) {
		sessions[i] = vsphereSessionCheck(vCenters[i], resolver, deps)
	})
	var checks []Check
	for i, vc := range vCenters {
		if vc.DisableCertificateVerification {
			checks = append(checks, Check{
				Group:       checkGroupInstallerTools,
				Name:        vc.Server + " vCenter TLS",
				Status:      StatusWarn,
				Evidence:    "disableCertificateVerification is set for " + vc.Server,
				Impact:      "the session probe (and apply) send vCenter basic-auth credentials over unverified TLS, exposing them to a man-in-the-middle on the management segment",
				Remediation: "add the vCenter CA to the provider trust and clear disableCertificateVerification outside self-signed labs",
			})
		}
		checks = append(checks, sessions[i])
	}
	return checks
}

func vsphereSessionCheck(vc v1alpha1.VSphereVCenter, resolver secret.Resolver, deps Deps) Check {
	name := vc.Server + " vCenter session"
	creds, err := resolver.ReadUserPasswordMaterial(vc.CredentialsRef.Name, secret.MaterialPrimary, "vCenter credentials")
	if err != nil {
		return Check{
			Group:    checkGroupInstallerTools,
			Name:     name,
			Status:   StatusWarn,
			Evidence: "session probe skipped: " + err.Error(),
			Impact:   "vCenter reachability and credentials were not verified",
		}
	}
	port := vc.Port
	if port == 0 {
		port = 443
	}
	url := fmt.Sprintf("https://%s:%d/api/session", vc.Server, port)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return failCheck(checkGroupInstallerTools, name, err.Error(), "vSphere machine creation needs a reachable vCenter API", "check the declared vcenters[].server and port")
	}
	req.SetBasicAuth(creds.Username, creds.Password)
	resp, err := preflightHTTPDo(deps, req, vc.DisableCertificateVerification)
	if err != nil {
		return failCheck(checkGroupInstallerTools, name, err.Error(), "vSphere machine creation needs a reachable vCenter API", "check network reachability, DNS, and TLS trust for "+vc.Server+" (disableCertificateVerification opts out of verification for self-signed labs)")
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		releaseVSphereSession(deps, url, vsphereSessionID(resp), vc.DisableCertificateVerification)
		return okCheck(checkGroupInstallerTools, name, fmt.Sprintf("%s answered HTTP %d", url, resp.StatusCode))
	case resp.StatusCode == http.StatusUnauthorized:
		return failCheck(checkGroupInstallerTools, name, fmt.Sprintf("%s answered HTTP 401", url), "vCenter rejected the declared credentials", "update the "+vc.CredentialsRef.Name+" secret (user:password) with bootwright secret set --name "+vc.CredentialsRef.Name)
	default:
		return failCheck(checkGroupInstallerTools, name, fmt.Sprintf("%s answered HTTP %d", url, resp.StatusCode), "vSphere machine creation needs a working vCenter session API", "check the vCenter endpoint health for "+vc.Server)
	}
}

func vsphereSessionID(resp *http.Response) string {
	if id := resp.Header.Get("vmware-api-session-id"); id != "" {
		return id
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(body)), `"`)
}

func releaseVSphereSession(deps Deps, url, sessionID string, insecureSkipVerify bool) {
	if sessionID == "" {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("vmware-api-session-id", sessionID)
	resp, err := preflightHTTPDo(deps, req, insecureSkipVerify)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func preflightHTTPDo(deps Deps, req *http.Request, insecureSkipVerify bool) (*http.Response, error) {
	if deps.HTTPDo != nil {
		return deps.HTTPDo(req, insecureSkipVerify)
	}
	client := &http.Client{Timeout: preflightHTTPProbeTimeout}
	if insecureSkipVerify {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return client.Do(req)
}
