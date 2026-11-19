package infrastructureservices

import (
	"net/netip"
	"net/url"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const MaxAcquisitionBypassEntries = 128

type ProxyBypassRule struct {
	all        bool
	host       string
	port       string
	subdomains bool
	prefix     netip.Prefix
}

func ParseProxyBypass(value string) (ProxyBypassRule, bool) {
	if !api.ValidLexical(api.LexicalProxyBypass, value) {
		return ProxyBypassRule{}, false
	}
	if value == "*" {
		return ProxyBypassRule{all: true}, true
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return ProxyBypassRule{prefix: prefix.Masked()}, true
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return ProxyBypassRule{host: address.String()}, true
	}
	rule := ProxyBypassRule{}
	if strings.Contains(value, ":") {
		value, rule.port = splitBypassPort(value)
	}
	value = strings.ToLower(value)
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*")
	}
	rule.subdomains = strings.HasPrefix(value, ".")
	rule.host = strings.TrimPrefix(value, ".")
	return rule, true
}

func splitBypassPort(value string) (string, string) {
	if bracketed, ok := strings.CutPrefix(value, "["); ok {
		host, port, _ := strings.Cut(bracketed, "]:")
		return host, port
	}
	host, port, _ := strings.Cut(value, ":")
	return host, port
}

func (r ProxyBypassRule) PortQualified() bool { return r.port != "" }

func (r ProxyBypassRule) Matches(target *url.URL) bool {
	if r.all {
		return true
	}
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	if port == "" {
		port = "443"
		if target.Scheme == "http" {
			port = "80"
		}
	}
	if r.port != "" && r.port != port {
		return false
	}
	if r.prefix.IsValid() {
		address, err := netip.ParseAddr(host)
		return err == nil && r.prefix.Contains(address)
	}
	if address, err := netip.ParseAddr(host); err == nil {
		expected, err := netip.ParseAddr(r.host)
		return err == nil && address.Unmap() == expected.Unmap()
	}
	return !r.subdomains && host == r.host || strings.HasSuffix(host, "."+r.host)
}
