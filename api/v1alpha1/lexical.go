package v1alpha1

import (
	"net"
	"net/netip"
	neturl "net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
var kubernetesLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[-_.A-Za-z0-9]{0,61}[A-Za-z0-9])?$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var userPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]*[$]?$`)
var digestPattern = regexp.MustCompile(`(?i)^(?:sha256:)?[a-f0-9]{64}$`)
var imageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var cliVersionPattern = regexp.MustCompile(`^v?[0-9][A-Za-z0-9._+-]*$`)
var clusterReleasePattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9][A-Za-z0-9._+-]*)?$`)
var packageVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~^:-]{0,95}$`)
var ldapAttributePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
var osReleasePattern = regexp.MustCompile(`^[1-9][0-9]*\.(?:0|[1-9][0-9]*)$`)

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
var cephHostLabelPattern = regexp.MustCompile(`^(?:_admin|[A-Za-z0-9][A-Za-z0-9_.-]{0,127})$`)

// uriExcluded are the printable ASCII characters RFC 3986 admits nowhere in a
// URI. kickstartSeparators are those a Kickstart line reads as a quote, an
// escape, a comment or a list separator.
const (
	uriExcluded         = "\"<>\\^`{|}"
	kickstartSeparators = "\"'\\#,"
)

type LexicalRule string

const (
	LexicalKubernetesLabelKey   LexicalRule = "kubernetes-label-key"
	LexicalKubernetesLabelValue LexicalRule = "kubernetes-label-value"
	LexicalName                 LexicalRule = "name"
	LexicalDNS                  LexicalRule = "dns"
	LexicalHost                 LexicalRule = "host"
	LexicalIP                   LexicalRule = "ip"
	LexicalCIDR                 LexicalRule = "cidr"
	LexicalHTTPURL              LexicalRule = "http-url"
	LexicalHTTPSURL             LexicalRule = "https-url"
	LexicalRepositoryURL        LexicalRule = "repository-url"
	LexicalMirrorURL            LexicalRule = "mirror-url"
	LexicalArtifactMirrorURL    LexicalRule = "artifact-mirror-url"
	LexicalProxyEndpoint        LexicalRule = "proxy-endpoint"
	LexicalProxyBypass          LexicalRule = "proxy-bypass"
	LexicalProxyDestination     LexicalRule = "proxy-destination"
	LexicalSystemdUnit          LexicalRule = "systemd-unit"
	LexicalPackageSpec          LexicalRule = "package-spec"
	LexicalKickstartToken       LexicalRule = "kickstart-token"
	LexicalIfname               LexicalRule = "ifname"
	LexicalCephHostLabel        LexicalRule = "ceph-host-label"
	LexicalBridge               LexicalRule = "bridge"
	LexicalRegistry             LexicalRule = "registry"
	LexicalRegistryBase         LexicalRule = "registry-base"
	LexicalImage                LexicalRule = "image"
	LexicalPackageRepository    LexicalRule = "package-repository"
	LexicalPackageRepositoryKey LexicalRule = "package-repository-key"
	LexicalNativeVersion        LexicalRule = "native-version"
	LexicalImageVersion         LexicalRule = "image-version"
	LexicalClusterRelease       LexicalRule = "cluster-release"
	LexicalOSRelease            LexicalRule = "os-release"
	LexicalCLIVersion           LexicalRule = "cli-version"
	LexicalPackageVersion       LexicalRule = "package-version"
	LexicalDuration             LexicalRule = "duration"
	LexicalMAC                  LexicalRule = "mac"
	LexicalAbsolutePath         LexicalRule = "absolute-path"
	LexicalDevicePath           LexicalRule = "device-path"
	LexicalRelativePath         LexicalRule = "relative-path"
	LexicalToken                LexicalRule = "token"
	LexicalPOSIXUser            LexicalRule = "posix-user"
	LexicalChecksum             LexicalRule = "checksum"
	LexicalLDAPURL              LexicalRule = "ldap-url"
	LexicalLDAPDN               LexicalRule = "ldap-dn"
	LexicalLDAPAttribute        LexicalRule = "ldap-attribute"
	LexicalLDAPFilter           LexicalRule = "ldap-filter"
	LexicalRealm                LexicalRule = "realm"
)

