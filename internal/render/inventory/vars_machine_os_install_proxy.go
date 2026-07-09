package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

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

func installTargetProxied(eff *proxy.Effective, host string) bool {
	return eff != nil && !proxy.Bypasses(eff, host)
}

func rhsmRegistrationHost(rhsm map[string]any) string {
	if satellite, ok := rhsm["satellite"].(map[string]any); ok {
		if hostname, ok := satellite["hostname"].(string); ok {
			return hostname
		}
	}
	return ""
}
