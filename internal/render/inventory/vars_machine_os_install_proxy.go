package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

// managedOSInstallProxyVars projects the already-resolved machineOSInstall
// proxy into the kickstart proxy inputs (its unauthenticated url plus, when
// present, the credentials-file path). A nil eff — no proxy, or a managed/unset
// selection that ResolveFor rejected — emits nothing. The credentialed proxy URL
// is assembled in the kickstart from this url plus the credentials file, keeping
// the proxy password out of vars.yaml the same way rhsm secrets stay out of it.
// Whether each install directive actually uses the proxy is decided per target
// by installTargetProxied, not here.
func managedOSInstallProxyVars(eff *proxy.Effective, state v1alpha1.State, secretsDir string) map[string]any {
	if eff == nil {
		return nil
	}
	url := eff.HTTPS
	if url == "" {
		url = eff.HTTP
	}
	if url == "" {
		return nil
	}
	out := map[string]any{"url": url}
	if eff.Auth.Name != "" {
		if path := secret.ResolveMaterialPath(eff.Auth.Name, secret.NewIndex(state), secretsDir, secret.MaterialPrimary); path != "" {
			out["credentialsPath"] = path
		}
	}
	return out
}

// installTargetProxied reports whether an install-time fetch to host should go
// through the machineOSInstall proxy: only when a proxy applies (eff != nil) and
// the host is not bypassed by no_proxy. An empty host (the public Red Hat CDN
// case for rhsm) is never bypassed, so it is proxied whenever a proxy applies.
// Anaconda has no no_proxy directive, so this per-target decision is what makes
// the kickstart honour noProxy for the rhsm/url/repo fetches.
func installTargetProxied(eff *proxy.Effective, host string) bool {
	return eff != nil && !proxy.Bypasses(eff, host)
}

// rhsmRegistrationHost returns the host the rhsm command registers against: the
// Satellite hostname when a Satellite redirect is set, otherwise "" for the
// public Red Hat CDN.
func rhsmRegistrationHost(rhsm map[string]any) string {
	if satellite, ok := rhsm["satellite"].(map[string]any); ok {
		if hostname, ok := satellite["hostname"].(string); ok {
			return hostname
		}
	}
	return ""
}
