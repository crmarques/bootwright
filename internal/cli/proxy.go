package cli

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

// returns nil when proxy is Bootwright-managed: Bootwright provisions that
// proxy, so bootstrap cannot route through a proxy that does not yet exist.
func resolveProxyEnv(state v1alpha1.State, secretsDir string) (map[string]string, error) {
	if proxy.IsManaged(state) {
		return nil, nil
	}
	for i := range state.Environments {
		env := state.Environments[i]
		eff := proxy.Resolve(state, &env)
		if eff == nil || (eff.HTTP == "" && eff.HTTPS == "" && len(eff.NoProxy) == 0) {
			continue
		}
		authority := ""
		if effectiveProxyEnvUsesCredentials(eff) {
			path := resolvedSecretPath(eff.Auth.Name, &env, secretsDir)
			creds, err := secret.ReadUserPasswordFile(path, "proxy credentials")
			if err != nil {
				return nil, err
			}
			authority = proxyCredentialAuthority(creds)
		}
		httpURL := injectProxyAuthority(eff.HTTP, authority)
		httpsURL := injectProxyAuthority(eff.HTTPS, authority)
		noProxy := strings.Join(eff.NoProxy, ",")
		out := map[string]string{}
		if httpURL != "" {
			out["HTTP_PROXY"] = httpURL
			out["http_proxy"] = httpURL
		}
		if httpsURL != "" {
			out["HTTPS_PROXY"] = httpsURL
			out["https_proxy"] = httpsURL
		}
		if noProxy != "" {
			out["NO_PROXY"] = noProxy
			out["no_proxy"] = noProxy
		}
		return out, nil
	}
	return nil, nil
}

func proxyEnvRequiresSecrets(state v1alpha1.State) bool {
	if proxy.IsManaged(state) {
		return false
	}
	for i := range state.Environments {
		if effectiveProxyEnvUsesCredentials(proxy.Resolve(state, &state.Environments[i])) {
			return true
		}
	}
	return false
}

func effectiveProxyEnvUsesCredentials(eff *proxy.Effective) bool {
	return eff != nil && eff.Auth.Name != "" && (eff.HTTP != "" || eff.HTTPS != "")
}

func proxyCredentialAuthority(creds secret.UserPassword) string {
	return url.UserPassword(creds.Username, creds.Password).String() + "@"
}

func ambientProxyEnv() map[string]string {
	out := map[string]string{}
	for _, key := range proxyEnvKeys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var proxyEnvKeys = []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

var proxySchemeAuthorityRE = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.\-]*://)([^@/]+@)?`)

func injectProxyAuthority(rawURL, authority string) string {
	if rawURL == "" || authority == "" {
		return rawURL
	}
	match := proxySchemeAuthorityRE.FindStringSubmatchIndex(rawURL)
	if len(match) < 4 {
		return rawURL
	}
	return rawURL[match[2]:match[3]] + authority + rawURL[match[1]:]
}

func proxySummary(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	if v := firstProxyEnv(env, "HTTPS_PROXY", "https_proxy"); v != "" {
		parts = append(parts, "https")
	}
	if v := firstProxyEnv(env, "HTTP_PROXY", "http_proxy"); v != "" {
		parts = append(parts, "http")
	}
	if v := firstProxyEnv(env, "NO_PROXY", "no_proxy"); v != "" {
		parts = append(parts, fmt.Sprintf("no_proxy: %d entries", noProxyEntryCount(v)))
	}
	return "configured (" + strings.Join(parts, ", ") + ")"
}

func noProxyEntryCount(value string) int {
	count := 0
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func firstProxyEnv(env map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := env[key]; v != "" {
			return v
		}
	}
	return ""
}
