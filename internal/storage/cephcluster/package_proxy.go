package cephcluster

import (
	"net/url"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func (a *admission) packageProxy(spec api.Value) {
	const field = "$.spec.packageRepositoryProxy"
	choice := spec.Get("packageRepositoryProxy")
	a.issues = api.AppendIssues(a.issues, infrastructureservices.ValidateProxy(choice, a.catalog, field, false)...)
	if !choice.Has("proxyRef") {
		return
	}
	proxy, found := a.catalog.Find(api.Proxy, choice.Get("proxyRef").Text())
	if !found {
		return
	}
	connection := proxy.Spec().Get("connection")
	endpoint := connection.Get("httpsProxy").Text()
	if proxy.Spec().Get("management").Text() != "external" ||
		!validPackageProxyEndpoint(endpoint) ||
		connection.Has("auth") || connection.Has("trustBundleRef") ||
		connection.Has("tlsVerification") && connection.Get("tlsVerification").Text() != "full" || choice.Get("noProxy").Len() > 128 {
		a.issue("api.invariant", field, "Ceph package acquisition supports only an external unauthenticated HTTP CONNECT Proxy with at most 128 bypass entries",
			"select direct: {} or an external Proxy whose connection.httpsProxy is an http:// endpoint, without auth, trustBundleRef or relaxed TLS verification")
	}
}

func validPackageProxyEndpoint(endpoint string) bool {
	if !api.ValidLexical(api.LexicalProxyEndpoint, endpoint) || strings.ContainsAny(endpoint, "%\\") {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		return err == nil && port > 0 && port <= 65535
	}
	return true
}
