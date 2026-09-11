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
		u, err := neturl.Parse(value)
		return err == nil && u.Opaque == "" && u.User == nil && u.Host != "" && validHost(u.Hostname()) && (u.Scheme == "https" || rule == "http-url" && u.Scheme == "http") && validURLPort(u)
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
	case "checksum":
		return digestPattern.MatchString(strings.TrimSpace(value))
	case "absolute-path":
		return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
	case "device-path":
		return strings.HasPrefix(value, "/dev/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
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
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, ":%") {
			return false
		}
	}
	return true
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
