package sshlocal

import (
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// PolicyPath is the host crypto-policy backend a session retains. Reading it
// is what keeps site and FIPS cryptographic selection in force while the rest
// of the system and personal client configuration stays out.
const PolicyPath = "/etc/crypto-policies/back-ends/openssh.config"

const maxPolicyBytes = 64 << 10

// cryptographicDirectives are the only client settings a session copies from
// the host policy. Each one selects algorithms and nothing else.
var cryptographicDirectives = map[string]bool{
	"ciphers":                     true,
	"macs":                        true,
	"kexalgorithms":               true,
	"gssapikexalgorithms":         true,
	"hostkeyalgorithms":           true,
	"pubkeyacceptedalgorithms":    true,
	"pubkeyacceptedkeytypes":      true,
	"hostbasedacceptedalgorithms": true,
	"hostbasedkeytypes":           true,
	"casignaturealgorithms":       true,
	"requiredrsasize":             true,
	"rsaminsize":                  true,
	"rekeylimit":                  true,
}
var policyLine = regexp.MustCompile(`^([A-Za-z]+)\s*(?:=\s*|\s+)([A-Za-z0-9@.,_+^*?!/ -]+)$`)

// cryptographicPolicy reduces the host policy backend to the allowlisted
// directives a session may carry, and refuses any other line, so a policy it
// cannot read in full is never half honored. No backend keeps client defaults.
func cryptographicPolicy(data []byte, present bool) ([]byte, error) {
	if !present {
		return nil, nil
	}
	if len(data) > maxPolicyBytes {
		return nil, failure("the host SSH crypto policy exceeds its byte limit", "")
	}
	var lines []string
	for _, raw := range strings.Split(string(data), "\n") {
		line, _, _ := strings.Cut(raw, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := policyLine.FindStringSubmatch(line)
		if match == nil {
			return nil, failure("the host SSH crypto policy has a line with unsupported syntax",
				"remove it from "+PolicyPath+" or report the platform that ships it")
		}
		keyword := strings.ToLower(match[1])
		switch {
		case keyword == "gssapikeyexchange":
			if !strings.EqualFold(strings.TrimSpace(match[2]), "no") {
				return nil, failure("the host SSH crypto policy enables unsupported GSSAPI key exchange", "retain only GSSAPIKeyExchange no in "+PolicyPath)
			}
		case !cryptographicDirectives[keyword]:
			return nil, failure("the host SSH crypto policy carries the unsupported directive "+match[1],
				"remove it from /etc/crypto-policies/local.d/openssh-*.config and run update-crypto-policies, or report the platform that ships it")
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("access.unavailable", message, "", remediation)
}

// keyFailure reports what a host-key observation could not prove. An
// unreachable endpoint is unknown, never an absent or an untrusted key.
func keyFailure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("trust.identity", message, "", remediation)
}
