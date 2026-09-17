package sshlocal

import (
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

// unsafeDirectives would change what a session connects to, what it offers, or
// what it runs. Their presence in the policy backend is not something to skip
// quietly: a host whose policy carries one is not a host this product can honor
// while still bounding the session, so the session refuses instead.
var unsafeDirectives = map[string]bool{
	"identityfile":        true,
	"certificatefile":     true,
	"identityagent":       true,
	"pkcs11provider":      true,
	"securitykeyprovider": true,
	"addkeystoagent":      true,
	"hostname":            true,
	"port":                true,
	"user":                true,
	"proxycommand":        true,
	"proxyjump":           true,
	"localcommand":        true,
	"permitlocalcommand":  true,
	"knownhostscommand":   true,
	"localforward":        true,
	"remoteforward":       true,
	"dynamicforward":      true,
	"include":             true,
	"match":               true,
	"host":                true,
}

// cryptographicPolicy reduces the host policy backend to the directives a
// session may carry. A host with no policy backend keeps the client's own
// compiled defaults, which is what a platform without one already uses.
func cryptographicPolicy(data []byte, present bool) ([]byte, error) {
	if !present {
		return nil, nil
	}
	if len(data) > maxPolicyBytes {
		return nil, failure("the host SSH crypto policy exceeds its byte limit", "")
	}
	var lines []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keyword := strings.ToLower(directive(line))
		switch {
		case cryptographicDirectives[keyword]:
			lines = append(lines, line)
		case unsafeDirectives[keyword]:
			return nil, failure("the host SSH crypto policy carries the unsupported directive "+directive(line),
				"remove it from "+PolicyPath+" or report the platform that ships it")
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func directive(line string) string {
	keyword := strings.Fields(line)[0]
	if index := strings.IndexByte(keyword, '='); index >= 0 {
		keyword = keyword[:index]
	}
	return keyword
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("access.unavailable", message, "", remediation)
}

// keyFailure reports what a host-key observation could not prove. An
// unreachable endpoint is unknown, never an absent or an untrusted key.
func keyFailure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("trust.identity", message, "", remediation)
}
