package sshlocal

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestTheSessionConfigurationCarriesOnlyCryptographicSelection(t *testing.T) {
	policy := strings.Join([]string{
		"# crypto policy",
		"",
		"Ciphers aes256-gcm@openssh.com",
		"MACs hmac-sha2-512",
		"KexAlgorithms ecdh-sha2-nistp256",
		"PubkeyAcceptedAlgorithms rsa-sha2-512",
		"RequiredRSASize 2048",
		"Compression no",
		"ServerAliveInterval 30",
	}, "\n")
	data, err := cryptographicPolicy([]byte(policy), true)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, kept := range []string{"Ciphers aes256-gcm@openssh.com", "MACs hmac-sha2-512",
		"KexAlgorithms ecdh-sha2-nistp256", "PubkeyAcceptedAlgorithms rsa-sha2-512", "RequiredRSASize 2048"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("policy dropped %q: %q", kept, got)
		}
	}
	for _, dropped := range []string{"Compression", "ServerAliveInterval", "#"} {
		if strings.Contains(got, dropped) {
			t.Fatalf("policy kept %q: %q", dropped, got)
		}
	}
}

// A directive that would change the target, the credential or what runs is not
// something to skip quietly: the session refuses rather than connecting under a
// policy it cannot honor while still bounding itself.
func TestAPolicyThatWouldRedirectTheSessionRefusesIt(t *testing.T) {
	for _, directive := range []string{
		"ProxyCommand nc %h %p", "IdentityFile /root/.ssh/id_rsa", "IdentityAgent /run/agent",
		"Include /etc/ssh/other", "Match host *", "LocalForward 8080 localhost:80",
		"PermitLocalCommand yes", "KnownHostsCommand /usr/bin/false", "User root", "Hostname elsewhere",
	} {
		t.Run(strings.Fields(directive)[0], func(t *testing.T) {
			_, err := cryptographicPolicy([]byte("Ciphers aes256-gcm@openssh.com\n"+directive+"\n"), true)
			if err == nil {
				t.Fatalf("%q was accepted", directive)
			}
			if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "access.unavailable" {
				t.Fatalf("diagnostic = %+v", reported)
			}
		})
	}
}

func TestAHostWithNoPolicyBackendKeepsTheClientDefaults(t *testing.T) {
	data, err := cryptographicPolicy(nil, false)
	if err != nil || data != nil {
		t.Fatalf("policy = %q (%v)", data, err)
	}
	empty, err := cryptographicPolicy([]byte("# nothing but a comment\n"), true)
	if err != nil || empty != nil {
		t.Fatalf("policy = %q (%v)", empty, err)
	}
}

func TestAnEqualsSeparatedDirectiveIsReadAsItsKeyword(t *testing.T) {
	if _, err := cryptographicPolicy([]byte("ProxyCommand=nc %h %p\n"), true); err == nil {
		t.Fatal("an equals-separated unsafe directive was accepted")
	}
	data, err := cryptographicPolicy([]byte("Ciphers=aes256-gcm@openssh.com\n"), true)
	if err != nil || !strings.Contains(string(data), "Ciphers=aes256-gcm@openssh.com") {
		t.Fatalf("policy = %q (%v)", data, err)
	}
}

func TestAnUnboundedPolicyIsRefused(t *testing.T) {
	if _, err := cryptographicPolicy([]byte(strings.Repeat("Ciphers aes256-gcm\n", 1<<16)), true); err == nil {
		t.Fatal("an unbounded policy was accepted")
	}
}