var lexicalRules = map[LexicalRule]func(string) bool{
	LexicalKubernetesLabelKey: func(value string) bool {
		prefix, name, qualified := strings.Cut(value, "/")
		if qualified {
			return validDNS(prefix) && kubernetesLabelPattern.MatchString(name)
		}
		return kubernetesLabelPattern.MatchString(value)
	},
	LexicalKubernetesLabelValue: kubernetesLabelPattern.MatchString,
	LexicalName:                 labelPattern.MatchString,
	LexicalDNS:                  validDNS,
	LexicalHost:                 validHost,
	LexicalIP: func(value string) bool {
		_, err := netip.ParseAddr(value)
		return err == nil && !strings.Contains(value, "%")
	},
	LexicalCIDR: func(value string) bool {
		_, err := netip.ParsePrefix(value)
		return err == nil
	},
	LexicalHTTPURL:  func(value string) bool { return validHTTPURL(value, false) },
	LexicalHTTPSURL: func(value string) bool { return validHTTPURL(value, true) },
	LexicalRepositoryURL: func(value string) bool {
		return validHTTPURL(value, false) && !strings.ContainsAny(value, "#'")
	},
	LexicalMirrorURL: validMirrorURL,
	LexicalArtifactMirrorURL: func(value string) bool {
		return ValidArtifactMirrorEndpoint(value) && defaultHTTPSPort(value)
	},
	LexicalProxyEndpoint: validProxyEndpoint,
	LexicalProxyBypass:   validProxyBypass,
	LexicalProxyDestination: func(value string) bool {
		_, _, ok := ProxyDestination(value)
		return ok
	},
	LexicalSystemdUnit: systemdUnitPattern.MatchString,
	LexicalPackageSpec: packageSpecPattern.MatchString,
	LexicalKickstartToken: func(value string) bool {
		return !strings.HasPrefix(value, "%") && printableExcept(value, kickstartSeparators)
	},
	LexicalIfname: func(value string) bool {
		return interfacePattern.MatchString(value) && value != "." && value != ".."
	},
	LexicalCephHostLabel: cephHostLabelPattern.MatchString,
	LexicalBridge: func(value string) bool {
		return bridgePattern.MatchString(value) && value != "." && value != ".."
	},
	LexicalRegistry:     validRegistry,
	LexicalRegistryBase: validRegistry,
	LexicalImage:        validImage,
	LexicalPackageRepository: func(value string) bool {
		u, err := neturl.Parse(value)
		return len(value) <= 2048 && len(strings.TrimRight(value, "/")) <= packageRepositoryBase && validHTTPURL(value, true) && err == nil && u.Fragment == "" && u.RawQuery == "" && plainURLPath(u.Path)
	},
	LexicalPackageRepositoryKey: func(value string) bool {
		u, err := neturl.Parse(value)
		return len(value) <= 2048 && validHTTPURL(value, true) && err == nil && u.Fragment == "" && u.RawQuery == "" && plainURLPath(u.Path)
	},
	LexicalNativeVersion: func(value string) bool {
		return len(value) > 0 && len(value) <= 1024 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }) < 0
	},
	LexicalImageVersion: func(value string) bool {
		return value != "latest" && (imageTagPattern.MatchString(value) || strings.HasPrefix(strings.ToLower(value), "sha256:") && digestPattern.MatchString(value))
	},
	LexicalClusterRelease: func(value string) bool {
		return len(value) <= 96 && clusterReleasePattern.MatchString(value)
	},
	LexicalOSRelease: func(value string) bool {
		return len(value) <= 64 && osReleasePattern.MatchString(value)
	},
	LexicalCLIVersion: func(value string) bool {
		return len(strings.TrimPrefix(value, "v")) <= 95 && cliVersionPattern.MatchString(value)
	},
	LexicalPackageVersion: packageVersionPattern.MatchString,
	LexicalDuration: func(value string) bool {
		duration, err := time.ParseDuration(value)
		return err == nil && duration > 0
	},
	LexicalMAC: func(value string) bool {
		mac, err := net.ParseMAC(value)
		return err == nil && len(mac) == 6
	},
	LexicalAbsolutePath: func(value string) bool {
		return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
	},
	LexicalDevicePath: func(value string) bool {
		return devicePathPattern.MatchString(value) && path.Clean(value) == value
	},
	LexicalRelativePath: func(value string) bool {
		return !strings.HasPrefix(value, "/") && value != "." && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 0)
	},
	LexicalToken:     tokenPattern.MatchString,
	LexicalPOSIXUser: userPattern.MatchString,
	LexicalChecksum: func(value string) bool {
		return digestPattern.MatchString(strings.TrimSpace(value))
	},
	LexicalLDAPURL: validLDAPURL,
	LexicalLDAPDN:  validLDAPDN,
	LexicalLDAPAttribute: func(value string) bool {
		return len(value) <= maxLDAPAttributeBytes && ldapAttributePattern.MatchString(value)
	},
	LexicalLDAPFilter: validLDAPFilter,
	LexicalRealm:      func(value string) bool { return validDNS(strings.ToLower(value)) },
}

func ValidLexical(rule LexicalRule, value string) bool {
	valid, ok := lexicalRules[rule]
	switch {
	case !ok:
		return false
	case rule == LexicalChecksum:
		return valid(value)
	case rule == LexicalKubernetesLabelValue && value == "":
		return true
	case value == "" || strings.TrimSpace(value) != value:
		return false
	}
	return valid(value)
}

