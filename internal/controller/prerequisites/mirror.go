package prerequisites

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

const WheelMirrorUsage = "file:///<absolute directory> or https://<host>[/<base>] with no credentials, query or fragment"

type WheelMirror struct {
	scheme string
	base   string
}

func ParseWheelMirror(raw string) (WheelMirror, error) {
	if raw == "" {
		return WheelMirror{}, nil
	}
	refused := errors.New("the wheel mirror must be " + WheelMirrorUsage)
	address, err := url.Parse(raw)
	if err != nil || address.User != nil || address.Opaque != "" || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return WheelMirror{}, refused
	}
	switch address.Scheme {
	case "file":
		if address.Host != "" || !path.IsAbs(address.Path) {
			return WheelMirror{}, refused
		}
		return WheelMirror{scheme: "file", base: path.Clean(address.Path)}, nil
	case "https":
		if address.Host == "" || address.Port() != "" && address.Port() != "443" {
			return WheelMirror{}, refused
		}
		return WheelMirror{scheme: "https", base: "https://" + address.Host + strings.TrimRight(path.Clean("/"+address.EscapedPath()), "/")}, nil
	}
	return WheelMirror{}, refused
}

func (m WheelMirror) Set() bool { return m.scheme != "" }

func (m WheelMirror) Directory() (string, bool) { return m.base, m.scheme == "file" }

func (m WheelMirror) String() string {
	if m.scheme == "file" {
		return (&url.URL{Scheme: "file", Path: m.base}).String()
	}
	return m.base
}

func (m WheelMirror) Location(filename string) string {
	if m.scheme == "file" {
		return (&url.URL{Scheme: "file", Path: path.Join(m.base, filename)}).String()
	}
	return m.base + "/" + url.PathEscape(filename)
}
