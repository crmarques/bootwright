package controller

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ProxyEnvironmentNames are the only variables a context-free acquisition reads,
// in the canonical case the elevated child receives. ALL_PROXY is not among them:
// every approved acquisition is HTTPS and no SOCKS route is qualified.
var ProxyEnvironmentNames = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY"}

var (
	ErrProxyEndpoint = errors.New("proxy endpoint is outside the qualified URL grammar")
	ErrProxyScheme   = errors.New("HTTPS acquisition requires an HTTPS proxy endpoint")
	ErrProxyBypass   = errors.New("proxy bypass entry is outside the qualified host, IP or CIDR grammar")
)

const maxProxyBypassEntries = 128

// ProxySelector answers the route one exact target takes. A nil result is
// direct access. DNS is not consulted to evaluate bypass entries.
type ProxySelector func(target *url.URL) *url.URL

func NewProxySelector(httpProxy, httpsProxy string, bypass []string) (ProxySelector, error) {
	var selected *url.URL
	if httpsProxy != "" {
		endpoint, err := parseProxyEndpoint(httpsProxy)
		if err != nil {
			return nil, err
		}
		selected = endpoint
	} else if httpProxy != "" {
		return nil, ErrProxyScheme
	}
	if len(bypass) > maxProxyBypassEntries {
		return nil, ErrProxyBypass
	}
	rules := make([]bypassRule, len(bypass))
	for index, value := range bypass {
		rule, ok := parseBypass(value)
		if !ok {
			return nil, ErrProxyBypass
		}
		rules[index] = rule
	}
	return func(target *url.URL) *url.URL {
		if target == nil {
			return nil
		}
		for _, rule := range rules {
			if rule.matches(target) {
				return nil
			}
		}
		return selected
	}, nil
}

// parseProxyEndpoint admits exactly the proxy-endpoint grammar admission and
// selection check, so a route never refuses an endpoint either admitted.
func parseProxyEndpoint(value string) (*url.URL, error) {
	if !api.ValidLexical("proxy-endpoint", value) {
		return nil, ErrProxyEndpoint
	}
	endpoint, err := url.Parse(value)
	if err != nil {
		return nil, ErrProxyEndpoint
	}
	return endpoint, nil
}

// Selector builds this route's target selection. A route selected from desired
// state and one read from the operator's environment share one grammar.
func (r Route) Selector() (ProxySelector, error) {
	return NewProxySelector(r.httpProxy, r.httpsProxy, r.noProxy)
}

// Configured reports whether a route was selected at all. The zero value is not
// a decision, so a consumer holding one keeps its own default.
func (r Route) Configured() bool { return r.direct || r.httpProxy != "" || r.httpsProxy != "" }

// Origin names what selected this route, so a report says why acquisition takes
// it: an environment variable, or the Proxy one context's Machine chose.
func (r Route) Origin() string {
	if r.origin != "" {
		return r.origin
	}
	if r.proxyName != "" {
		return "Proxy " + r.proxyName
	}
	return ""
}

func (r Route) Summary() string {
	endpoint := r.httpsProxy
	if endpoint == "" {
		endpoint = r.httpProxy
	}
	if endpoint == "" {
		return "direct"
	}
	summary := endpoint
	if origin := r.Origin(); origin != "" {
		summary += " (" + origin + ")"
	}
	switch len(r.noProxy) {
	case 0:
	case 1:
		summary += ", 1 bypass entry"
	default:
		summary += ", " + strconv.Itoa(len(r.noProxy)) + " bypass entries"
	}
	return summary
}

// RouteFromEnvironment selects the acquisition route for the commands that run
// before any context exists. It reads exactly ProxyEnvironmentNames in either
// case, and an unset environment keeps direct access.
func RouteFromEnvironment(lookup func(string) (string, bool)) (Route, error) {
	if lookup == nil {
		return Route{direct: true}, nil
	}
	values := map[string]string{}
	for _, name := range ProxyEnvironmentNames {
		value, err := ambientValue(lookup, name)
		if err != nil {
			return Route{}, err
		}
		values[name] = value
	}
	if values["HTTPS_PROXY"] == "" {
		if values["HTTP_PROXY"] != "" {
			return Route{}, routeFailure("every dependency source is HTTPS, so HTTP_PROXY alone selects no acquisition route", "Set HTTPS_PROXY to the proxy endpoint, or unset HTTP_PROXY for direct access.")
		}
		return Route{direct: true}, nil
	}
	route := Route{origin: "HTTPS_PROXY", httpsProxy: values["HTTPS_PROXY"]}
	bypass, err := parseBypassList(values["NO_PROXY"])
	if err != nil {
		return Route{}, err
	}
	route.noProxy = bypass
	if _, err := route.Selector(); err != nil {
		return Route{}, environmentFault(err)
	}
	return route, nil
}

// ambientValue refuses a name whose two spellings disagree rather than choosing
// one, because either choice silently discards an operator's explicit value.
func ambientValue(lookup func(string) (string, bool), name string) (string, error) {
	upper, _ := lookup(name)
	lower, _ := lookup(strings.ToLower(name))
	upper, lower = strings.TrimSpace(upper), strings.TrimSpace(lower)
	if upper != "" && lower != "" && upper != lower {
		return "", routeFailure(name+" and "+strings.ToLower(name)+" are both set to different values", "Set one spelling of "+name+", or set both to the same value.")
	}
	if upper != "" {
		return upper, nil
	}
	return lower, nil
}

func parseBypassList(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	entries := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, entry)
	}
	if len(entries) > maxProxyBypassEntries {
		return nil, routeFailure("NO_PROXY carries more bypass entries than acquisition admits", "Reduce NO_PROXY to at most "+strconv.Itoa(maxProxyBypassEntries)+" entries.")
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return entries, nil
}

func environmentFault(err error) error {
	switch {
	case errors.Is(err, ErrProxyBypass):
		return routeFailure("a NO_PROXY entry is outside the qualified host, IP or CIDR grammar", "Use host names, domain suffixes, IP addresses or CIDR blocks of at most 1024 bytes each in NO_PROXY.")
	case errors.Is(err, ErrProxyScheme):
		return routeFailure("every dependency source is HTTPS, so HTTP_PROXY alone selects no acquisition route", "Set HTTPS_PROXY to the proxy endpoint, or unset HTTP_PROXY for direct access.")
	default:
		return routeFailure("HTTPS_PROXY is not an absolute HTTP or HTTPS endpoint without credentials", "Set HTTPS_PROXY to a credential-free endpoint such as http://proxy.example:3128.")
	}
}

func routeFailure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("controller.unsupported", message, "", remediation)
}

type bypassRule struct {
	all        bool
	host       string
	port       string
	subdomains bool
	prefix     netip.Prefix
}

// parseBypass builds the rule of an entry in the proxy-bypass grammar
// admission and selection check; it refuses whatever that grammar refuses and
// nothing else.
func parseBypass(value string) (bypassRule, bool) {
	if !api.ValidLexical("proxy-bypass", value) {
		return bypassRule{}, false
	}
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

// splitBypassPort splits an admitted entry into its host and port.
func splitBypassPort(value string) (string, string) {
	if bracketed, ok := strings.CutPrefix(value, "["); ok {
		host, port, _ := strings.Cut(bracketed, "]:")
		return host, port
	}
	host, port, _ := strings.Cut(value, ":")
	return host, port
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
