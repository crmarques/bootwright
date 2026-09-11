package bundlelocal

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type sourceFetcher func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error)

// Acquirer provides the same bounded explicit-route HTTPS transport to the
// native installer. Only a source exactly present in the compiled catalog can
// be acquired; the returned bytes already satisfy its size and SHA-256 lock.
type Acquirer struct{}

func (Acquirer) Acquire(ctx context.Context, source prerequisites.DependencySource, egress prerequisites.SetupEgress) ([]byte, error) {
	record, _, err := compiledCatalog()
	if err != nil {
		return nil, err
	}
	approved := false
	for _, candidate := range record.Baseline {
		approved = approved || candidate == source
	}
	for _, native := range record.Native {
		for _, candidate := range native.Packages {
			approved = approved || candidate.Source == source
		}
	}
	if !approved {
		return nil, bundleFailure("dependency acquisition source differs from the compiled catalog")
	}
	return fetchSource(ctx, source, egress)
}

func fetchSource(ctx context.Context, source prerequisites.DependencySource, egress prerequisites.SetupEgress) ([]byte, error) {
	if source.Bytes <= 0 || source.Bytes > maxMemberBytes {
		return nil, bundleFailure("dependency source exceeds its qualified acquisition limit")
	}
	origin, err := url.Parse(source.URL)
	if err != nil || !approvedOrigin(origin) {
		return nil, bundleFailure("dependency source is outside the approved HTTPS origins")
	}
	client, closeClient, err := acquisitionClient(ctx, egress, approvedOrigin, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	return receiveSource(ctx, source, client.Do)
}

// acquisitionClient shares the fixed explicit-route transport with the
// publisher metadata resolvers. Each consumer supplies its narrower origin
// boundary and separately enforces response bytes and content integrity.
func acquisitionClient(ctx context.Context, egress prerequisites.SetupEgress, approved func(*url.URL) bool, timeout time.Duration) (*http.Client, func(), error) {
	if approved == nil || timeout <= 0 || timeout > 5*time.Minute {
		return nil, nil, bundleFailure("dependency acquisition transport policy is invalid")
	}
	proxy, err := explicitProxy(egress)
	if err != nil {
		return nil, nil, err
	}
	roots, err := qualifiedSystemRoots(ctx)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		Proxy: proxy, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10, DisableCompression: true,
	}
	client := &http.Client{
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 5 || !approved(request.URL) {
				return bundleFailure("dependency acquisition redirected outside its approved HTTPS boundary")
			}
			return nil
		},
	}
	return client, transport.CloseIdleConnections, nil
}

func receiveSource(ctx context.Context, source prerequisites.DependencySource, send func(*http.Request) (*http.Response, error)) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, bundleFailure("dependency acquisition request is invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Bootwright-Controller/1")
	response, err := send(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, bundleFailure("approved dependency source could not be acquired")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > source.Bytes || response.Header.Get("Content-Encoding") != "" {
		return nil, bundleFailure("dependency acquisition returned an unapproved status or representation")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, source.Bytes+1))
	if err != nil || !approvedBytes(source, data) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, bundleFailure("acquired dependency does not match its approved size and SHA-256")
	}
	return data, nil
}

func approvedOrigin(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Fragment != "" || value.Opaque != "" || value.Port() != "" && value.Port() != "443" {
		return false
	}
	switch value.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "files.pythonhosted.org", "cdn-ubi.redhat.com", "dl.fedoraproject.org":
		return true
	}
	return false
}

// Proxy selection is request-local and never reads HTTP_PROXY, HTTPS_PROXY,
// ALL_PROXY or NO_PROXY. DNS is not consulted to evaluate bypass entries.
func explicitProxy(egress prerequisites.SetupEgress) (func(*http.Request) (*url.URL, error), error) {
	var selected *url.URL
	if egress.HTTPSProxy != "" {
		var err error
		selected, err = url.Parse(egress.HTTPSProxy)
		if err != nil || selected.Hostname() == "" || selected.User != nil || selected.Fragment != "" || selected.RawQuery != "" ||
			selected.Opaque != "" || selected.Path != "" && selected.Path != "/" || selected.Scheme != "http" && selected.Scheme != "https" {
			return nil, bundleFailure("explicit HTTPS acquisition proxy is invalid")
		}
	} else if egress.HTTPProxy != "" {
		return nil, bundleFailure("HTTPS dependency acquisition requires an explicit HTTPS proxy endpoint")
	}
	bypasses := make([]bypassRule, len(egress.NoProxy))
	for index, value := range egress.NoProxy {
		rule, ok := parseBypass(value)
		if !ok {
			return nil, bundleFailure("proxy bypass entry is outside the qualified host, IP or CIDR grammar")
		}
		bypasses[index] = rule
	}
	return func(request *http.Request) (*url.URL, error) {
		for _, rule := range bypasses {
			if rule.matches(request.URL) {
				return nil, nil
			}
		}
		return selected, nil
	}, nil
}

type bypassRule struct {
	all        bool
	host       string
	port       string
	subdomains bool
	prefix     netip.Prefix
}

func parseBypass(value string) (bypassRule, bool) {
	if value == "*" {
		return bypassRule{all: true}, true
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return bypassRule{prefix: prefix.Masked()}, true
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return bypassRule{host: address.String()}, true
	}
	rule := bypassRule{}
	if strings.Contains(value, ":") {
		var err error
		value, rule.port, err = net.SplitHostPort(value)
		if err != nil || rule.port == "" {
			return bypassRule{}, false
		}
		for _, c := range rule.port {
			if c < '0' || c > '9' {
				return bypassRule{}, false
			}
		}
	}
	value = strings.ToLower(value)
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*")
	}
	rule.subdomains = strings.HasPrefix(value, ".")
	value = strings.TrimPrefix(value, ".")
	if value == "" || strings.HasSuffix(value, ".") || strings.ContainsAny(value, "/\\@?#\x00\r\n\t ") {
		return bypassRule{}, false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':') {
			return bypassRule{}, false
		}
	}
	rule.host = value
	return rule, true
}

func (r bypassRule) matches(target *url.URL) bool {
	if r.all {
		return true
	}
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	if port == "" {
		port = "443"
	}
	if r.port != "" && r.port != port {
		return false
	}
	if r.prefix.IsValid() {
		address, err := netip.ParseAddr(host)
		return err == nil && r.prefix.Contains(address)
	}
	return !r.subdomains && host == r.host || strings.HasSuffix(host, "."+r.host)
}
