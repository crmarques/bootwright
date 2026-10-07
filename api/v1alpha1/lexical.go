package v1alpha1

import (
	"net"
	"net/netip"
	neturl "net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var userPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]*[$]?$`)
var digestPattern = regexp.MustCompile(`(?i)^(?:sha256:)?[a-f0-9]{64}$`)
var imageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var cliVersionPattern = regexp.MustCompile(`^(?:latest|v?(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8}))$`)
var packageVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~^:-]{0,95}$`)

// ociPathComponentPattern is one repository path component of the OCI
// distribution grammar, outside of which container runtimes refuse to pull.
var ociPathComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*$`)

// devicePathPattern confines a device path to characters that neither an
// installer directive nor a shell word can reinterpret, because the path
// reaches both verbatim.
var devicePathPattern = regexp.MustCompile(`^/dev/[A-Za-z0-9._:+/-]+$`)

// systemdUnitPattern is systemd's unit-name alphabet and 255-byte bound
// without the backslash of its escapes, which a Kickstart line would consume.
// packageSpecPattern admits a name, a version, a glob, an @group, an
// @^environment and an @module:stream/profile, and nothing that opens a
// Kickstart section, excludes a package or ends the line. interfacePattern is
// a Linux interface name within IFNAMSIZ. bridgePattern is that name without
// the `+` firewalld reads as an interface wildcard, since a managed network
// puts its bridge in a firewalld zone by name.
var systemdUnitPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,254}$`)
var packageSpecPattern = regexp.MustCompile(`^[A-Za-z0-9_@*][A-Za-z0-9_.+*?@:~^/\[\]-]*$`)
var interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,15}$`)
var bridgePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

// uriExcluded are the printable ASCII characters RFC 3986 admits nowhere in a
// URI. kickstartSeparators are those a Kickstart line reads as a quote, an
// escape, a comment or a list separator.
const (
	uriExcluded         = "\"<>\\^`{|}"
	kickstartSeparators = "\"'\\#,"
)

func ValidLexical(rule, value string) bool {
	if rule == "checksum" {
		return digestPattern.MatchString(strings.TrimSpace(value))
	}
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	switch rule {
	case "name":
		return labelPattern.MatchString(value)
	case "dns":
		return validDNS(value)
	case "host":
		return validHost(value)
	case "ip":
		_, err := netip.ParseAddr(value)
		return err == nil && !strings.Contains(value, "%")
	case "cidr":
		_, err := netip.ParsePrefix(value)
		return err == nil
	case "address":
		if p, err := netip.ParsePrefix(value); err == nil {
			return p.Bits() > 0
		}
		return validHost(value)
	case "http-url", "https-url":
		return validHTTPURL(value, rule == "https-url")
	case "repository-url":
		return validHTTPURL(value, false) && !strings.ContainsAny(value, "#'")
	case "mirror-url":
		return validMirrorURL(value)
	case "proxy-endpoint":
		return validProxyEndpoint(value)
	case "proxy-bypass":
		return validProxyBypass(value)
	case "systemd-unit":
		return systemdUnitPattern.MatchString(value)
	case "package-spec":
		return packageSpecPattern.MatchString(value)
	case "kickstart-token":
		return !strings.HasPrefix(value, "%") && printableExcept(value, kickstartSeparators)
	case "ifname":
		return interfacePattern.MatchString(value) && value != "." && value != ".."
	case "bridge":
		return bridgePattern.MatchString(value) && value != "." && value != ".."
	case "registry", "registry-base":
		return validRegistry(value)
	case "image":
		return validImage(value)
	case "image-version":
		return value != "latest" && (imageTagPattern.MatchString(value) || strings.HasPrefix(strings.ToLower(value), "sha256:") && digestPattern.MatchString(value))
	case "cli-version":
		return cliVersionPattern.MatchString(value)
	case "package-version":
		return packageVersionPattern.MatchString(value)
	case "duration":
		duration, err := time.ParseDuration(value)
		return err == nil && duration > 0
	case "mac":
		mac, err := net.ParseMAC(value)
		return err == nil && len(mac) == 6
	case "absolute-path":
		return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
	case "device-path":
		return devicePathPattern.MatchString(value) && path.Clean(value) == value
	case "relative-path":
		return !strings.HasPrefix(value, "/") && value != "." && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 0)
	case "token":
		return tokenPattern.MatchString(value)
	case "posix-user":
		return userPattern.MatchString(value)
	}
	return false
}