func plainURLPath(decoded string) bool {
	for _, part := range strings.Split(decoded, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return !strings.Contains(decoded, "\\")
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

const maxArtifactMirrorSuffix = len("/helm/v") + 95

const packageRepositoryBase = 2048 - len("/repodata/") - 240

// ValidArtifactMirrorEndpoint leaves room for the longest client release path.
func ValidArtifactMirrorEndpoint(value string) bool {
	return len(value) <= maxMirrorURLBytes-maxArtifactMirrorSuffix && validMirrorURL(value)
}

func defaultHTTPSPort(value string) bool {
	u, err := neturl.Parse(value)
	return err == nil && (u.Port() == "" || u.Port() == "443")
}

// validMirrorURL admits an HTTPS URL with an unescaped path and no query or fragment.
func validMirrorURL(value string) bool {
	if len(value) > maxMirrorURLBytes || strings.ContainsAny(value, "?#") || !validHTTPURL(value, true) {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.RawPath == ""
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

const ProxyDestinationForms = "a host name, a .domain suffix, which admits the domain and every name beneath it, or an IP address, each optionally with :port, an IPv6 address bracketed before one"

func ProxyDestination(value string) (string, int, bool) {
	host, port := value, 0
	switch {
	case strings.HasPrefix(value, "["):
		end := strings.Index(value, "]")
		if end < 0 || !strings.HasPrefix(value[end+1:], ":") {
			return "", 0, false
		}
		address, err := netip.ParseAddr(value[1:end])
		if err != nil || !address.Is6() || address.Zone() != "" {
			return "", 0, false
		}
		host, port = address.String(), destinationPort(value[end+2:])
		if port == 0 {
			return "", 0, false
		}
		return host, port, true
	case strings.Count(value, ":") == 1:
		host, port = value[:strings.Index(value, ":")], destinationPort(value[strings.Index(value, ":")+1:])
		if port == 0 {
			return "", 0, false
		}
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" || port != 0 && address.Is6() {
			return "", 0, false
		}
		return address.String(), port, true
	}
	if !validDNS(strings.TrimPrefix(host, ".")) {
		return "", 0, false
	}
	return host, port, true
}

func destinationPort(digits string) int {
	if digits == "" || len(digits) > 5 || digits[0] == '0' {
		return 0
	}
	port := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0
		}
		port = port*10 + int(c-'0')
	}
	if port > 65535 {
		return 0
	}
	return port
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
		return strings.Contains(base, "/") && validRegistry(base) && strings.HasPrefix(strings.ToLower(digest), "sha256:") && digestPattern.MatchString(digest)
	}
	lastSlash := strings.LastIndex(value, "/")
	colon := strings.LastIndex(value, ":")
	if lastSlash < 0 || colon <= lastSlash || colon <= 0 {
		return false
	}
	return validRegistry(value[:colon]) && value[colon+1:] != "latest" && imageTagPattern.MatchString(value[colon+1:])
}

const (
	maxLDAPAttributeBytes = 127
	maxLDAPDNBytes        = 1024
	maxLDAPFilterBytes    = 1024
	maxOIDCIssuerBytes    = 2048

	LDAPUsernameToken = "@USERNAME@"
)

func validLDAPURL(value string) bool {
	if len(value) > 512 || !printableExcept(value, uriExcluded) || strings.ContainsAny(value, "?#") || !strings.HasPrefix(value, "ldaps://") && !strings.HasPrefix(value, "ldap://") {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.Opaque == "" && u.User == nil && u.Path == "" && u.RawPath == "" && u.Host != "" &&
		validHost(u.Hostname()) && (u.Scheme == "ldaps" || u.Scheme == "ldap") && validURLPort(u)
}

const OIDCIssuerForm = "an https URL of at most 2048 bytes with no userinfo, query, fragment or trailing slash, such as https://sso.example.test/realms/ceph"

func ValidOIDCIssuer(value string) bool {
	if len(value) > maxOIDCIssuerBytes || strings.ContainsAny(value, "?#") || strings.HasSuffix(value, "/") || !validHTTPURL(value, true) {
		return false
	}
	u, err := neturl.Parse(value)
	return err == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

func validLDAPDN(value string) bool {
	return len(value) <= maxLDAPDNBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validLDAPFilter(value string) bool {
	if len(value) > maxLDAPFilterBytes || !validLDAPDN(value) || !strings.HasPrefix(value, "(") || !strings.HasSuffix(value, ")") || strings.Contains(value, LDAPUsernameToken) {
		return false
	}
	depth := 0
	for index := range len(value) {
		switch value[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}
