package prerequisites

import (
	"context"
	"net/url"
	"strings"
)

// ClientInstallation is one authorized controller-stage publication of the
// target tools a context selects into Target, the writable shared area.
// Identity is the frozen block's request digest. Publish receives the sources
// the automation is about to add before it adds any. Launch belongs to the
// controller runtime the caller already holds.
type ClientInstallation struct {
	Target   BundleArea
	Identity string
	Tools    []ToolDefinition
	Egress   SetupEgress
	Trust    MirrorTrust
	Launch   PythonLaunch
	Publish  func(context.Context, []string) error
	Progress func(ProgressEvent)
	// Output retains what this run prints on its own standard output and
	// error. The stage runs as one lifecycle block, so its output belongs to
	// that block's attempt exactly as any other adapter run's does.
	Output RunOutput
}

// MirrorAuthority identifies the HTTPS endpoint scoped to a lent trust anchor.
// The default HTTPS port is implicit in the frozen authority.
func MirrorAuthority(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" || port == "443" {
		return host
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}

type MirrorTrust struct {
	Host   string
	Anchor []byte
}

// StagePreparation is a controller stage attempt's durable intent, recorded
// before its first host effect: the host package requirements it is about to
// install and the target tool sources it is about to add.
type StagePreparation struct {
	Packages     []string `json:"packages"`
	AddedSources []string `json:"addedSources"`
}