func validDNS(value string) bool {
	if len(value) > 253 || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func validHost(value string) bool {
	if ip, err := netip.ParseAddr(value); err == nil {
		return ip.Zone() == ""
	}
	return validDNS(value)
}

// validHTTPURL is an absolute http or https URL with a DNS or IP host, an
// optional port and no userinfo, written only in RFC 3986's own characters so
// that no whitespace, control or non-ASCII character reaches a consumer.
func validHTTPURL(value string, secure bool) bool {
	if !printableExcept(value, uriExcluded) {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.Opaque == "" && u.User == nil && u.Host != "" && validHost(u.Hostname()) && (u.Scheme == "https" || !secure && u.Scheme == "http") && validURLPort(u)
}

// maxMirrorURLBytes bounds a download mirror, which a frozen controller
// request carries.
const maxMirrorURLBytes = 4096

// validMirrorURL is the one grammar of a download mirror, shared by admission
// and the controller stage that fetches beneath it: an HTTPS base URL on the
// default port with an unescaped path, no query and no fragment, because the
// stage appends a release path to it.
func validMirrorURL(value string) bool {
	if len(value) > maxMirrorURLBytes || strings.ContainsAny(value, "?#") || !validHTTPURL(value, true) {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.RawPath == "" && (u.Port() == "" || u.Port() == "443")
}

// maxProxyEndpointBytes bounds a proxy endpoint, which a setup receipt and a
// frozen controller request carry.
const maxProxyEndpointBytes = 4096

// validProxyEndpoint is the one grammar of a proxy endpoint an acquisition
// route takes, declared or read from the environment: a bounded ASCII http or
// https URL with a host and nothing after it, so it carries no credential.
func validProxyEndpoint(value string) bool {
	if len(value) > maxProxyEndpointBytes || !printableExcept(value, "") {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Opaque == "" &&
		(u.Path == "" || u.Path == "/") && (u.Scheme == "http" || u.Scheme == "https")
}

// maxProxyBypassBytes bounds one proxy bypass entry, which a setup receipt
// and the controller stage's acquisition each refuse past it.
const maxProxyBypassBytes = 1024

// ProxyBypassForms names every accepted form of a proxy bypass entry, as a
// remedy writes it.
const ProxyBypassForms = "*, a host name, a .domain or *.domain suffix, an IP address or a CIDR block, each name or address optionally with :port, in at most 1024 bytes"

// ProxyEndpointForm names the accepted form of a proxy endpoint, as a remedy
// writes it.
const ProxyEndpointForm = "a bare http or https endpoint such as http://proxy.example.test:3128, with no userinfo, path, query or fragment"

// validProxyBypass is the one grammar of a proxy bypass entry: "*", a CIDR
// block, an IP address, or a host name or domain suffix, written as *.domain
// or .domain, with an optional port, an IPv6 host bracketed before one.
func validProxyBypass(value string) bool {
	if len(value) > maxProxyBypassBytes {
		return false
	}
	if value == "*" {
		return true
	}
	if _, err := netip.ParsePrefix(value); err == nil {
		return true
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return true
	}
	if strings.Contains(value, ":") {
		host, ok := proxyBypassHost(value)
		if !ok {
			return false
		}
		value = host
	}
	value = strings.ToLower(value)
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*")
	}
	value = strings.TrimPrefix(value, ".")
	if value == "" || strings.HasSuffix(value, ".") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

// proxyBypassHost is the host of a bypass entry that names a port, which is
// all digits after the one colon of a host or the bracket of an IPv6 host.
func proxyBypassHost(value string) (string, bool) {
	host, port := "", ""
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end < 0 || !strings.HasPrefix(value[end+1:], ":") {
			return "", false
		}
		host, port = value[1:end], value[end+2:]
		if strings.ContainsAny(host, "[]") {
			return "", false
		}
	} else {
		separator := strings.Index(value, ":")
		if separator != strings.LastIndex(value, ":") {
			return "", false
		}
		host, port = value[:separator], value[separator+1:]
	}
	if port == "" {
		return "", false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return host, true
}

// printableExcept holds when every byte is printable ASCII other than space
// and none is in excluded.
func printableExcept(value, excluded string) bool {
	for index := range len(value) {
		if b := value[index]; b < 0x21 || b > 0x7e || strings.IndexByte(excluded, b) >= 0 {
			return false
		}
	}
	return true
}

func validURLPort(u *neturl.URL) bool {
	if u.Port() == "" {
		return !strings.HasSuffix(u.Host, ":")
	}
	n := IntegerValue(u.Port())
	port, ok := n.Int64()
	return ok && port > 0 && port <= 65535
}

func validRegistry(value string) bool {
	if strings.ContainsAny(value, "?#@\\\x00 \t\r\n") || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	u, err := neturl.Parse("https://" + value)
	if err != nil || u.User != nil || !validHost(u.Hostname()) || !validURLPort(u) {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/") {
		if u.Path == "" {
			break
		}
		if !ociPathComponentPattern.MatchString(part) {
			return false
		}
	}
	return true
}

// CanonicalImage writes an image reference's digest algorithm and hex in
// lowercase and leaves everything else, a tag included, as written.
func CanonicalImage(value string) string {
	base, digest, found := strings.Cut(value, "@")
	if !found {
		return value
	}
	return base + "@" + strings.ToLower(digest)
}

func validImage(value string) bool {
	if base, digest, found := strings.Cut(value, "@"); found {
		return validRegistry(base) && strings.HasPrefix(strings.ToLower(digest), "sha256:") && digestPattern.MatchString(digest)
	}
	lastSlash := strings.LastIndex(value, "/")
	colon := strings.LastIndex(value, ":")
	if colon <= lastSlash || colon <= 0 {
		return false
	}
	return validRegistry(value[:colon]) && value[colon+1:] != "latest" && imageTagPattern.MatchString(value[colon+1:])
}
